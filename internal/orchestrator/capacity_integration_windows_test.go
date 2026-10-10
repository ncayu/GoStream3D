//go:build windows

package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestWindowsCapacityRealSceneAndCancellationCleanup(t *testing.T) {
	if _, err := findWindowsBrowser("chromium"); err != nil {
		t.Skip(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<canvas width="640" height="360"></canvas><script>const c=document.querySelector('canvas').getContext('2d');function draw(t){c.fillStyle='hsl('+(t/10%360)+',80%,50%)';c.fillRect(0,0,640,360);requestAnimationFrame(draw)}requestAnimationFrame(draw)</script>`)
	}))
	defer target.Close()
	hp, cp := freeTestPort(t, 36400), freeTestPort(t, 36500)
	ports, _ := NewPortAllocator(PortRange{hp, hp + 1}, PortRange{cp, cp + 1}, PortRange{56400, 56415}, 8)
	root := t.TempDir()
	controller, err := NewNativeManager(ports, NativeConfig{WorkRoot: root, ReadyTimeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	m := controller.(*WindowsProcessManager)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	info := SessionInfo{Token: "fixture", TargetURL: target.URL, Resolution: "640x360", FPS: 30, RenderMode: "nvidia"}
	stage, err := m.RunCapacityStage(ctx, info, 2, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(stage.Measurements) != 2 {
		t.Fatal(stage)
	}
	for _, p := range stage.Measurements {
		if p.LowFPS < 10 || p.RenderFPS <= 0 || p.Samples != 3 {
			t.Fatal(stage)
		}
	}
	if ports.Available() != 2 || len(m.PerformanceSnapshot()) != 0 {
		t.Fatal("test workers or leases leaked")
	}
	files, _ := os.ReadDir(root)
	if len(files) != 0 {
		t.Fatal("test profiles not removed")
	}
	stopCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	if _, err = m.RunCapacityStage(stopCtx, info, 1, 30*time.Second); err == nil {
		t.Fatal("cancelled stage reported success")
	}
	if ports.Available() != 2 || len(m.PerformanceSnapshot()) != 0 {
		t.Fatal("cancel leaked workers or ports")
	}
	t.Logf("real animated fixture: %+v", stage)
}
