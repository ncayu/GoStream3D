package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreLoadsHealthyRecordsBesideInvalidRecord(t *testing.T) {
	m, _ := NewSessionManager(&fakeWorkers{}, time.Minute)
	info, _ := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/scene"})
	good, _ := json.Marshal(info)
	path := filepath.Join(t.TempDir(), "sessions.json")
	raw := append(append([]byte("["), good...), []byte(`,{"token":"bad","target_url":"javascript:bad"}]`)...)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	restored, _ := NewSessionManager(&fakeWorkers{}, time.Minute)
	if err := restored.EnableStore(path); err != nil {
		t.Fatalf("one damaged record blocked healthy links: %v", err)
	}
	if _, ok := restored.Lookup(info.Token); !ok {
		t.Fatal("healthy link missing")
	}
	state := restored.StoreStatus()
	if len(state.Issues) != 1 || state.Archive == "" {
		t.Fatal(state)
	}
	preserved, err := os.ReadFile(filepath.Join(path+".backups", state.Archive))
	if err != nil || string(preserved) != string(raw) {
		t.Fatal("original damaged bytes not preserved", err)
	}
}

func TestStoreBackupsRecoverMissingWithoutReplacingCurrent(t *testing.T) {
	m, _ := NewSessionManager(&fakeWorkers{}, time.Minute)
	path := filepath.Join(t.TempDir(), "sessions.json")
	_ = m.EnableStore(path)
	a, _ := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/a", Name: "Original A"})
	b, _ := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/b", Name: "B"})
	id, err := m.BackupStore()
	if err != nil {
		t.Fatal(err)
	}
	// Model one missing definition with a changed surviving definition.
	m.mu.Lock()
	delete(m.sessions, b.Token)
	m.sessions[a.Token].info.Name = "Current A"
	m.mu.Unlock()
	if err = m.save(); err != nil {
		t.Fatal(err)
	}
	p, err := m.PreviewRestore(id)
	if err != nil || len(p.Add) != 1 || p.Existing != 1 {
		t.Fatal(p, err)
	}
	if _, err = m.RestoreMissing(id, "wrong digest"); err == nil {
		t.Fatal("unreviewed contents accepted")
	}
	if _, err = m.PreviewRestore("../sessions.json"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if _, err = m.RestoreMissing(id, p.Digest); err != nil {
		t.Fatal(err)
	}
	current, _ := m.Lookup(a.Token)
	recovered, _ := m.Lookup(b.Token)
	if current.Name != "Current A" || !recovered.RenderPaused || recovered.TargetURL != b.TargetURL {
		t.Fatal(current, recovered)
	}
}

func TestUnparseableStoreIsProtectedAndLatestBackupRestores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	m, _ := NewSessionManager(&fakeWorkers{}, time.Minute)
	_ = m.EnableStore(path)
	info, _ := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/scene"})
	raw := []byte(`[{broken`)
	_ = os.WriteFile(path, raw, 0600)
	restored, _ := NewSessionManager(&fakeWorkers{}, time.Minute)
	if err := restored.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	if !restored.StoreStatus().WriteBlocked || restored.save() == nil {
		t.Fatal("corrupt source could be overwritten")
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(raw) {
		t.Fatal("source changed")
	}
	for _, b := range restored.StoreStatus().Backups {
		if b.ValidRecords == 1 && b.Readable {
			p, e := restored.PreviewRestore(b.ID)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = restored.RestoreMissing(b.ID, p.Digest); e != nil {
				t.Fatal(e)
			}
			if _, ok := restored.Lookup(info.Token); !ok || restored.StoreStatus().WriteBlocked {
				t.Fatal("not restored")
			}
			return
		}
	}
	t.Fatal("latest project had no automatic healthy backup")
}
