package orchestrator

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

type fakeCapacityRunner struct {
	calls  []int
	block  bool
	during func()
}

func (f *fakeCapacityRunner) RunCapacityStage(ctx context.Context, _ SessionInfo, n int, d time.Duration) (CapacityStage, error) {
	if f.block {
		<-ctx.Done()
		return CapacityStage{Streams: n}, ctx.Err()
	}
	if f.during != nil {
		f.during()
	}
	f.calls = append(f.calls, n)
	s := CapacityStage{Streams: n}
	for i := 0; i < n; i++ {
		fps := 30.
		if n == 3 && i == 1 {
			fps = 18
		}
		s.Measurements = append(s.Measurements, CapacityStream{LowFPS: fps, AverageFPS: fps, Samples: int(d / time.Second)})
	}
	return s, nil
}

func TestCapacityRejectsChangedCPUConditions(t *testing.T) {
	limit := 50
	r := &fakeCapacityRunner{during: func() { limit = 30 }}
	s := NewCapacityService(r, func() int { return 1 }, DefaultPerformanceSettings, "")
	s.SetCPUPercentSource(func() int { return limit })
	if err := s.Start(context.Background(), SessionInfo{Token: "test", FPS: 30}, CapacityRequest{"test", 1, 30, 24}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Fatal("test did not complete")
	}
	p := s.Snapshot()
	if p.Status != "failed" || p.Recommended != 0 || p.CPUCap != 50 {
		t.Fatal(p)
	}
}
func TestCapacityUsesWorstStreamStopsAndPersists(t *testing.T) {
	r := &fakeCapacityRunner{}
	path := filepath.Join(t.TempDir(), "capacity.json")
	s := NewCapacityService(r, func() int { return 5 }, DefaultPerformanceSettings, path)
	info := SessionInfo{Token: "test", FPS: 30}
	req := CapacityRequest{ProjectToken: "test", MaxStreams: 5, Seconds: 30, MinFPS: 24}
	if err := s.Start(context.Background(), info, req); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Fatal("test did not finish")
	}
	got := s.Snapshot()
	if got.Recommended != 2 || len(got.Stages) != 3 || got.Stages[2].Passed || got.Status != "complete" {
		t.Fatal(got)
	}
	restored := NewCapacityService(r, func() int { return 5 }, DefaultPerformanceSettings, path)
	if restored.Snapshot().Recommended != 2 {
		t.Fatal("report not persisted")
	}
	values := make([]float64, 30)
	for i := range values {
		values[i] = 30
	}
	values[0] = 1
	values[1] = 1
	values[2] = 1
	if lowFPS(values) < 24 {
		t.Fatal("90% of windows at threshold should pass")
	}
	values[3] = 1
	if lowFPS(values) >= 24 {
		t.Fatal("more than 10% failing windows must fail")
	}
}
func TestCapacityCancellationAdmissionAndValidation(t *testing.T) {
	r := &fakeCapacityRunner{block: true}
	s := NewCapacityService(r, func() int { return 1 }, DefaultPerformanceSettings, "")
	info := SessionInfo{Token: "test", FPS: 30}
	req := CapacityRequest{"test", 2, 30, 24}
	if err := s.Start(context.Background(), info, req); err == nil {
		t.Fatal("overbooked available slots")
	}
	req.MaxStreams = 1
	req.Seconds = 1
	if err := s.Start(context.Background(), info, req); err == nil {
		t.Fatal("too short test accepted")
	}
	req.Seconds = 30
	if err := s.Start(context.Background(), info, req); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background(), info, req); err == nil {
		t.Fatal("concurrent test allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Status != "cancelled" {
		t.Fatal(s.Snapshot())
	}
}
