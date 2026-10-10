package orchestrator

import (
	"math"
	"path/filepath"
	"testing"
)

func TestGPUFrameBudgetsShareLimitAndKeepAnimationRunning(t *testing.T) {
	workers := []GPUFrameDemand{
		{Token: "interactive", TargetFPS: 60, Watching: true, Interactive: true},
		{Token: "animation", TargetFPS: 60, Watching: true, Policy: "animation"},
		{Token: "passive", TargetFPS: 60, Watching: true},
		{Token: "idle", TargetFPS: 60},
	}
	budgets := AllocateGPUFrameBudgets(workers, 60)
	total := 0.0
	for _, fps := range budgets {
		total += fps
		if fps <= 0 {
			t.Fatal("zero means unlimited to the page scheduler")
		}
	}
	if total > 60.001 {
		t.Fatalf("shared budget exceeded: %v", budgets)
	}
	if budgets["animation"] <= 6 || budgets["interactive"] < budgets["animation"] || budgets["idle"] > 2 {
		t.Fatalf("policy/priority ignored: %v", budgets)
	}
	full := AllocateGPUFrameBudgets(workers, 200)
	if full["animation"] != 60 || full["passive"] != 6 {
		t.Fatal(full)
	}
	workers[0].CaptureFPS, workers[0].FrameAckMS = 12, 400
	pressured := AllocateGPUFrameBudgets(workers, 200)
	if pressured["interactive"] >= full["interactive"] {
		t.Fatal("measured latency did not reduce pressure")
	}
	workers[0].FrameAckMS = 0
	if got := AllocateGPUFrameBudgets(workers, 200)["interactive"]; got != 60 {
		t.Fatal("low capture FPS on its own was mistaken for overload", got)
	}
	for i := range workers {
		workers[i].TargetFPS = 120
	}
	for _, fps := range AllocateGPUFrameBudgets(workers, 1) {
		if math.IsNaN(fps) || fps <= 0 {
			t.Fatal(fps)
		}
	}
}

func TestPerformanceSettingsPersistAndRejectInvalidBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "performance.json")
	s, err := NewPerformanceSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Settings() != DefaultPerformanceSettings() {
		t.Fatal(s.Settings())
	}
	next := PerformanceSettings{GPUFPSBudget: 90, ReclaimIdlePreloads: true}
	if err := s.Update(next); err != nil {
		t.Fatal(err)
	}
	restored, err := NewPerformanceSettings(path)
	if err != nil || restored.Settings() != next {
		t.Fatal(restored, err)
	}
	if err := s.Update(PerformanceSettings{GPUFPSBudget: -1}); err == nil || s.Settings() != next {
		t.Fatal("invalid update changed settings")
	}
}
