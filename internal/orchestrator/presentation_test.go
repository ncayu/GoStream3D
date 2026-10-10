package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPresentationPersistsWithoutInterruptingViewer(t *testing.T) {
	workers := &fakeWorkers{}
	m, _ := NewSessionManager(workers, time.Minute)
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := m.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	info, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com"})
	if err != nil || !info.ShowFPS {
		t.Fatalf("default FPS setting: %+v %v", info, err)
	}
	_, release, err := m.Acquire(context.Background(), info.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	starts, stops := workers.starts, workers.stops
	changed, err := m.SetShowFPS(info.Token, false)
	if err != nil || changed.ShowFPS {
		t.Fatalf("toggle with active viewer: %+v %v", changed, err)
	}
	status, _ := m.Status(info.Token)
	if status.Viewers != 1 || !status.Running || workers.starts != starts || workers.stops != stops || changed.FPS != info.FPS {
		t.Fatalf("presentation interrupted rendering: %+v, starts=%d stops=%d", status, workers.starts, workers.stops)
	}
	reloaded, _ := NewSessionManager(&fakeWorkers{}, time.Minute)
	if err := reloaded.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	if saved, ok := reloaded.Lookup(info.Token); !ok || saved.ShowFPS {
		t.Fatalf("hidden FPS not restored: %+v", saved)
	}
	// A legacy API client editing other fields must keep the saved display choice.
	updated, warm, err := reloaded.Update(context.Background(), info.Token, SessionOptions{TargetURL: info.TargetURL})
	if err != nil || updated.ShowFPS || warm {
		t.Fatalf("omitted show_fps reset presentation: %+v %v", updated, err)
	}
	m.persistMu.Lock()
	m.storeReport.WriteBlocked = true
	m.persistMu.Unlock()
	if _, err := m.SetShowFPS(info.Token, true); err == nil {
		t.Fatal("blocked persistence reported success")
	}
	if status, _ := m.Status(info.Token); status.ShowFPS || status.Viewers != 1 {
		t.Fatalf("failed save did not roll back: %+v", status)
	}
}

func TestPresentationLegacyStoreDefaultsToVisible(t *testing.T) {
	m, _ := NewSessionManager(&fakeWorkers{}, time.Minute)
	info, _ := m.CreatePublished(SessionOptions{TargetURL: "https://example.com"})
	data, _ := json.Marshal(info)
	var legacy map[string]json.RawMessage
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy, "show_fps")
	data, _ = json.Marshal([]map[string]json.RawMessage{legacy})
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := NewSessionManager(&fakeWorkers{}, time.Minute)
	if err := reloaded.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	if saved, ok := reloaded.Lookup(info.Token); !ok || !saved.ShowFPS {
		t.Fatalf("legacy FPS changed: %+v", saved)
	}
	falseValue := false
	updated, warm, err := reloaded.Update(context.Background(), info.Token, SessionOptions{TargetURL: info.TargetURL, ShowFPS: &falseValue})
	if err != nil || updated.ShowFPS || warm {
		t.Fatalf("full edit of display option: %+v %v", updated, err)
	}
}
