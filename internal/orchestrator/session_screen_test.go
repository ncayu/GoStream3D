package orchestrator

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAdaptiveSessionSurvivesStoreAndWarmsIndependentWorkers(t *testing.T) {
	m, _ := NewSessionManager(&fakeWorkers{}, time.Minute)
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := m.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	info, err := m.CreatePublished(SessionOptions{TargetURL: "https://example.com/scene", Resolution: "adaptive", FPS: 60, ConcurrencyMode: "multi", MaxViewers: 6, PreloadCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	next, _ := NewSessionManager(&fakeWorkers{}, time.Minute)
	if err := next.EnableStore(path); err != nil {
		t.Fatal(err)
	}
	saved, ok := next.Lookup(info.Token)
	if !ok || saved.Resolution != "adaptive" || saved.FPS != 60 {
		t.Fatalf("adaptive setting lost: %+v", saved)
	}
	for _, token := range []string{info.Token, "independent-extra"} {
		spec := channelSpec(saved, token)
		if !spec.Adaptive || spec.Screen != "1920x1080@60" || spec.Token != token {
			t.Fatalf("wrong prewarm spec: %+v", spec)
		}
	}
	for _, test := range []struct {
		resolution string
		fps        int
		valid      bool
	}{
		{"adaptive", 30, true}, {"adaptive", 120, true}, {"adaptive", 0, false}, {"adaptive", 61, false},
		{"1920x1080", 60, true}, {"2560x1440", 60, false}, {"9999x9999", 30, false},
	} {
		if validSessionScreen(test.resolution, test.fps) != test.valid {
			t.Errorf("screen validation: %+v", test)
		}
	}
}
