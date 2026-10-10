package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var supportedScreens = map[string]bool{
	"1280x720@30": true, "1280x720@60": true,
	"1280x720@90": true, "1280x720@120": true,
	"1920x1080@30": true, "1920x1080@60": true,
	"1920x1080@90": true, "1920x1080@120": true,
	"2560x1440@30": true,
}
var ErrInvalidSessionOptions = errors.New("invalid session options")
var ErrRenderPaused = errors.New("rendering is stopped by the administrator")
var ErrSessionBusy = errors.New("session has active viewers")
var ErrSessionNotFound = errors.New("session not found")

type WorkerController interface {
	StartWorker(context.Context, WorkerSpec) (Worker, error)
	Stop(context.Context, string) error
}

// WorkerActivityController lets preloaded workers sleep while nobody watches.
// Renderers without this capability continue to run normally.
type WorkerActivityController interface {
	SetActive(context.Context, string, bool) error
}

type SessionOptions struct {
	TargetURL        string
	Resolution       string
	FPS              int
	RenderMode       string
	ReadySelector    string
	ConcurrencyMode  string
	MaxViewers       int
	PreloadCount     int
	Name             string
	FramePolicy      string
	ReservedPreloads int
	ShowFPS          *bool
}

type SessionInfo struct {
	Token            string    `json:"token"`
	TargetURL        string    `json:"target_url,omitempty"`
	Resolution       string    `json:"resolution"`
	FPS              int       `json:"fps"`
	RenderMode       string    `json:"render_mode"`
	ReadySelector    string    `json:"ready_selector,omitempty"`
	ConcurrencyMode  string    `json:"concurrency_mode"`
	MaxViewers       int       `json:"max_viewers"`
	PreloadCount     int       `json:"preload_count"`
	Name             string    `json:"name"`
	FramePolicy      string    `json:"frame_policy"`
	ReservedPreloads int       `json:"reserved_preloads"`
	ShowFPS          bool      `json:"show_fps"`
	CreatedAt        time.Time `json:"created_at"`
	RenderPaused     bool      `json:"render_paused,omitempty"`
}

type SessionStatus struct {
	SessionInfo
	Viewers            int       `json:"viewers"`
	RunningInstances   int       `json:"running_instances"`
	ReadyInstances     int       `json:"ready_instances"`
	AvailableInstances int       `json:"available_instances"`
	StartingInstances  int       `json:"starting_instances"`
	PoolError          string    `json:"pool_error,omitempty"`
	Running            bool      `json:"running"`
	StartedAt          time.Time `json:"started_at,omitempty"`
	PreloadError       string    `json:"preload_error,omitempty"`
	StreamURL          string    `json:"stream_url,omitempty"`
	PreloadSuspended   bool      `json:"preload_suspended"`
	IdleSince          time.Time `json:"idle_since,omitempty"`
}

var ErrViewerLimit = errors.New("viewer limit reached")

type session struct {
	mu                    sync.Mutex
	lifecycleMu           sync.Mutex
	startMu               sync.Mutex
	startCancel           context.CancelFunc
	cachedStatus          atomic.Pointer[SessionStatus]
	info                  SessionInfo
	viewers               int
	primaryInUse          bool
	channels              map[string]bool // true while a viewer owns this extra worker
	standby               map[string]Worker
	pendingChannels       map[string]context.CancelFunc
	transitioning         bool
	reusableChannels      []string
	poolMu                sync.Mutex
	changed               chan struct{}
	poolError             string
	primaryLoading        bool
	primaryDirty          bool
	idleSince             time.Time
	started               bool
	worker                Worker
	deleted               bool
	published             bool
	lastPreloadCheck      time.Time
	preloadError          string
	preloadSuspendedUntil time.Time
}

type SessionManager struct {
	mu                 sync.RWMutex
	sessions           map[string]*session
	workers            WorkerController
	idleTimeout        time.Duration
	now                func() time.Time
	storePath          string
	persistMu          sync.Mutex
	storeReport        StoreReport // Protected by persistMu.
	keepPreloaded      bool
	capacityMu         sync.RWMutex
	channelCapacity    int
	capacitySource     func() int
	preloadContext     context.Context
	preloadTimeout     time.Duration
	stopping           atomic.Bool
	settingsMu         sync.RWMutex
	performanceSource  func() PerformanceSettings
	availabilitySource func() int
	viewerDemand       atomic.Int64
	reclaimSlots       chan struct{}
}

