package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

func (s *session) notifyLocked() {
	if s.changed != nil {
		close(s.changed)
	}
	s.changed = make(chan struct{})
}

func (m *SessionManager) warmPrimary(ctx context.Context, s *session) (Worker, error) {
	waited := false
	for {
		s.mu.Lock()
		if s.deleted || !s.published || m.stopping.Load() {
			s.mu.Unlock()
			return Worker{}, ErrSessionNotFound
		}
		if s.info.RenderPaused {
			s.mu.Unlock()
			return Worker{}, ErrRenderPaused
		}
		if ctx.Err() != nil {
			s.mu.Unlock()
			return Worker{}, ctx.Err()
		}
		if m.deferPreloadLocked(s) {
			s.mu.Unlock()
			return Worker{}, ErrPreloadDeferred
		}
		if s.primaryLoading || s.transitioning {
			if s.changed == nil {
				s.changed = make(chan struct{})
			}
			changed := s.changed
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return Worker{}, ctx.Err()
			case <-changed:
			}
			waited = true
			continue
		}
		// Never probe/restart the primary while a viewer is interacting with it.
		if s.started && !s.primaryDirty && (s.primaryInUse || waited) {
			worker := s.worker
			s.lastPreloadCheck = m.now()
			s.mu.Unlock()
			return worker, nil
		}
		if s.primaryDirty {
			if err := m.workers.Stop(ctx, s.info.Token); err != nil {
				s.mu.Unlock()
				return Worker{}, err
			}
			s.started, s.primaryDirty, s.primaryInUse = false, false, false
			s.worker = Worker{}
		}
		s.primaryLoading = true
		s.lastPreloadCheck = m.now()
		startCtx, cancel := context.WithCancel(ctx)
		s.startMu.Lock()
		s.startCancel = cancel
		s.startMu.Unlock()
		spec := channelSpec(s.info, s.info.Token)
		s.notifyLocked()
		s.cacheStatusLocked()
		s.mu.Unlock()
		worker, err := m.workers.StartWorker(startCtx, spec)
		s.mu.Lock()
		if err == nil {
			err = startCtx.Err()
		}
		if err == nil && (s.deleted || s.info.RenderPaused || s.transitioning || m.stopping.Load()) {
			err = context.Canceled
		}
		s.mu.Unlock()
		var stopErr error
		if err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			stopErr = m.workers.Stop(cleanupCtx, spec.Token)
			cleanupCancel()
		}
		s.mu.Lock()
		s.startMu.Lock()
		s.startCancel = nil
		s.startMu.Unlock()
		cancel()
		s.primaryLoading = false
		if err == nil {
			s.started = true
			s.worker = worker
			s.preloadError = ""
			if s.viewers == 0 {
				s.idleSince = m.now()
			}
		} else {
			s.started = stopErr != nil
			s.primaryDirty = stopErr != nil
			s.worker = Worker{}
			if !s.info.RenderPaused && !s.deleted {
				s.preloadError = errors.Join(err, stopErr).Error()
			}
		}
		s.notifyLocked()
		s.cacheStatusLocked()
		s.mu.Unlock()
		return worker, errors.Join(err, stopErr)
	}
}

func channelSpec(info SessionInfo, token string) WorkerSpec {
	resolution := info.Resolution
	if resolution == "adaptive" {
		resolution = "1920x1080" // Prewarm before a viewer supplies its viewport.
	}
	return WorkerSpec{Token: token, ProjectToken: info.Token, FramePolicy: info.FramePolicy, TargetURL: info.TargetURL, Screen: fmt.Sprintf("%s@%d", resolution, info.FPS), Adaptive: info.Resolution == "adaptive", RenderMode: info.RenderMode, ReadySelector: info.ReadySelector}
}

func (s *session) reserveChannelLocked(ctx context.Context, token string, owned bool) (context.Context, WorkerSpec) {
	if s.channels == nil {
		s.channels = make(map[string]bool)
	}
	if s.pendingChannels == nil {
		s.pendingChannels = make(map[string]context.CancelFunc)
	}
	startCtx, cancel := context.WithCancel(ctx)
	s.channels[token], s.pendingChannels[token] = owned, cancel
	s.notifyLocked()
	s.cacheStatusLocked()
	return startCtx, channelSpec(s.info, token)
}

// Extra browsers start without holding the session lock. A ready first channel
// remains available while expensive additional 3D scenes are initialized.
func (m *SessionManager) startChannel(ctx context.Context, s *session, spec WorkerSpec) (Worker, error) {
	worker, err := m.workers.StartWorker(ctx, spec)
	s.mu.Lock()
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && (s.deleted || s.info.RenderPaused || s.transitioning || m.stopping.Load()) {
		err = context.Canceled
	}
	s.mu.Unlock()
	var stopErr error
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		stopErr = m.workers.Stop(cleanupCtx, spec.Token)
		cancel()
	}
	s.mu.Lock()
	owned := s.channels[spec.Token]
	if cancel := s.pendingChannels[spec.Token]; cancel != nil {
		cancel()
	}
	delete(s.pendingChannels, spec.Token)
	if err != nil {
		if stopErr == nil {
			delete(s.channels, spec.Token)
		} else {
			s.channels[spec.Token] = false
		}
		delete(s.standby, spec.Token)
		if owned && s.viewers > 0 {
			s.viewers--
		}
	} else if !owned {
		if s.standby == nil {
			s.standby = make(map[string]Worker)
		}
		s.standby[spec.Token] = worker
	}
	s.notifyLocked()
	s.cacheStatusLocked()
	s.mu.Unlock()
	return worker, errors.Join(err, stopErr)
}

