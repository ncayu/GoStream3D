package orchestrator

import (
	"context"
	"errors"
	"sort"
	"time"
)

var ErrPreloadDeferred = errors.New("idle preload deferred for viewer demand")

func (m *SessionManager) SetPerformanceSettingsSource(source func() PerformanceSettings) {
	m.settingsMu.Lock()
	defer m.settingsMu.Unlock()
	m.performanceSource = source
}
func (m *SessionManager) PerformanceSettings() PerformanceSettings {
	m.settingsMu.RLock()
	source := m.performanceSource
	m.settingsMu.RUnlock()
	if source != nil {
		return source()
	}
	return DefaultPerformanceSettings()
}
func (m *SessionManager) SetChannelAvailabilitySource(source func() int) {
	m.settingsMu.Lock()
	defer m.settingsMu.Unlock()
	m.availabilitySource = source
}

// Call with s.mu held. A yielded pool waits at least a minute and for real
// allocator headroom. Background maintenance never evicts another project.
func (m *SessionManager) deferPreloadLocked(s *session) bool {
	if m.viewerDemand.Load() > 0 {
		return true
	}
	if s.preloadSuspendedUntil.IsZero() {
		return false
	}
	if m.now().Before(s.preloadSuspendedUntil) {
		return true
	}
	m.settingsMu.RLock()
	source := m.availabilitySource
	m.settingsMu.RUnlock()
	if source != nil && source() <= 0 {
		return true
	}
	s.preloadSuspendedUntil = time.Time{}
	s.cacheStatusLocked()
	return false
}

func (m *SessionManager) CanReclaimIdle(token string) bool {
	if !m.PerformanceSettings().ReclaimIdlePreloads {
		return false
	}
	for _, status := range m.List() {
		if status.Token != token && status.Viewers == 0 && status.StartingInstances == 0 &&
			status.AvailableInstances > status.ReservedPreloads {
			return true
		}
	}
	return false
}

// Only actual viewer admission may reclaim, after the allocator reports
// exhaustion. The normal cold-start path retries without holding a target
// session lock while inspecting other projects.
func (m *SessionManager) Acquire(ctx context.Context, token string) (Worker, func(), error) {
	m.viewerDemand.Add(1)
	defer m.viewerDemand.Add(-1)
	for attempts := 0; ; attempts++ {
		worker, release, err := m.acquire(ctx, token)
		if !errors.Is(err, ErrPortsExhausted) || attempts >= 100 || !m.PerformanceSettings().ReclaimIdlePreloads {
			return worker, release, err
		}
		if err := ctx.Err(); err != nil {
			return Worker{}, nil, err
		}
		freed, reclaimErr := m.reclaimIdlePreload(ctx, token)
		if !freed {
			return Worker{}, nil, errors.Join(err, reclaimErr)
		}
	}
}

func (m *SessionManager) reclaimIdlePreload(ctx context.Context, target string) (bool, error) {
	select {
	case m.reclaimSlots <- struct{}{}:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	defer func() { <-m.reclaimSlots }()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	m.mu.RLock()
	items := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		items = append(items, s)
	}
	m.mu.RUnlock()
	// Oldest idle projects yield first; tie-breaking makes reclamation predictable.
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].cachedStatus.Load(), items[j].cachedStatus.Load()
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		if a.IdleSince.Equal(b.IdleSince) {
			return a.Token < b.Token
		}
		return a.IdleSince.Before(b.IdleSince)
	})
	var allErr error
	for _, s := range items {
		if !s.lifecycleMu.TryLock() {
			continue
		}
		if !s.mu.TryLock() {
			s.lifecycleMu.Unlock()
			continue
		}
		total := len(s.standby)
		if s.started {
			total++
		}
		eligible := s.info.Token != target && !s.deleted && s.published && !s.transitioning &&
			s.viewers == 0 && !s.primaryInUse && !s.primaryLoading && !s.primaryDirty && len(s.pendingChannels) == 0 && total > s.info.ReservedPreloads
		if !eligible {
			s.mu.Unlock()
			s.lifecycleMu.Unlock()
			continue
		}
		tokens := make([]string, 0, len(s.standby))
		for token := range s.standby {
			tokens = append(tokens, token)
		}
		sort.Strings(tokens)
		victim := s.info.Token
		if len(tokens) > 0 {
			victim = tokens[0]
		}
		stopCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := m.workers.Stop(stopCtx, victim)
		cancel()
		if err == nil {
			if victim == s.info.Token {
				s.started = false
				s.worker = Worker{}
				s.idleSince = time.Time{}
			} else {
				delete(s.standby, victim)
				delete(s.channels, victim)
			}
			s.preloadSuspendedUntil = m.now().Add(time.Minute)
			s.preloadError = ""
			s.poolError = "空闲预加载已让出通道，将在资源空闲后恢复"
			s.notifyLocked()
			s.cacheStatusLocked()
		} else {
			allErr = errors.Join(allErr, err)
		}
		s.mu.Unlock()
		s.lifecycleMu.Unlock()
		if err == nil {
			return true, nil
		}
	}
	return false, allErr
}