// SetKeepPreloaded must be called before the manager begins serving requests.
// Published links then retain their renderer between visits.
func (m *SessionManager) SetKeepPreloaded(value bool) { m.keepPreloaded = value }

// SetPreloadContext bounds background replenishment to the server lifetime.
func (m *SessionManager) SetPreloadContext(ctx context.Context, timeout time.Duration) {
	m.preloadContext, m.preloadTimeout = ctx, timeout
}

// SetChannelCapacity supplies the shared worker capacity. Project quotas may
// exceed it; the worker allocator enforces actual admission across all projects.
func (m *SessionManager) SetChannelCapacity(value int) {
	m.capacityMu.Lock()
	defer m.capacityMu.Unlock()
	m.channelCapacity = value
	m.capacitySource = nil
}

// SetChannelCapacitySource keeps management responses current when the shared
// worker allocator is resized at runtime. A nil source uses the static value.
func (m *SessionManager) SetChannelCapacitySource(source func() int) {
	m.capacityMu.Lock()
	defer m.capacityMu.Unlock()
	m.capacitySource = source
}

func (m *SessionManager) ChannelCapacity() int {
	m.capacityMu.RLock()
	source, value := m.capacitySource, m.channelCapacity
	m.capacityMu.RUnlock()
	if source != nil {
		return source()
	}
	return value
}

func NewSessionManager(workers WorkerController, idleTimeout time.Duration) (*SessionManager, error) {
	if workers == nil || idleTimeout <= 0 {
		return nil, errors.New("worker controller and positive idle timeout are required")
	}
	return &SessionManager{sessions: make(map[string]*session), workers: workers, idleTimeout: idleTimeout, now: time.Now, preloadContext: context.Background(), preloadTimeout: 310 * time.Second, reclaimSlots: make(chan struct{}, 1)}, nil
}

func (m *SessionManager) Create(targetURL, resolution string, fps int) (SessionInfo, error) {
	return m.CreatePublished(SessionOptions{TargetURL: targetURL, Resolution: resolution, FPS: fps})
}

// CreatePublished persists a link immediately. Call Warm independently to
// preload the renderer without tying its lifetime to the HTTP request.
func (m *SessionManager) CreatePublished(options SessionOptions) (SessionInfo, error) {
	info, err := m.create(options)
	if err != nil {
		return SessionInfo{}, err
	}
	m.mu.RLock()
	s := m.sessions[info.Token]
	m.mu.RUnlock()
	s.mu.Lock()
	s.published = true
	s.cacheStatusLocked()
	s.mu.Unlock()
	if err := m.save(); err != nil {
		m.mu.Lock()
		delete(m.sessions, info.Token)
		m.mu.Unlock()
		return SessionInfo{}, err
	}
	return info, nil
}

func (m *SessionManager) CreatePrewarmed(ctx context.Context, options SessionOptions) (SessionInfo, error) {
	info, err := m.create(options)
	if err != nil {
		return SessionInfo{}, err
	}
	m.mu.RLock()
	s := m.sessions[info.Token]
	m.mu.RUnlock()
	s.mu.Lock()
	_, err = m.startLocked(ctx, s)
	s.mu.Unlock()
	if err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		stopErr := m.workers.Stop(stopCtx, info.Token)
		cancel()
		if stopErr == nil {
			m.mu.Lock()
			delete(m.sessions, info.Token)
			m.mu.Unlock()
		} else {
			s.mu.Lock()
			s.started = true
			s.idleSince = m.now()
			s.mu.Unlock()
		}
		return SessionInfo{}, errors.Join(err, stopErr)
	}
	s.mu.Lock()
	s.published = true
	s.cacheStatusLocked()
	s.mu.Unlock()
	if err := m.save(); err != nil {
		s.mu.Lock()
		s.published = false
		s.cacheStatusLocked()
		s.mu.Unlock()
		stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		stopErr := m.workers.Stop(stopCtx, info.Token)
		cancel()
		if stopErr == nil {
			m.mu.Lock()
			delete(m.sessions, info.Token)
			m.mu.Unlock()
		}
		return SessionInfo{}, errors.Join(err, stopErr)
	}
	if m.keepPreloaded {
		m.warmPool(ctx, s)
	}
	return info, nil
}