// PreloadCount is a minimum resident pool, including workers currently owned
// by viewers. It never means shared input, or N additional idle workers.
func (m *SessionManager) warmPool(ctx context.Context, s *session) {
	if !s.poolMu.TryLock() {
		return
	}
	defer s.poolMu.Unlock()
	for {
		s.mu.Lock()
		if ctx.Err() != nil || m.stopping.Load() || s.deleted || !s.published || s.info.RenderPaused || s.transitioning {
			s.mu.Unlock()
			return
		}
		if m.deferPreloadLocked(s) {
			s.mu.Unlock()
			return
		}
		target := s.info.PreloadCount
		// A worker whose previous cleanup failed is not a reusable ready slot.
		for token, owned := range s.channels {
			if !owned && s.pendingChannels[token] == nil && s.standby[token].Token == "" {
				if err := m.workers.Stop(ctx, token); err != nil {
					s.poolError = err.Error()
					s.cacheStatusLocked()
					s.mu.Unlock()
					return
				}
				delete(s.channels, token)
			}
		}
		total := len(s.channels)
		if s.started {
			total++
		}
		// Trim only unowned, ready standby instances when the configured target falls.
		if total > target {
			tokens := make([]string, 0, len(s.standby))
			for token := range s.standby {
				tokens = append(tokens, token)
			}
			sort.Strings(tokens)
			for _, token := range tokens {
				if total <= target {
					break
				}
				if err := m.workers.Stop(ctx, token); err != nil {
					s.poolError = err.Error()
					s.cacheStatusLocked()
					s.mu.Unlock()
					return
				}
				delete(s.channels, token)
				delete(s.standby, token)
				total--
			}
		}
		if total >= target {
			s.poolError = ""
			s.notifyLocked()
			s.cacheStatusLocked()
			s.mu.Unlock()
			return
		}
		var token string
		var err error
		if n := len(s.reusableChannels); n > 0 {
			token = s.reusableChannels[n-1]
			s.reusableChannels = s.reusableChannels[:n-1]
		} else {
			token, err = newChannelToken(s.info.Token)
		}
		if err != nil {
			s.poolError = err.Error()
			s.cacheStatusLocked()
			s.mu.Unlock()
			return
		}
		startCtx, spec := s.reserveChannelLocked(ctx, token, false)
		s.mu.Unlock()
		if _, err = m.startChannel(startCtx, s, spec); err != nil {
			s.mu.Lock()
			if !s.info.RenderPaused && !s.deleted {
				if errors.Is(err, ErrPortsExhausted) {
					s.poolError = "服务器总通道数不足，部分预加载实例尚未就绪"
				} else {
					s.poolError = err.Error()
				}
			}
			s.cacheStatusLocked()
			s.mu.Unlock()
			return
		}
	}
}

func (m *SessionManager) replenish(s *session) {
	if !m.keepPreloaded || m.stopping.Load() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(m.preloadContext, m.preloadTimeout)
		defer cancel()
		s.mu.Lock()
		token := s.info.Token
		valid := !s.deleted && !s.info.RenderPaused && s.info.PreloadCount > 1
		s.mu.Unlock()
		if valid {
			_, _ = m.Warm(ctx, token)
		}
	}()
}

func (m *SessionManager) releaseViewer(s *session, token string, primary bool) {
	s.mu.Lock()
	refresh := m.keepPreloaded && s.info.PreloadCount > 1 && !s.deleted && !s.info.RenderPaused && !m.stopping.Load()
	if primary {
		s.primaryInUse = false
		if refresh {
			// Restart the process before it becomes eligible for another viewer. Its
			// isolated disk cache remains reusable, but previous navigation/input does not.
			s.primaryDirty = true
			s.primaryInUse = true
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			if err := m.workers.Stop(cleanupCtx, token); err == nil {
				s.started = false
				s.worker = Worker{}
				s.primaryDirty = false
				s.primaryInUse = false
			} else {
				s.poolError = err.Error()
			}
			cancel()
		}
	} else if _, exists := s.channels[token]; exists {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := m.workers.Stop(cleanupCtx, token); err == nil {
			delete(s.channels, token)
			delete(s.standby, token)
			if refresh {
				s.reusableChannels = append(s.reusableChannels, token)
			}
		} else {
			s.channels[token] = false
			delete(s.standby, token)
			s.poolError = err.Error()
		}
		cancel()
	}
	if s.viewers > 0 {
		s.viewers--
	}
	if s.viewers == 0 {
		s.idleSince = m.now()
	}
	s.notifyLocked()
	s.cacheStatusLocked()
	s.mu.Unlock()
	if refresh {
		m.replenish(s)
	}
}
