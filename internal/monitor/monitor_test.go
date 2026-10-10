package monitor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestNetworkRateHandlesCounterReset(t *testing.T) {
	if got := megabitsPerSecond(1_000_000, 2_000_000, 2); got != 4 {
		t.Fatalf("expected 4 Mbps, got %f", got)
	}
	if got := megabitsPerSecond(2_000_000, 1_000_000, 2); got != 0 {
		t.Fatalf("counter reset should yield zero, got %f", got)
	}
}

func TestSampleReportsHostAndKeepsHistory(t *testing.T) {
	svc := New()
	svc.snapshot.History = []NetworkPoint{{Time: time.Now().Add(-2 * time.Hour)}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	svc.Sample(ctx)
	first := svc.Snapshot()
	if first.Errors == nil || first.History == nil || first.Partitions == nil || first.Interfaces == nil {
		t.Fatal("monitor API slices must encode as empty arrays, not null")
	}
	if first.SampledAt.IsZero() || first.CPUCores < 1 || first.Memory.TotalBytes == 0 {
		t.Fatalf("missing host metrics: %+v", first)
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(first.History) != 1 {
		t.Fatalf("history must discard samples older than one hour: %d", len(first.History))
	}
	point := first.History[0]
	if point.CPUPercent == nil || *point.CPUPercent != first.CPUPercent || point.MemoryPercent == nil || *point.MemoryPercent != first.Memory.UsedPercent || !point.Time.Equal(first.SampledAt) {
		t.Fatalf("resource trends must match the same host sample: %+v", point)
	}
	if point.GPUs == nil || len(point.GPUs) != len(first.GPUs) {
		t.Fatal("GPU trend must share the same sample and encode an empty array when unavailable")
	}
	for i, card := range first.GPUs {
		saved := point.GPUs[i]
		if saved.ID != card.ID || saved.Name != card.Name || (saved.Utilization == nil) != (card.Utilization == nil) || (saved.Utilization != nil && *saved.Utilization != *card.Utilization) {
			t.Fatal("GPU trend did not preserve the sampled device and utilization")
		}
	}
	svc.Sample(ctx)
	second := svc.Snapshot()
	if len(second.History) != 2 {
		t.Fatalf("expected two resource/network samples, got %d", len(second.History))
	}
	second.History[0].RxMbps = -1
	*second.History[0].CPUPercent = -1
	*second.History[0].MemoryPercent = -1
	if svc.Snapshot().History[0].RxMbps < 0 {
		t.Fatal("snapshot history aliases internal monitor state")
	}
	if saved := svc.Snapshot().History[0]; *saved.CPUPercent < 0 || *saved.MemoryPercent < 0 {
		t.Fatal("resource history aliases internal monitor state")
	}
}

func TestGPUHistoryKeepsDeviceIdentityMissingSamplesAndSnapshotIsolation(t *testing.T) {
	value := 0.0
	cards := []GPU{{ID: "GPU-a", Name: "Same model", Utilization: &value}, {ID: "GPU-b", Name: "Same model"}}
	points := gpuHistory(cards)
	value = 90
	if *points[0].Utilization != 0 || points[1].Utilization != nil || points[0].ID == points[1].ID {
		t.Fatal("history must retain real zero, missing data and independent GPU IDs")
	}
	svc := New()
	svc.snapshot.History = []NetworkPoint{{GPUs: points}, {GPUs: gpuHistory(nil)}}
	copy := svc.Snapshot()
	copy.History[0].GPUs[0].ID = "changed"
	*copy.History[0].GPUs[0].Utilization = 100
	saved := svc.Snapshot()
	if saved.History[0].GPUs[0].ID != "GPU-a" || *saved.History[0].GPUs[0].Utilization != 0 || saved.History[1].GPUs == nil || len(saved.History[1].GPUs) != 0 {
		t.Fatal("snapshot aliases GPU history or fabricates unavailable samples")
	}
}

func TestMonitorIntervalPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor-settings.json")
	svc, err := NewWithSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.Settings().IntervalSeconds; got != 30 {
		t.Fatalf("default interval = %d, want 30", got)
	}
	if err := svc.SetInterval(60 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetInterval(120 * time.Second); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewWithSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := restarted.Settings().IntervalSeconds; got != 120 {
		t.Fatalf("persisted interval = %d, want 120", got)
	}
	if err := restarted.SetInterval(5 * time.Second); !errors.Is(err, ErrInvalidInterval) {
		t.Fatalf("expected invalid interval, got %v", err)
	}
	if got := restarted.Settings().IntervalSeconds; got != 120 {
		t.Fatalf("invalid update changed interval to %d", got)
	}
}
