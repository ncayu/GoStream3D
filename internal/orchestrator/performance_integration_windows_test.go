//go:build windows

package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A small animated fixture measures the live JPEG path without touching saved
// projects. It verifies policy application; it is not a 3D capacity benchmark.
func TestWindowsAnimationPolicyAndTelemetryIntegration(t *testing.T) {
	if _, err := findWindowsBrowser("chromium"); err != nil {
		t.Skip(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, `<body style="margin:0"><canvas width="640" height="360"></canvas><script>const c=document.querySelector('canvas').getContext('2d');function draw(t){c.fillStyle='hsl('+(t/10%360)+',80%,50%)';c.fillRect(0,0,640,360);requestAnimationFrame(draw)}requestAnimationFrame(draw);document.body.dataset.ready='yes';</script></body>`)
	}))
	defer target.Close()
	hp, cp := freeTestPort(t, 35400), freeTestPort(t, 35500)
	ports, err := NewPortAllocator(PortRange{hp, hp + 1}, PortRange{cp, cp + 1}, PortRange{55400, 55415}, 8)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := NewNativeManager(ports, NativeConfig{WorkRoot: t.TempDir(), ReadyTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	m := controller.(*WindowsProcessManager)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, policy := range []string{"interactive", "animation"} {
		worker, err := m.StartWorker(ctx, WorkerSpec{Token: "metrics-" + policy, ProjectToken: "fixture", TargetURL: target.URL, Screen: "640x360@30", RenderMode: "nvidia", FramePolicy: policy, ReadySelector: "body[data-ready='yes']"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			_ = m.Stop(stopCtx, worker.Token)
		})
		gateway := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { _ = m.Serve(rw, r, worker.SignalURL) }))
		t.Cleanup(gateway.Close)
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		if err := conn.WriteJSON(map[string]any{"type": "telemetry"}); err != nil {
			t.Fatal(err)
		}
		// Drain frames while supplying measured decode feedback from this Go
		// client. Browser createImageBitmap/drawImage timing is tested separately.
		go func() {
			var id uint64
			for {
				kind, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if kind == websocket.TextMessage {
					var meta struct {
						ID uint64 `json:"id"`
					}
					_ = json.Unmarshal(data, &meta)
					id = meta.ID
					continue
				}
				at := time.Now()
				_, err = jpeg.Decode(bytes.NewReader(data))
				elapsed := float64(time.Since(at)) / float64(time.Millisecond)
				if err == nil && id > 0 {
					_ = conn.WriteJSON(map[string]any{"type": "frameAck", "frameID": id, "decodeMS": elapsed, "displayFPS": 0})
				}
			}
		}()
	}
	measureAt := time.Now()
	deadline := measureAt.Add(12 * time.Second)
	for {
		stats := m.PerformanceSnapshot()
		ready := len(stats) == 2 && time.Since(measureAt) >= 6*time.Second
		for _, p := range stats {
			want := 6.0
			if p.Policy == "animation" {
				want = 30
			}
			if p.BudgetFPS != want || p.FeedbackAt.IsZero() || p.CaptureFPS <= 0 || p.WriteMS <= 0 || p.CaptureDeliveryMS <= 0 || p.RenderFPS <= 0 || p.SendFPS <= 0 {
				ready = false
			}
		}
		if ready {
			data, _ := json.Marshal(stats)
			t.Log("fixture_metrics=" + string(data))
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live policies or telemetry did not settle: %+v", stats)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