func (m *SessionManager) create(options SessionOptions) (SessionInfo, error) {
	var err error
	options, err = normalizeSessionOptions(options)
	if err != nil {
		return SessionInfo{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for range 3 {
		var raw [18]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return SessionInfo{}, fmt.Errorf("generate token: %w", err)
		}
		token := "g3d_" + base64.RawURLEncoding.EncodeToString(raw[:])
		if _, exists := m.sessions[token]; exists {
			continue
		}
		info := sessionInfoFromOptions(token, m.now().UTC(), options)
		m.sessions[token] = &session{info: info}
		return info, nil
	}
	return SessionInfo{}, errors.New("could not generate a unique token")
}

func normalizeSessionOptions(options SessionOptions) (SessionOptions, error) {
	if options.FramePolicy == "" {
		options.FramePolicy = "interactive"
	}
	if options.FramePolicy != "interactive" && options.FramePolicy != "animation" {
		return SessionOptions{}, fmt.Errorf("%w: frame_policy must be interactive or animation", ErrInvalidSessionOptions)
	}
	if err := ValidateTarget(options.TargetURL); err != nil {
		return SessionOptions{}, fmt.Errorf("%w: %v", ErrInvalidSessionOptions, err)
	}
	if options.Resolution == "" {
		options.Resolution = "1920x1080"
	}
	if options.FPS == 0 {
		options.FPS = 30
	}
	if !validSessionScreen(options.Resolution, options.FPS) {
		return SessionOptions{}, fmt.Errorf("%w: unsupported resolution/fps combination", ErrInvalidSessionOptions)
	}
	if options.RenderMode == "" {
		options.RenderMode = "cpu"
	}
	if options.RenderMode != "cpu" && options.RenderMode != "nvidia" {
		return SessionOptions{}, fmt.Errorf("%w: render_mode must be cpu or nvidia", ErrInvalidSessionOptions)
	}
	if err := validateReadySelector(options.ReadySelector); err != nil {
		return SessionOptions{}, fmt.Errorf("%w: %v", ErrInvalidSessionOptions, err)
	}
	if options.ConcurrencyMode == "" {
		options.ConcurrencyMode = "single"
	}
	if options.ConcurrencyMode != "single" && options.ConcurrencyMode != "multi" {
		return SessionOptions{}, fmt.Errorf("%w: concurrency_mode must be single or multi", ErrInvalidSessionOptions)
	}
	if options.ConcurrencyMode == "single" {
		if options.MaxViewers != 0 && options.MaxViewers != 1 {
			return SessionOptions{}, fmt.Errorf("%w: 单路会话的最大会话数必须为 1", ErrInvalidSessionOptions)
		}
		options.MaxViewers = 1
	}
	if options.MaxViewers == 0 {
		options.MaxViewers = 3
	}
	if options.MaxViewers < 1 || options.MaxViewers > 100 {
		return SessionOptions{}, fmt.Errorf("%w: max_viewers must be between 1 and 100", ErrInvalidSessionOptions)
	}
	if options.ConcurrencyMode == "multi" && options.MaxViewers < 2 {
		return SessionOptions{}, fmt.Errorf("%w: multi mode requires at least 2 viewers", ErrInvalidSessionOptions)
	}
	if options.PreloadCount == 0 {
		options.PreloadCount = 1
	}
	if options.PreloadCount < 1 || options.PreloadCount > options.MaxViewers {
		return SessionOptions{}, fmt.Errorf("%w: preload_count must be between 1 and max_viewers", ErrInvalidSessionOptions)
	}
	if options.ReservedPreloads < 0 || options.ReservedPreloads > options.PreloadCount {
		return SessionOptions{}, fmt.Errorf("%w: reserved_preloads must be between 0 and preload_count", ErrInvalidSessionOptions)
	}
	options.Name = strings.TrimSpace(options.Name)
	if len(options.Name) > 100 {
		return SessionOptions{}, fmt.Errorf("%w: name is too long", ErrInvalidSessionOptions)
	}
	return options, nil
}

func sessionInfoFromOptions(token string, createdAt time.Time, options SessionOptions) SessionInfo {
	showFPS := true
	if options.ShowFPS != nil {
		showFPS = *options.ShowFPS
	}
	return SessionInfo{Token: token, TargetURL: options.TargetURL, Resolution: options.Resolution, FPS: options.FPS,
		RenderMode: options.RenderMode, ReadySelector: options.ReadySelector, ConcurrencyMode: options.ConcurrencyMode,
		MaxViewers: options.MaxViewers, PreloadCount: options.PreloadCount, Name: options.Name, CreatedAt: createdAt,
		FramePolicy: options.FramePolicy, ReservedPreloads: options.ReservedPreloads, ShowFPS: showFPS}
}

func (m *SessionManager) Lookup(token string) (SessionInfo, bool) {
	m.mu.RLock()
	s, ok := m.sessions[token]
	m.mu.RUnlock()
	if !ok {
		return SessionInfo{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.published || s.deleted {
		return SessionInfo{}, false
	}
	return s.info, true
}

// Status reads the published snapshot without waiting for a slow browser
// preload that holds the session lock.
func (m *SessionManager) Status(token string) (SessionStatus, bool) {
	m.mu.RLock()
	s := m.sessions[token]
	m.mu.RUnlock()
	if s == nil {
		return SessionStatus{}, false
	}
	if s.mu.TryLock() {
		s.cacheStatusLocked()
		s.mu.Unlock()
	}
	status := s.cachedStatus.Load()
	if status == nil {
		return SessionStatus{}, false
	}
	return *status, true
}

// Update changes a published link in place. Render settings require a fresh
// worker, while the token and creation time remain stable.
func (m *SessionManager) Update(ctx context.Context, token string, options SessionOptions) (SessionInfo, bool, error) {
	options, err := normalizeSessionOptions(options)
	if err != nil {
		return SessionInfo{}, false, err
	}
	m.mu.RLock()
	s := m.sessions[token]
	m.mu.RUnlock()
	if s == nil {
		return SessionInfo{}, false, ErrSessionNotFound
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if status := s.cachedStatus.Load(); status != nil && (status.TargetURL != options.TargetURL ||
		status.Resolution != options.Resolution || status.FPS != options.FPS ||
		status.RenderMode != options.RenderMode || status.ReadySelector != options.ReadySelector || status.FramePolicy != options.FramePolicy) {
		s.startMu.Lock()
		if s.startCancel != nil {
			s.startCancel()
		}
		s.startMu.Unlock()
	}
	s.mu.Lock()
	if s.deleted || !s.published {
		s.mu.Unlock()
		return SessionInfo{}, false, ErrSessionNotFound
	}
	if s.viewers > 0 {
		s.mu.Unlock()
		return SessionInfo{}, false, ErrSessionBusy
	}
	previous := s.info
	next := sessionInfoFromOptions(token, previous.CreatedAt, options)
	if options.ShowFPS == nil {
		next.ShowFPS = previous.ShowFPS
	}
	next.RenderPaused = previous.RenderPaused
	restart := next.TargetURL != previous.TargetURL || next.Resolution != previous.Resolution ||
		next.FPS != previous.FPS || next.RenderMode != previous.RenderMode || next.ReadySelector != previous.ReadySelector || next.FramePolicy != previous.FramePolicy
	if restart {
		s.transitioning = true
		defer func() { s.mu.Lock(); s.transitioning = false; s.notifyLocked(); s.cacheStatusLocked(); s.mu.Unlock() }()
		if err := m.stopChannelsLocked(ctx, s); err != nil {
			s.mu.Unlock()
			return SessionInfo{}, false, err
		}
	}
	if restart && (s.started || s.primaryDirty) {
		if err := m.workers.Stop(ctx, token); err != nil {
			s.mu.Unlock()
			return SessionInfo{}, false, err
		}
		s.started = false
		s.worker = Worker{}
		s.primaryDirty = false
		s.primaryInUse = false
		s.idleSince = time.Time{}
	}
	s.info = next
	s.preloadError = ""
	s.cacheStatusLocked()
	s.mu.Unlock()
	if err := m.save(); err != nil {
		s.mu.Lock()
		s.info = previous
		s.cacheStatusLocked()
		s.mu.Unlock()
		return SessionInfo{}, false, err
	}
	return next, (restart || next.PreloadCount != previous.PreloadCount || next.MaxViewers != previous.MaxViewers) && !next.RenderPaused, nil
}

// SetShowFPS changes presentation without restarting workers or ending viewers.
func (m *SessionManager) SetShowFPS(token string, visible bool) (SessionInfo, error) {
	m.mu.RLock()
	s := m.sessions[token]
	m.mu.RUnlock()
	if s == nil {
		return SessionInfo{}, ErrSessionNotFound
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	if s.deleted || !s.published {
		s.mu.Unlock()
		return SessionInfo{}, ErrSessionNotFound
	}
	previous := s.info.ShowFPS
	s.info.ShowFPS = visible
	s.cacheStatusLocked()
	info := s.info
	s.mu.Unlock()
	if err := m.save(); err != nil {
		s.mu.Lock()
		s.info.ShowFPS = previous
		s.cacheStatusLocked()
		s.mu.Unlock()
		return SessionInfo{}, err
	}
	return info, nil
}

func (m *SessionManager) List() []SessionStatus {
	m.mu.RLock()
	items := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		items = append(items, s)
	}
	m.mu.RUnlock()
	result := make([]SessionStatus, 0, len(items))
	for _, s := range items {
		if s.mu.TryLock() {
			s.cacheStatusLocked()
			s.mu.Unlock()
		}
		if status := s.cachedStatus.Load(); status != nil {
			result = append(result, *status)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].Token < result[j].Token
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

// cacheStatusLocked keeps published link metadata readable while a slow
// browser start holds the session lock. Call only while s.mu is held.
func (s *session) cacheStatusLocked() {
	if !s.published || s.deleted {
		s.cachedStatus.Store(nil)
		return
	}
	instances := len(s.channels)
	ready, available, starting := len(s.standby), len(s.standby), len(s.pendingChannels)
	if s.primaryLoading {
		starting++
	}
	for token, owned := range s.channels {
		if owned && s.pendingChannels[token] == nil {
			ready++
		}
	}
	if s.started {
		instances++
		if !s.primaryDirty && !s.primaryLoading {
			ready++
		}
		if !s.primaryInUse && !s.primaryLoading && !s.primaryDirty && !s.transitioning {
			available++
		}
	}
	status := &SessionStatus{SessionInfo: s.info, Viewers: s.viewers, RunningInstances: instances, Running: s.started,
		StartedAt: s.worker.StartedAt, PreloadError: s.preloadError, PoolError: s.poolError, ReadyInstances: ready, AvailableInstances: available, StartingInstances: starting,
		PreloadSuspended: !s.preloadSuspendedUntil.IsZero(), IdleSince: s.idleSince}
	s.cachedStatus.Store(status)
}

func (m *SessionManager) stopChannelsLocked(ctx context.Context, s *session) error {
	var allErr error
	s.startMu.Lock()
	if s.startCancel != nil {
		s.startCancel()
	}
	s.startMu.Unlock()
	for _, cancel := range s.pendingChannels {
		cancel()
	}
	for s.primaryLoading || len(s.pendingChannels) > 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		s.mu.Lock()
	}
	for token := range s.channels {
		if s.pendingChannels[token] != nil {
			continue
		}
		if err := m.workers.Stop(ctx, token); err != nil {
			allErr = errors.Join(allErr, fmt.Errorf("stop channel %s: %w", token, err))
		} else {
			delete(s.channels, token)
			delete(s.standby, token)
		}
	}
	s.notifyLocked()
	s.cacheStatusLocked()
	return allErr
}

func newChannelToken(parent string) (string, error) {
	var raw [9]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return parent + "_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func (m *SessionManager) Delete(ctx context.Context, token string) error {
	m.mu.RLock()
	s := m.sessions[token]
	m.mu.RUnlock()
	if s == nil {
		return ErrSessionNotFound
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.startMu.Lock()
	if s.startCancel != nil {
		s.startCancel()
	}
	s.startMu.Unlock()
	m.mu.Lock()
	if m.sessions[token] != s || !s.published {
		m.mu.Unlock()
		return errors.New("session not found")
	}
	s.mu.Lock()
	s.transitioning = true
	defer func() { s.mu.Lock(); s.transitioning = false; s.notifyLocked(); s.mu.Unlock() }()
	if err := m.stopChannelsLocked(ctx, s); err != nil {
		s.mu.Unlock()
		m.mu.Unlock()
		return err
	}
	if s.started || s.primaryDirty {
		if err := m.workers.Stop(ctx, token); err != nil {
			s.mu.Unlock()
			m.mu.Unlock()
			return err
		}
	}
	s.deleted = true
	s.started = false
	s.worker = Worker{}
	s.cacheStatusLocked()
	delete(m.sessions, token)
	s.mu.Unlock()
	m.mu.Unlock()
	if err := m.save(); err != nil {
		s.mu.Lock()
		s.deleted = false
		s.cacheStatusLocked()
		s.mu.Unlock()
		m.mu.Lock()
		m.sessions[token] = s
		m.mu.Unlock()
		return err
	}
	return nil
}

func (m *SessionManager) Warm(ctx context.Context, token string) (Worker, error) {
	m.mu.RLock()
	s := m.sessions[token]
	m.mu.RUnlock()
	if s == nil {
		return Worker{}, ErrSessionNotFound
	}
	worker, err := m.warmPrimary(ctx, s)
	if err == nil && m.keepPreloaded {
		m.warmPool(ctx, s)
	}
	return worker, err
}

// PauseRendering keeps the link but stops its worker and disables automatic
// preloading, including after a gateway restart.
func (m *SessionManager) PauseRendering(ctx context.Context, token string) error {
	m.mu.RLock()
	s := m.sessions[token]
	m.mu.RUnlock()
	if s == nil {
		return errors.New("session not found")
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.startMu.Lock()
	if s.startCancel != nil {
		s.startCancel()
	}
	s.startMu.Unlock()
	s.mu.Lock()
	if s.deleted || !s.published {
		s.mu.Unlock()
		return errors.New("session not found")
	}
	previous := s.info.RenderPaused
	s.info.RenderPaused = true
	s.poolError = ""
	s.preloadError = ""
	stopErr := m.stopChannelsLocked(ctx, s)
	if s.started || s.primaryDirty {
		if err := m.workers.Stop(ctx, token); err != nil {
			stopErr = errors.Join(stopErr, err)
		} else {
			s.started = false
			s.worker = Worker{}
			s.primaryInUse = false
			s.primaryDirty = false
			s.idleSince = time.Time{}
		}
	}
	s.cacheStatusLocked()
	s.mu.Unlock()
	if err := m.save(); err != nil {
		s.mu.Lock()
		s.info.RenderPaused = previous
		s.cacheStatusLocked()
		s.mu.Unlock()
		return errors.Join(stopErr, err)
	}
	return stopErr
}

// EnableRendering persists the desired running state. Warm can then run in the
// background so the management request does not wait for a slow 3D page.
func (m *SessionManager) EnableRendering(token string) error {
	m.mu.RLock()
	s := m.sessions[token]
	m.mu.RUnlock()
	if s == nil {
		return errors.New("session not found")
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	if s.deleted || !s.published {
		s.mu.Unlock()
		return errors.New("session not found")
	}
	previous := s.info.RenderPaused
	s.info.RenderPaused = false
	s.preloadError = ""
	s.cacheStatusLocked()
	s.mu.Unlock()
	if err := m.save(); err != nil {
		s.mu.Lock()
		s.info.RenderPaused = previous
		s.cacheStatusLocked()
		s.mu.Unlock()
		return err
	}
	return nil
}

// ResumeRendering persists the desired running state before warming. If the
// browser fails to start, the normal preload retry loop can recover it.
func (m *SessionManager) ResumeRendering(ctx context.Context, token string) (Worker, error) {
	if err := m.EnableRendering(token); err != nil {
		return Worker{}, err
	}
	return m.Warm(ctx, token)
}

// Acquire assigns one browser worker to one viewer. A preloaded worker serves
// the first viewer; additional viewers receive separate processes and profiles.
func (m *SessionManager) acquire(ctx context.Context, token string) (Worker, func(), error) {
	m.mu.RLock()
	s, ok := m.sessions[token]
	m.mu.RUnlock()
	if !ok {
		return Worker{}, nil, ErrSessionNotFound
	}
	for {
		s.mu.Lock()
		if s.deleted || !s.published || m.stopping.Load() {
			s.mu.Unlock()
			return Worker{}, nil, ErrSessionNotFound
		}
		if s.info.RenderPaused {
			s.mu.Unlock()
			return Worker{}, nil, ErrRenderPaused
		}
		if s.transitioning {
			changed := s.changed
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return Worker{}, nil, ctx.Err()
			case <-changed:
				continue
			}
		}
		if s.viewers >= s.info.MaxViewers {
			s.mu.Unlock()
			return Worker{}, nil, ErrViewerLimit
		}
		var worker Worker
		var err error
		channelToken := token
		primary := false
		if s.started && !s.primaryInUse && !s.primaryDirty && !s.primaryLoading {
			primary = true
			worker, err = m.startLocked(ctx, s)
			if err == nil {
				if activity, ok := m.workers.(WorkerActivityController); ok {
					err = activity.SetActive(ctx, token, true)
				}
			}
			if err == nil {
				s.primaryInUse = true
				s.viewers++
			}
		} else if len(s.standby) > 0 {
			tokens := make([]string, 0, len(s.standby))
			for key := range s.standby {
				tokens = append(tokens, key)
			}
			sort.Strings(tokens)
			channelToken = tokens[0]
			worker = s.standby[channelToken]
			delete(s.standby, channelToken)
			s.channels[channelToken] = true
			s.viewers++
		} else {
			waiting := s.primaryLoading
			for key := range s.pendingChannels {
				if !s.channels[key] {
					waiting = true
					break
				}
			}
			if waiting {
				if s.changed == nil {
					s.changed = make(chan struct{})
				}
				changed := s.changed
				s.mu.Unlock()
				select {
				case <-ctx.Done():
					return Worker{}, nil, ctx.Err()
				case <-changed:
					continue
				}
			}
			if !s.started && !s.primaryInUse && !s.primaryDirty {
				primary = true
				worker, err = m.startLocked(ctx, s)
				if err == nil {
					s.primaryInUse = true
					s.viewers++
				}
			} else {
				channelToken, err = newChannelToken(token)
				if err == nil {
					s.viewers++
					startCtx, spec := s.reserveChannelLocked(ctx, channelToken, true)
					s.mu.Unlock()
					worker, err = m.startChannel(startCtx, s, spec)
					if err != nil {
						return Worker{}, nil, err
					}
					var once sync.Once
					return worker, func() { once.Do(func() { m.releaseViewer(s, channelToken, false) }) }, nil
				}
			}
		}
		if err != nil {
			s.mu.Unlock()
			return Worker{}, nil, err
		}
		s.idleSince = time.Time{}
		s.preloadSuspendedUntil = time.Time{}
		s.cacheStatusLocked()
		s.mu.Unlock()
		var once sync.Once
		return worker, func() { once.Do(func() { m.releaseViewer(s, channelToken, primary) }) }, nil
	}
}

func (m *SessionManager) startLocked(ctx context.Context, s *session) (Worker, error) {
	if s.primaryDirty {
		if err := m.workers.Stop(ctx, s.info.Token); err != nil {
			return Worker{}, err
		}
		s.started = false
		s.worker = Worker{}
		s.primaryDirty = false
		s.primaryInUse = false
	}
	s.primaryLoading = !s.started
	s.cacheStatusLocked()
	defer func() { s.primaryLoading = false; s.notifyLocked(); s.cacheStatusLocked() }()
	s.lastPreloadCheck = m.now()
	startCtx, cancel := context.WithCancel(ctx)
	s.startMu.Lock()
	s.startCancel = cancel
	s.startMu.Unlock()
	defer func() {
		s.startMu.Lock()
		s.startCancel = nil
		s.startMu.Unlock()
		cancel()
	}()
	worker, err := m.workers.StartWorker(startCtx, channelSpec(s.info, s.info.Token))
	if err != nil {
		s.preloadError = err.Error()
		s.started = false
		s.worker = Worker{}
		s.cacheStatusLocked()
		return Worker{}, err
	}
	s.preloadError = ""
	s.worker = worker
	s.started = true
	s.cacheStatusLocked()
	if s.viewers == 0 {
		s.idleSince = m.now()
	}
	return worker, nil
}

// PreloadSaved restores renderers for links loaded from the store. The caller
// can run it after opening the public listener so slow pages do not delay the
// management UI. Failed links remain manageable.
func (m *SessionManager) PreloadSaved(ctx context.Context) error {
	if !m.keepPreloaded {
		return nil
	}
	var allErr error
	items := m.List()
	sort.SliceStable(items, func(i, j int) bool { return items[i].ReservedPreloads > items[j].ReservedPreloads })
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return errors.Join(allErr, err)
		}
		if item.RenderPaused {
			continue
		}
		if _, err := m.Warm(ctx, item.Token); err != nil {
			allErr = errors.Join(allErr, fmt.Errorf("preload %s: %w", item.Token, err))
		}
	}
	return allErr
}

// MaintainPreloads checks preloaded renderers periodically and retries failed
// ones. A failed page never removes its saved link.
func (m *SessionManager) MaintainPreloads(ctx context.Context) error {
	if !m.keepPreloaded || m.stopping.Load() {
		return nil
	}
	var allErr error
	for _, item := range m.List() {
		if ctx.Err() != nil {
			return errors.Join(allErr, ctx.Err())
		}
		m.mu.RLock()
		s := m.sessions[item.Token]
		m.mu.RUnlock()
		if s == nil {
			continue
		}
		s.mu.Lock()
		interval := time.Minute
		if !s.started || s.preloadError != "" || s.poolError != "" {
			interval = 30 * time.Second
		}
		refresh := s.published && !s.deleted && !s.info.RenderPaused && m.now().Sub(s.lastPreloadCheck) >= interval
		s.mu.Unlock()
		if refresh {
			if _, err := m.Warm(ctx, item.Token); err != nil {
				if errors.Is(err, ErrPreloadDeferred) {
					continue
				}
				allErr = errors.Join(allErr, fmt.Errorf("refresh preload %s: %w", item.Token, err))
			}
		}
	}
	return allErr
}

func (m *SessionManager) ReapOnce(ctx context.Context) error {
	m.mu.RLock()
	items := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		items = append(items, s)
	}
	m.mu.RUnlock()
	var allErr error
	for _, s := range items {
		s.mu.Lock()
		for token, inUse := range s.channels {
			if !inUse && s.standby[token].Token == "" && s.pendingChannels[token] == nil {
				if err := m.workers.Stop(ctx, token); err != nil {
					allErr = errors.Join(allErr, fmt.Errorf("reap channel %s: %w", token, err))
				} else {
					delete(s.channels, token)
					delete(s.standby, token)
				}
			}
		}
		removeHidden := false
		if s.started && s.viewers == 0 && (!m.keepPreloaded || !s.published || s.info.RenderPaused) && !s.idleSince.IsZero() && m.now().Sub(s.idleSince) >= m.idleTimeout {
			if err := m.workers.Stop(ctx, s.info.Token); err != nil {
				allErr = errors.Join(allErr, fmt.Errorf("reap %s: %w", s.info.Token, err))
			} else {
				s.started = false
				s.worker = Worker{}
				s.idleSince = time.Time{}
				removeHidden = !s.published
			}
		}
		if s.started && s.viewers == 0 && s.published && !s.info.RenderPaused && m.keepPreloaded {
			if activity, ok := m.workers.(WorkerActivityController); ok {
				if err := activity.SetActive(ctx, s.info.Token, false); err != nil {
					allErr = errors.Join(allErr, fmt.Errorf("idle render worker %s: %w", s.info.Token, err))
				}
			}
		}
		s.cacheStatusLocked()
		s.mu.Unlock()
		if removeHidden {
			m.mu.Lock()
			if m.sessions[s.info.Token] == s {
				delete(m.sessions, s.info.Token)
			}
			m.mu.Unlock()
		}
	}
	return allErr
}

func (m *SessionManager) RunReaper(ctx context.Context, interval, preloadTimeout time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if preloadTimeout <= 0 {
		preloadTimeout = 310 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := m.ReapOnce(reapCtx)
			cancel()
			if err != nil && logger != nil {
				logger.Error("idle worker cleanup failed", "error", err)
			}
			if m.keepPreloaded {
				preloadCtx, cancelPreload := context.WithTimeout(ctx, preloadTimeout)
				err = m.MaintainPreloads(preloadCtx)
				cancelPreload()
				if err != nil && logger != nil {
					logger.Error("preloaded worker refresh failed", "error", err)
				}
			}
		}
	}
}

// StopAll releases workers during a graceful gateway shutdown.
func (m *SessionManager) StopAll(ctx context.Context) error {
	m.stopping.Store(true)
	m.mu.RLock()
	items := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		items = append(items, s)
	}
	m.mu.RUnlock()
	var allErr error
	for _, s := range items {
		s.startMu.Lock()
		if s.startCancel != nil {
			s.startCancel()
		}
		s.startMu.Unlock()
		s.mu.Lock()
		if err := m.stopChannelsLocked(ctx, s); err != nil {
			allErr = errors.Join(allErr, err)
		}
		if s.started || s.primaryDirty {
			if err := m.workers.Stop(ctx, s.info.Token); err != nil {
				allErr = errors.Join(allErr, fmt.Errorf("stop %s: %w", s.info.Token, err))
			} else {
				s.started = false
				s.worker = Worker{}
				s.primaryInUse = false
				s.primaryDirty = false
				s.idleSince = time.Time{}
			}
		}
		s.cacheStatusLocked()
		s.mu.Unlock()
	}
	return allErr
}
