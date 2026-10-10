package orchestrator

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeWorkers struct {
	mu       sync.Mutex
	starts   int
	stops    int
	startErr error
}

type blockingWorkers struct {
	entered chan struct{}
	release chan struct{}
}

func (w *blockingWorkers) StartWorker(ctx context.Context, spec WorkerSpec) (Worker, error) {
	close(w.entered)
	select {
	case <-w.release:
		return Worker{Token: spec.Token}, nil
	case <-ctx.Done():
		return Worker{}, ctx.Err()
	}
}

func (*blockingWorkers) Stop(context.Context, string) error { return nil }

func TestListRemainsAvailableDuringSlowPreload(t *testing.T) {
	workers := &blockingWorkers{entered: make(chan struct{}), release: make(chan struct{})}
	m, err := NewSessionManager(workers, 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	info, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/scene"})
	if err != nil {
		t.Fatal(err)
	}
	warmDone := make(chan error, 1)
	go func() { _, err := m.Warm(context.Background(), info.Token); warmDone <- err }()
	<-workers.entered
	statusDone := make(chan SessionStatus, 1)
	go func() { status, _ := m.Status(info.Token); statusDone <- status }()
	select {
	case status := <-statusDone:
		if status.Token != info.Token || status.Running {
			t.Fatalf("unexpected status during preload: %+v", status)
		}
	case <-time.After(time.Second):
		close(workers.release)
		<-warmDone
		t.Fatal("public session status blocked on browser preload")
	}
	listDone := make(chan []SessionStatus, 1)
	go func() { listDone <- m.List() }()
	select {
	case list := <-listDone:
		if len(list) != 1 || list[0].Token != info.Token || list[0].Running {
			t.Fatalf("unexpected status during preload: %+v", list)
		}
	case <-time.After(time.Second):
		close(workers.release)
		<-warmDone
		t.Fatal("admin session list blocked on browser preload")
	}
	close(workers.release)
	if err := <-warmDone; err != nil {
		t.Fatal(err)
	}
	if list := m.List(); len(list) != 1 || !list[0].Running {
		t.Fatalf("preloaded status was not refreshed: %+v", list)
	}
}

func (f *fakeWorkers) StartWorker(_ context.Context, spec WorkerSpec) (Worker, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	if f.startErr != nil {
		return Worker{}, f.startErr
	}
	return Worker{Token: spec.Token, Screen: spec.Screen, RenderMode: spec.RenderMode}, nil
}

func TestCreatePrewarmedFailureDoesNotPublishLink(t *testing.T) {
	workers := &fakeWorkers{startErr: errors.New("container failed")}
	m, err := NewSessionManager(workers, 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.CreatePrewarmed(context.Background(), SessionOptions{TargetURL: "https://example.com/scene"})
	if err == nil {
		t.Fatal("expected preload failure")
	}
	if len(m.sessions) != 0 {
		t.Fatal("failed preload left a published session")
	}
}

func (f *fakeWorkers) Stop(_ context.Context, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return nil
}

func TestUpdateLinkPreservesAddressAndSavedPause(t *testing.T) {
	workers := &fakeWorkers{}
	m, err := NewSessionManager(workers, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(t.TempDir(), "sessions.json")
	if err := m.EnableStore(store); err != nil {
		t.Fatal(err)
	}
	info, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/old", Name: "Old"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Warm(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	if err := m.PauseRendering(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	updated, shouldWarm, err := m.Update(context.Background(), info.Token, SessionOptions{
		TargetURL: "https://example.com/new", Name: "New", Resolution: "1280x720", FPS: 60,
		RenderMode: "nvidia", ConcurrencyMode: "single",
	})
	if err != nil {
		t.Fatal(err)
	}
	if shouldWarm || updated.Token != info.Token || !updated.CreatedAt.Equal(info.CreatedAt) || !updated.RenderPaused {
		t.Fatalf("link identity or pause changed: %+v, warm=%v", updated, shouldWarm)
	}
	reloaded, err := NewSessionManager(&fakeWorkers{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.EnableStore(store); err != nil {
		t.Fatal(err)
	}
	saved, ok := reloaded.Lookup(info.Token)
	if !ok || saved.TargetURL != "https://example.com/new" || saved.Name != "New" || saved.FPS != 60 || !saved.RenderPaused {
		t.Fatalf("edited link was not persisted: %+v", saved)
	}
}

func TestUpdateRejectsActiveViewerAndRestartsOnlyForRenderChanges(t *testing.T) {
	workers := &fakeWorkers{}
	m, err := NewSessionManager(workers, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	info, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/scene"})
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := m.Acquire(context.Background(), info.Token)
	if err != nil {
		t.Fatal(err)
	}
	options := SessionOptions{TargetURL: "https://example.com/scene", Name: "Edited"}
	if _, _, err := m.Update(context.Background(), info.Token, options); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("expected busy, got %v", err)
	}
	release()
	_, shouldWarm, err := m.Update(context.Background(), info.Token, options)
	if err != nil || shouldWarm || workers.stops != 0 {
		t.Fatalf("metadata edit restarted worker: warm=%v, stops=%d, err=%v", shouldWarm, workers.stops, err)
	}
	options.FPS = 60
	_, shouldWarm, err = m.Update(context.Background(), info.Token, options)
	if err != nil || !shouldWarm || workers.stops != 1 {
		t.Fatalf("render edit did not stop worker: warm=%v, stops=%d, err=%v", shouldWarm, workers.stops, err)
	}
}

type activityWorkers struct {
	fakeWorkers
	states []bool
}

func (w *activityWorkers) SetActive(_ context.Context, _ string, active bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.states = append(w.states, active)
	return nil
}

func TestPreloadedWorkerSleepsWithoutViewersAndWakesOnAcquire(t *testing.T) {
	workers := &activityWorkers{}
	m, err := NewSessionManager(workers, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	m.SetKeepPreloaded(true)
	info, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/scene"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Warm(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	if err := m.ReapOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, release, err := m.Acquire(context.Background(), info.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ReapOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	release()
	if err := m.ReapOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	workers.mu.Lock()
	defer workers.mu.Unlock()
	if len(workers.states) != 3 || workers.states[0] || !workers.states[1] || workers.states[2] {
		t.Fatalf("unexpected activity changes: %v", workers.states)
	}
	if workers.stops != 0 {
		t.Fatalf("preloaded worker was stopped: %d", workers.stops)
	}
}

func TestSessionIdleReapAndReconnect(t *testing.T) {
	workers := &fakeWorkers{}
	m, err := NewSessionManager(workers, 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m.now = func() time.Time { return now }
	info, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/3d", Resolution: "1920x1080", FPS: 30, ConcurrencyMode: "multi", MaxViewers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Lookup(info.Token); !ok {
		t.Fatal("created token not found")
	}
	_, releaseFirst, err := m.Acquire(context.Background(), info.Token)
	if err != nil {
		t.Fatal(err)
	}
	_, releaseSecond, err := m.Acquire(context.Background(), info.Token)
	if err != nil {
		t.Fatal(err)
	}
	releaseFirst()
	now = now.Add(200 * time.Second)
	if err := m.ReapOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if workers.stops != 0 {
		t.Fatal("worker was reaped with one active viewer")
	}
	releaseSecond()
	if workers.stops != 1 {
		t.Fatalf("extra channel should stop on disconnect, got %d stops", workers.stops)
	}
	now = now.Add(179 * time.Second)
	if err := m.ReapOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if workers.stops != 1 {
		t.Fatal("worker was reaped before idle timeout")
	}
	now = now.Add(time.Second)
	if err := m.ReapOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if workers.stops != 2 {
		t.Fatalf("want two independent workers stopped, got %d", workers.stops)
	}
	if _, ok := m.Lookup(info.Token); !ok {
		t.Fatal("short link was deleted during worker reap")
	}
}

func TestMultiChannelIsolationAndCapacity(t *testing.T) {
	workers := &fakeWorkers{}
	m, err := NewSessionManager(workers, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	m.SetKeepPreloaded(true)
	m.SetChannelCapacity(3)
	if _, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/future-capacity", ConcurrencyMode: "multi", MaxViewers: 10}); err != nil {
		t.Fatalf("project quota above configured host capacity should be saved: %v", err)
	}
	info, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/3d", ConcurrencyMode: "multi", MaxViewers: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Warm(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	first, releaseFirst, err := m.Acquire(context.Background(), info.Token)
	if err != nil {
		t.Fatal(err)
	}
	second, releaseSecond, err := m.Acquire(context.Background(), info.Token)
	if err != nil {
		t.Fatal(err)
	}
	third, releaseThird, err := m.Acquire(context.Background(), info.Token)
	if err != nil {
		t.Fatal(err)
	}
	if first.Token == second.Token || first.Token == third.Token || second.Token == third.Token {
		t.Fatalf("viewers received shared workers: %q %q %q", first.Token, second.Token, third.Token)
	}
	if _, _, err := m.Acquire(context.Background(), info.Token); !errors.Is(err, ErrViewerLimit) {
		t.Fatalf("expected per-link channel quota, got %v", err)
	}
	status, ok := m.Status(info.Token)
	if !ok || status.Viewers != 3 || status.RunningInstances != 3 {
		t.Fatalf("incorrect active channel accounting: %+v", status)
	}
	releaseSecond()
	status, _ = m.Status(info.Token)
	if status.Viewers != 2 || status.RunningInstances != 2 || workers.stops != 1 {
		t.Fatalf("secondary worker was not released: %+v, stops=%d", status, workers.stops)
	}
	replacement, releaseReplacement, err := m.Acquire(context.Background(), info.Token)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Token == second.Token || replacement.Token == first.Token || replacement.Token == third.Token {
		t.Fatalf("replacement channel was reused across viewers: %q", replacement.Token)
	}
	releaseReplacement()
	releaseThird()
	releaseFirst()
	status, _ = m.Status(info.Token)
	if status.Viewers != 0 || status.RunningInstances != 1 {
		t.Fatalf("preloaded primary worker should remain ready: %+v", status)
	}
}

// leasedTestWorkers exercises actual pool admission without starting browsers.
// Existing primaries retain one lease while extra viewer workers are released.
type leasedTestWorkers struct {
	mu     sync.Mutex
	ports  *PortAllocator
	active map[string]PortLease
}

func (w *leasedTestWorkers) StartWorker(_ context.Context, spec WorkerSpec) (Worker, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	lease, ok := w.active[spec.Token]
	if !ok {
		var err error
		lease, err = w.ports.Allocate()
		if err != nil {
			return Worker{}, err
		}
		w.active[spec.Token] = lease
	}
	return Worker{Token: spec.Token, CDPPort: lease.CDPPort, Screen: spec.Screen, RenderMode: spec.RenderMode}, nil
}

func (w *leasedTestWorkers) Stop(_ context.Context, token string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if lease, ok := w.active[token]; ok {
		lease.Release()
		delete(w.active, token)
	}
	return nil
}

func TestProjectQuotaPersistsAboveCapacityAndExpandedPoolAdmitsFourthViewer(t *testing.T) {
	ports := channelTestPorts(t)
	settings, err := NewChannelSettings(filepath.Join(t.TempDir(), "channel-settings.json"), ports)
	if err != nil {
		t.Fatal(err)
	}
	workers := &leasedTestWorkers{ports: ports, active: make(map[string]PortLease)}
	m, err := NewSessionManager(workers, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	m.SetChannelCapacitySource(ports.Capacity)
	store := filepath.Join(t.TempDir(), "sessions.json")
	if err := m.EnableStore(store); err != nil {
		t.Fatal(err)
	}
	info, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/3d", RenderMode: "nvidia", ConcurrencyMode: "multi", MaxViewers: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Update(context.Background(), info.Token, SessionOptions{TargetURL: info.TargetURL, RenderMode: "nvidia", ConcurrencyMode: "multi", MaxViewers: 8, PreloadCount: 9}); !errors.Is(err, ErrInvalidSessionOptions) {
		t.Fatalf("preload count above project quota accepted: %v", err)
	}
	if unchanged, _ := m.Lookup(info.Token); unchanged.MaxViewers != 10 || unchanged.PreloadCount != 1 {
		t.Fatalf("invalid preload update changed stored configuration: %+v", unchanged)
	}
	updated, _, err := m.Update(context.Background(), info.Token, SessionOptions{TargetURL: info.TargetURL, RenderMode: "nvidia", ConcurrencyMode: "multi", MaxViewers: 8})
	if err != nil || updated.MaxViewers != 8 || updated.Token != info.Token {
		t.Fatalf("quota update above server capacity failed: %+v, %v", updated, err)
	}
	reloaded, err := NewSessionManager(workers, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.SetChannelCapacitySource(ports.Capacity)
	if err := reloaded.EnableStore(store); err != nil {
		t.Fatal(err)
	}
	saved, ok := reloaded.Lookup(info.Token)
	if !ok || saved.MaxViewers != 8 {
		t.Fatalf("custom quota was not restored: %+v", saved)
	}
	t.Cleanup(func() {
		if err := reloaded.StopAll(context.Background()); err != nil {
			t.Error(err)
		}
	})
	seenTokens, seenPorts := map[string]bool{}, map[int]bool{}
	var releases []func()
	acquire := func() {
		t.Helper()
		worker, release, err := reloaded.Acquire(context.Background(), info.Token)
		if err != nil {
			t.Fatal(err)
		}
		if seenTokens[worker.Token] || seenPorts[worker.CDPPort] {
			t.Fatalf("viewers shared a browser/input channel: %+v", worker)
		}
		seenTokens[worker.Token], seenPorts[worker.CDPPort] = true, true
		releases = append(releases, release)
	}
	for range 3 {
		acquire()
	}
	if _, _, err := reloaded.Acquire(context.Background(), info.Token); !errors.Is(err, ErrPortsExhausted) {
		t.Fatalf("unexpanded pool admitted a fourth viewer: %v", err)
	}
	if status, _ := reloaded.Status(info.Token); status.Viewers != 3 || status.RunningInstances != 3 {
		t.Fatalf("failed admission changed viewer accounting: %+v", status)
	}
	if err := settings.SetMaxChannels(10); err != nil {
		t.Fatal(err)
	}
	if reloaded.ChannelCapacity() != 10 {
		t.Fatal("management capacity source did not reflect resize")
	}
	for range 5 {
		acquire()
	}
	if _, _, err := reloaded.Acquire(context.Background(), info.Token); !errors.Is(err, ErrViewerLimit) {
		t.Fatalf("configured project quota was not enforced: %v", err)
	}
	if got := settings.Settings(); got.ActiveChannels != 8 {
		t.Fatalf("active worker count = %+v", got)
	}
	for _, release := range releases {
		release()
	}
	if got := settings.Settings(); got.ActiveChannels != 1 {
		t.Fatalf("secondary channels did not release their pool leases: %+v", got)
	}
}

func TestConfiguredPoolIsSharedAcrossProjectsAndReleasedChannelIsReusable(t *testing.T) {
	ports := channelTestPorts(t)
	workers := &leasedTestWorkers{ports: ports, active: make(map[string]PortLease)}
	m, err := NewSessionManager(workers, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	m.SetChannelCapacitySource(ports.Capacity)
	t.Cleanup(func() {
		if err := m.StopAll(context.Background()); err != nil {
			t.Error(err)
		}
	})
	first, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/a", ConcurrencyMode: "multi", MaxViewers: 10})
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/b", ConcurrencyMode: "multi", MaxViewers: 10})
	if err != nil {
		t.Fatal(err)
	}
	firstPrimary, releaseFirst, err := m.Acquire(context.Background(), first.Token)
	if err != nil {
		t.Fatal(err)
	}
	firstExtra, releaseExtra, err := m.Acquire(context.Background(), first.Token)
	if err != nil {
		t.Fatal(err)
	}
	secondPrimary, releaseSecond, err := m.Acquire(context.Background(), second.Token)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Acquire(context.Background(), second.Token); !errors.Is(err, ErrPortsExhausted) {
		t.Fatalf("separate projects bypassed global pool admission: %v", err)
	}
	releaseExtra()
	replacement, releaseReplacement, err := m.Acquire(context.Background(), second.Token)
	if err != nil {
		t.Fatalf("released channel was not available to another project: %v", err)
	}
	if replacement.Token == firstPrimary.Token || replacement.Token == firstExtra.Token || replacement.Token == secondPrimary.Token || replacement.CDPPort != firstExtra.CDPPort {
		t.Fatalf("replacement must reuse ports with its own isolated browser identity: %+v", replacement)
	}
	releaseReplacement()
	releaseSecond()
	releaseFirst()
}

func TestCreatePrewarmedStartsDistinctWorkers(t *testing.T) {
	workers := &fakeWorkers{}
	m, err := NewSessionManager(workers, 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.CreatePrewarmed(context.Background(), SessionOptions{TargetURL: "https://example.com/a", RenderMode: "cpu"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.CreatePrewarmed(context.Background(), SessionOptions{TargetURL: "https://example.com/b", RenderMode: "nvidia", ReadySelector: "#scene-ready"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Token == second.Token {
		t.Fatal("distinct pages share a token")
	}
	if workers.starts != 2 {
		t.Fatalf("expected two preloaded workers, got %d", workers.starts)
	}
	if second.RenderMode != "nvidia" || second.ReadySelector != "#scene-ready" {
		t.Fatalf("options lost: %+v", second)
	}
	_, release, err := m.Acquire(context.Background(), first.Token)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if workers.starts != 3 {
		t.Fatalf("expected controller health check on acquire, got %d", workers.starts)
	}
}

func TestHighFPSLinkSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	m, err := NewSessionManager(&fakeWorkers{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	info, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/scene", Resolution: "1920x1080", FPS: 120})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := NewSessionManager(&fakeWorkers{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	loaded, ok := restored.Lookup(info.Token)
	if !ok || loaded.FPS != 120 {
		t.Fatalf("high FPS link was not restored: %+v", loaded)
	}
	if _, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/scene", Resolution: "2560x1440", FPS: 120}); err == nil {
		t.Fatal("unsupported 1440p/120 FPS link was accepted")
	}
}

func TestPrewarmedWorkerExpiresWithoutViewer(t *testing.T) {
	workers := &fakeWorkers{}
	m, err := NewSessionManager(workers, 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m.now = func() time.Time { return now }
	info, err := m.CreatePrewarmed(context.Background(), SessionOptions{TargetURL: "https://example.com/scene"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(180 * time.Second)
	if err := m.ReapOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if workers.stops != 1 {
		t.Fatalf("expected idle prewarm cleanup, got %d", workers.stops)
	}
	if _, ok := m.Lookup(info.Token); !ok {
		t.Fatal("short link should survive idle cleanup")
	}
}

func TestSessionStoreAndSingleViewerLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	workers := &fakeWorkers{}
	m, err := NewSessionManager(workers, 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	info, err := m.CreatePrewarmed(context.Background(), SessionOptions{TargetURL: "https://example.com/twin", Name: "Twin A", ConcurrencyMode: "single"})
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := m.Acquire(context.Background(), info.Token)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Acquire(context.Background(), info.Token); !errors.Is(err, ErrViewerLimit) {
		t.Fatalf("want viewer limit, got %v", err)
	}
	release()
	restored, err := NewSessionManager(&fakeWorkers{}, 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	got, ok := restored.Lookup(info.Token)
	if !ok || got.Name != "Twin A" || got.MaxViewers != 1 {
		t.Fatalf("restored link incorrect: %+v", got)
	}
	if err := m.Delete(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	again, err := NewSessionManager(&fakeWorkers{}, 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Lookup(info.Token); ok {
		t.Fatal("deleted link restored")
	}
}

func TestPauseRenderingPersistsAndPreventsAutomaticRestart(t *testing.T) {
	store := filepath.Join(t.TempDir(), "sessions.json")
	workers := &fakeWorkers{}
	m, err := NewSessionManager(workers, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	m.SetKeepPreloaded(true)
	if err := m.EnableStore(store); err != nil {
		t.Fatal(err)
	}
	info, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/scene"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Warm(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	if err := m.PauseRendering(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	status := m.List()[0]
	if !status.RenderPaused || status.Running || workers.stops != 1 {
		t.Fatalf("render worker was not stopped: %+v, stops=%d", status, workers.stops)
	}
	if _, _, err := m.Acquire(context.Background(), info.Token); !errors.Is(err, ErrRenderPaused) {
		t.Fatalf("stopped link accepted a viewer: %v", err)
	}
	if err := m.MaintainPreloads(context.Background()); err != nil {
		t.Fatal(err)
	}
	if workers.starts != 1 {
		t.Fatalf("maintenance restarted a stopped renderer: %d", workers.starts)
	}
	restoredWorkers := &fakeWorkers{}
	restored, err := NewSessionManager(restoredWorkers, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	restored.SetKeepPreloaded(true)
	if err := restored.EnableStore(store); err != nil {
		t.Fatal(err)
	}
	if err := restored.PreloadSaved(context.Background()); err != nil {
		t.Fatal(err)
	}
	if restoredWorkers.starts != 0 || !restored.List()[0].RenderPaused {
		t.Fatal("stopped render state was not preserved after restart")
	}
	if _, err := restored.ResumeRendering(context.Background(), info.Token); err != nil {
		t.Fatal(err)
	}
	if restoredWorkers.starts != 1 || restored.List()[0].RenderPaused || !restored.List()[0].Running {
		t.Fatal("renderer did not resume")
	}
}

func TestPauseRenderingCancelsInFlightPreload(t *testing.T) {
	workers := &blockingWorkers{entered: make(chan struct{}), release: make(chan struct{})}
	m, err := NewSessionManager(workers, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	info, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/scene"})
	if err != nil {
		t.Fatal(err)
	}
	warmDone := make(chan error, 1)
	go func() { _, err := m.Warm(context.Background(), info.Token); warmDone <- err }()
	<-workers.entered
	stopDone := make(chan error, 1)
	go func() { stopDone <- m.PauseRendering(context.Background(), info.Token) }()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop waited for an in-flight preload")
	}
	if err := <-warmDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("preload was not canceled: %v", err)
	}
	if status := m.List()[0]; !status.RenderPaused || status.Running {
		t.Fatalf("unexpected stopped status: %+v", status)
	}
}

func TestSavedProjectsPreloadOnRestartAndStayReady(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	first, err := NewSessionManager(&fakeWorkers{}, 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	info, err := first.CreatePrewarmed(context.Background(), SessionOptions{TargetURL: "https://example.com/twin"})
	if err != nil {
		t.Fatal(err)
	}
	restoredWorkers := &fakeWorkers{}
	restored, err := NewSessionManager(restoredWorkers, 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	restored.SetKeepPreloaded(true)
	now := time.Now()
	restored.now = func() time.Time { return now }
	if err := restored.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	if err := restored.PreloadSaved(context.Background()); err != nil {
		t.Fatal(err)
	}
	statuses := restored.List()
	if len(statuses) != 1 || statuses[0].Token != info.Token || !statuses[0].Running || restoredWorkers.starts != 1 {
		t.Fatalf("saved project was not preloaded: %+v, starts=%d", statuses, restoredWorkers.starts)
	}
	now = now.Add(181 * time.Second)
	if err := restored.ReapOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if restoredWorkers.stops != 0 || !restored.List()[0].Running {
		t.Fatal("published preload was reaped")
	}
}

func TestFailedPreloadKeepsLinkAndRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	first, _ := NewSessionManager(&fakeWorkers{}, 180*time.Second)
	if err := first.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	info, err := first.CreatePrewarmed(context.Background(), SessionOptions{TargetURL: "https://example.com/twin"})
	if err != nil {
		t.Fatal(err)
	}
	workers := &fakeWorkers{startErr: errors.New("browser failed")}
	restored, _ := NewSessionManager(workers, 180*time.Second)
	restored.SetKeepPreloaded(true)
	now := time.Now()
	restored.now = func() time.Time { return now }
	if err := restored.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	if err := restored.PreloadSaved(context.Background()); err == nil {
		t.Fatal("expected preload failure")
	}
	status := restored.List()[0]
	if status.Token != info.Token || status.Running || status.PreloadError == "" {
		t.Fatalf("unexpected failed preload status: %+v", status)
	}
	workers.startErr = nil
	now = now.Add(31 * time.Second)
	if err := restored.MaintainPreloads(context.Background()); err != nil {
		t.Fatal(err)
	}
	status = restored.List()[0]
	if !status.Running || status.PreloadError != "" {
		t.Fatalf("preload did not recover: %+v", status)
	}
}
