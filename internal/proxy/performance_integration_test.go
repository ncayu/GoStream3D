package proxy

import (
	"github.com/gorilla/websocket"
	"gostream3d/internal/orchestrator"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebRTCTelemetryConsumedAndBoundToLeasedWorker(t *testing.T) {
	u := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := u.Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		for {
			kind, b, e := c.ReadMessage()
			if e != nil {
				return
			}
			if c.WriteMessage(kind, b) != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	p, _ := NewWebSocketProxy("secret")
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = p.ServeWorker(w, r, orchestrator.Worker{Token: "leased-worker", SignalURL: upstream.URL}, orchestrator.SessionInfo{Token: "leased-project", Name: "GPU project", RenderMode: "nvidia", FPS: 30})
	}))
	defer gateway.Close()
	c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if e = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"webrtc-performance","token":"forged-worker","metrics":{"width":1920,"height":1080,"codec":"video/H264","sample_seconds":3,"display_fps":29,"bitrate_mbps":6,"packets_lost":0}}`)); e != nil {
		t.Fatal(e)
	}
	ordinary := `{"event":"client/heartbeat"}`
	if e = c.WriteMessage(websocket.TextMessage, []byte(ordinary)); e != nil {
		t.Fatal(e)
	}
	_, b, e := c.ReadMessage()
	if e != nil || string(b) != ordinary {
		t.Fatalf("private report reached Neko or signaling broken: %s %v", b, e)
	}
	all := p.PerformanceSnapshot()
	if len(all) != 1 || all[0].Token != "leased-worker" || all[0].ProjectToken != "leased-project" || all[0].WebRTC == nil || *all[0].WebRTC.DisplayFPS != 29 || all[0].TargetFPS != 30 {
		t.Fatal(all)
	}
	c.Close()
	deadline := time.Now().Add(3 * time.Second)
	for len(p.PerformanceSnapshot()) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(p.PerformanceSnapshot()) != 0 {
		t.Fatal("closed viewer still reported")
	}
}
