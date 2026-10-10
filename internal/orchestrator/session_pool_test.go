package orchestrator

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type poolTestWorkers struct {
	*leasedTestWorkers
	control    sync.Mutex
	block      chan struct{}
	entered    chan WorkerSpec
	failStop   bool
	coldStarts int
	primary    string
}

func (w *poolTestWorkers) StartWorker(ctx context.Context, spec WorkerSpec) (Worker, error) {
	w.mu.Lock()
	_, hot := w.active[spec.Token]
	w.mu.Unlock()
	w.control.Lock()
	block, entered := w.block, w.entered
	if !hot {
		w.coldStarts++
	}
	w.control.Unlock()
	if !hot && spec.Token != w.primary && block != nil {
		if entered != nil {
			entered <- spec
		}
		select {
		case <-block:
		case <-ctx.Done():
			return Worker{}, ctx.Err()
		}
	}
	return w.leasedTestWorkers.StartWorker(ctx, spec)
}
func (w *poolTestWorkers) Stop(ctx context.Context, token string) error {
	w.control.Lock()
	fail := w.failStop
	w.control.Unlock()
	if fail {
		return errors.New("cleanup failed")
	}
	return w.leasedTestWorkers.Stop(ctx, token)
}
func newPoolTest(t *testing.T) (*SessionManager, *poolTestWorkers, SessionInfo) {
	t.Helper()
	w := &poolTestWorkers{leasedTestWorkers: &leasedTestWorkers{ports: channelTestPorts(t), active: make(map[string]PortLease)}}
	m, _ := NewSessionManager(w, time.Minute)
	m.SetKeepPreloaded(true)
	m.SetPreloadContext(context.Background(), 2*time.Second)
	info, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/3d", ConcurrencyMode: "multi", MaxViewers: 6, PreloadCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.StopAll(context.Background()); err != nil {
			t.Error(err)
		}
	})
	w.primary = info.Token
	return m, w, info
}
func waitPool(t *testing.T, m *SessionManager, token string, ready int) SessionStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, _ := m.Status(token)
		if status.ReadyInstances == ready && status.StartingInstances == 0 {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	status, _ := m.Status(token)
	t.Fatalf("pool did not reach %d ready: %+v", ready, status)
	return status
}
func TestPreloadPoolReadyViewersUseThreeExistingIndependentWorkers(t *testing.T) {
	m, w, info := newPoolTest(t)
	if _, err := m.Warm(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	status := waitPool(t, m, info.Token, 3)
	if status.AvailableInstances != 3 || status.Viewers != 0 {
		t.Fatalf("unexpected ready pool: %+v", status)
	}
	tokens := make(map[string]bool)
	for range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		worker, release, err := m.Acquire(ctx, info.Token)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if tokens[worker.Token] {
			t.Fatal("two viewers own one worker")
		}
		tokens[worker.Token] = true
		t.Cleanup(release)
	}
	if _, err := m.Warm(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	w.control.Lock()
	starts := w.coldStarts
	w.control.Unlock()
	if starts != 3 {
		t.Fatalf("warm viewers triggered cold startup: %d", starts)
	}
	if _, _, err := m.Acquire(context.Background(), info.Token); !errors.Is(err, ErrPortsExhausted) {
		t.Fatalf("shared pool admission not enforced: %v", err)
	}
	if status, _ = m.Status(info.Token); status.Viewers != 3 || status.ReadyInstances != 3 || status.AvailableInstances != 0 {
		t.Fatalf("unexpected busy pool: %+v", status)
	}
}

func TestPreloadThreeOfSixStartsFourthThroughSixthOnDemand(t *testing.T) {
	m, w, info := newPoolTest(t)
	settings, err := NewChannelSettings(filepath.Join(t.TempDir(), "channels.json"), w.ports)
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.SetMaxChannels(6); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Warm(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	if status := waitPool(t, m, info.Token, 3); status.AvailableInstances != 3 {
		t.Fatalf("first three workers were not preloaded: %+v", status)
	}
	tokens := make(map[string]bool)
	for viewer := 1; viewer <= 6; viewer++ {
		worker, release, err := m.Acquire(context.Background(), info.Token)
		if err != nil {
			t.Fatalf("viewer %d could not enter: %v", viewer, err)
		}
		if tokens[worker.Token] {
			t.Fatalf("viewer %d shared worker %s", viewer, worker.Token)
		}
		tokens[worker.Token] = true
		t.Cleanup(release)
		w.control.Lock()
		starts := w.coldStarts
		w.control.Unlock()
		if starts != max(3, viewer) {
			t.Fatalf("viewer %d caused %d cold starts, want %d", viewer, starts, max(3, viewer))
		}
	}
	if _, _, err := m.Acquire(context.Background(), info.Token); !errors.Is(err, ErrViewerLimit) {
		t.Fatalf("seventh viewer exceeded six-session quota: %v", err)
	}
	if got := settings.Settings(); got.ActiveChannels != 6 {
		t.Fatalf("expected six independent channels: %+v", got)
	}
}
func TestPreloadPoolSlowExtraDoesNotBlockFirstReadyViewerAndPauseCancels(t *testing.T) {
	m, w, info := newPoolTest(t)
	w.control.Lock()
	w.block = make(chan struct{})
	w.entered = make(chan WorkerSpec, 1)
	entered := w.entered
	w.control.Unlock()
	done := make(chan error, 1)
	go func() { _, err := m.Warm(context.Background(), info.Token); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("extra preload did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	first, release, err := m.Acquire(ctx, info.Token)
	cancel()
	if err != nil || first.Token != info.Token {
		t.Fatalf("first ready route blocked: %v", err)
	}
	defer release()
	if status, _ := m.Status(info.Token); status.StartingInstances != 1 || status.ReadyInstances != 1 {
		t.Fatalf("missing loading status: %+v", status)
	}
	pauseCtx, pauseCancel := context.WithTimeout(context.Background(), time.Second)
	defer pauseCancel()
	if err := m.PauseRendering(pauseCtx, info.Token); err != nil {
		t.Fatal(err)
	}
	<-done
	if len(w.active) != 0 {
		t.Fatal("pause leaked a startup lease")
	}
	if status, _ := m.Status(info.Token); !status.RenderPaused || status.StartingInstances != 0 || status.ReadyInstances != 0 {
		t.Fatalf("unexpected stopped pool: %+v", status)
	}
}
func TestPreloadPoolReleaseResetsAndReusesIsolatedProfileSlot(t *testing.T) {
	m, w, info := newPoolTest(t)
	if _, err := m.Warm(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	_, releaseFirst, err := m.Acquire(context.Background(), info.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseFirst()
	second, releaseSecond, err := m.Acquire(context.Background(), info.Token)
	if err != nil {
		t.Fatal(err)
	}
	releaseSecond()
	releaseSecond()
	status := waitPool(t, m, info.Token, 3)
	if status.Viewers != 1 || status.AvailableInstances != 2 {
		t.Fatalf("release counted twice or pool not replenished: %+v", status)
	}
	m.mu.RLock()
	s := m.sessions[info.Token]
	m.mu.RUnlock()
	s.mu.Lock()
	_, cached := s.standby[second.Token]
	s.mu.Unlock()
	if !cached {
		t.Fatal("replacement lost its isolated profile/cache slot")
	}
	w.control.Lock()
	starts := w.coldStarts
	w.control.Unlock()
	if starts != 4 {
		t.Fatalf("released scene was not reset exactly once: %d", starts)
	}
}
func TestPreloadPoolPartialCapacityKeepsReadyPrimaryAndPersistsTarget(t *testing.T) {
	m, w, info := newPoolTest(t)
	settings, err := NewChannelSettings(filepath.Join(t.TempDir(), "channels.json"), w.ports)
	if err != nil {
		t.Fatal(err)
	}
	if err = settings.SetMaxChannels(1); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(t.TempDir(), "sessions.json")
	m.storePath = store
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Warm(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	status, _ := m.Status(info.Token)
	if status.ReadyInstances != 1 || status.PoolError == "" || status.StartingInstances != 0 {
		t.Fatalf("partial capacity hidden: %+v", status)
	}
	restored, _ := NewSessionManager(&fakeWorkers{}, time.Minute)
	if err := restored.EnableStore(store); err != nil {
		t.Fatal(err)
	}
	loaded, ok := restored.Lookup(info.Token)
	if !ok || loaded.PreloadCount != 3 || loaded.MaxViewers != 6 {
		t.Fatalf("preload target or quota lost: %+v", loaded)
	}
	if err = settings.SetMaxChannels(3); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Warm(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	if status = waitPool(t, m, info.Token, 3); status.PoolError != "" {
		t.Fatalf("recovery retained failure: %+v", status)
	}
}
func TestPreloadPoolDeleteAndShutdownWaitForCanceledExtra(t *testing.T) {
	for _, operation := range []string{"delete", "shutdown", "update"} {
		t.Run(operation, func(t *testing.T) {
			m, w, info := newPoolTest(t)
			w.control.Lock()
			w.block = make(chan struct{})
			w.entered = make(chan WorkerSpec, 1)
			entered := w.entered
			w.control.Unlock()
			done := make(chan error, 1)
			go func() { _, err := m.Warm(context.Background(), info.Token); done <- err }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("extra preload did not start")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var err error
			switch operation {
			case "delete":
				err = m.Delete(ctx, info.Token)
			case "shutdown":
				err = m.StopAll(ctx)
			case "update":
				_, _, err = m.Update(ctx, info.Token, SessionOptions{TargetURL: "https://example.com/new", ConcurrencyMode: "multi", MaxViewers: 6, PreloadCount: 3})
			}
			if err != nil {
				t.Fatal(err)
			}
			<-done
			if len(w.active) != 0 {
				t.Fatalf("%s leaked an allocated worker", operation)
			}
		})
	}
}
