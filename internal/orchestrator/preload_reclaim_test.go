package orchestrator

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestViewerReclaimsOnlyUnreservedIdlePreloads(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			ports := channelTestPorts(t)
			workers := &leasedTestWorkers{ports: ports, active: make(map[string]PortLease)}
			m, _ := NewSessionManager(workers, time.Minute)
			m.SetKeepPreloaded(true)
			m.SetPerformanceSettingsSource(func() PerformanceSettings {
				return PerformanceSettings{GPUFPSBudget: 120, ReclaimIdlePreloads: enabled}
			})
			m.SetChannelAvailabilitySource(ports.Available)
			t.Cleanup(func() { _ = m.StopAll(context.Background()) })
			a, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/a", ConcurrencyMode: "multi", MaxViewers: 3, PreloadCount: 3, ReservedPreloads: 1})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Warm(context.Background(), a.Token); err != nil {
				t.Fatal(err)
			}
			b, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/b"})
			if err != nil {
				t.Fatal(err)
			}
			worker, release, err := m.Acquire(context.Background(), b.Token)
			if !enabled {
				if !errors.Is(err, ErrPortsExhausted) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || worker.Token != b.Token {
				t.Fatal(worker, err)
			}
			defer release()
			status, _ := m.Status(a.Token)
			if status.ReadyInstances != 2 || !status.PreloadSuspended {
				t.Fatalf("idle pool not yielded: %+v", status)
			}
			if err := m.MaintainPreloads(context.Background()); err != nil {
				t.Fatal(err)
			}
			if status, _ := m.Status(a.Token); status.ReadyInstances != 2 {
				t.Fatal("reclaimed slot immediately reloaded", status)
			}
			// Protect the remaining idle floor, and never evict the active viewer.
			c, _ := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/c"})
			_, releaseC, err := m.Acquire(context.Background(), c.Token)
			if err != nil {
				t.Fatal(err)
			}
			defer releaseC()
			d, _ := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/d"})
			if _, _, err := m.Acquire(context.Background(), d.Token); !errors.Is(err, ErrPortsExhausted) {
				t.Fatalf("reserved or active channel reclaimed: %v", err)
			}
			if status, _ := m.Status(a.Token); status.ReadyInstances != 1 {
				t.Fatal(status)
			}
		})
	}
}

func TestReclaimWaitingCanBeCancelledAndPoolResumesWithHeadroom(t *testing.T) {
	m, workers, a := newPoolTest(t)
	m.SetChannelAvailabilitySource(workers.ports.Available)
	if _, err := m.Warm(context.Background(), a.Token); err != nil {
		t.Fatal(err)
	}
	b, _ := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/b"})
	m.reclaimSlots <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, err := m.Acquire(ctx, b.Token)
	<-m.reclaimSlots
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("reclamation wait ignored cancellation", err)
	}
	if status, _ := m.Status(a.Token); status.ReadyInstances != 3 {
		t.Fatal("cancelled demand evicted a preload", status)
	}
	_, release, err := m.Acquire(context.Background(), b.Token)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := m.PauseRendering(context.Background(), b.Token); err != nil {
		t.Fatal(err)
	}
	// Real free capacity alone is insufficient before the cooldown.
	if err := m.MaintainPreloads(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status, _ := m.Status(a.Token); status.ReadyInstances != 2 {
		t.Fatal(status)
	}
	m.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if err := m.MaintainPreloads(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status, _ := m.Status(a.Token); status.ReadyInstances != 3 || status.PreloadSuspended {
		t.Fatal("yielded pool did not recover", status)
	}
}

func TestReclaimCleanupFailureRetainsOwnership(t *testing.T) {
	m, w, a := newPoolTest(t)
	if _, err := m.Warm(context.Background(), a.Token); err != nil {
		t.Fatal(err)
	}
	b, _ := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/b"})
	w.control.Lock()
	w.failStop = true
	w.control.Unlock()
	_, _, err := m.Acquire(context.Background(), b.Token)
	w.control.Lock()
	w.failStop = false
	w.control.Unlock()
	if err == nil {
		t.Fatal("failed cleanup admitted a new worker")
	}
	if got, _ := m.Status(a.Token); got.ReadyInstances != 3 || got.PreloadSuspended {
		t.Fatal("failed cleanup lost ownership", got)
	}
	if w.ports.Available() != 0 {
		t.Fatal("failed cleanup released a lease")
	}
}

func TestReservedPreloadsRestoreBeforeUnreservedPools(t *testing.T) {
	ports := channelTestPorts(t)
	w := &leasedTestWorkers{ports: ports, active: make(map[string]PortLease)}
	path := filepath.Join(t.TempDir(), "sessions.json")
	m, _ := NewSessionManager(w, time.Minute)
	if err := m.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	a, _ := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/a", ConcurrencyMode: "multi", MaxViewers: 3, PreloadCount: 3})
	b, _ := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/b", ReservedPreloads: 1})
	restored, _ := NewSessionManager(w, time.Minute)
	restored.SetKeepPreloaded(true)
	if err := restored.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.StopAll(context.Background()) })
	if err := restored.PreloadSaved(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status, _ := restored.Status(b.Token); status.ReadyInstances != 1 || status.ReservedPreloads != 1 {
		t.Fatal("priority preload was crowded out", status)
	}
	if status, _ := restored.Status(a.Token); status.ReadyInstances != 2 {
		t.Fatal(status)
	}
}
