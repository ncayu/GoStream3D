//go:build windows

package orchestrator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"gostream3d/internal/viewport"
)

// Hold a real CDP reply to reproduce a stalled resize without launching Chrome.
func TestWindowsViewportResizeDoesNotBlockReleases(t *testing.T) {
	for _, mode := range []string{"cpu", "nvidia"} {
		t.Run(mode, func(t *testing.T) {
			peer := make(chan *websocket.Conn, 1)
			stop := make(chan struct{})
			browser := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(rw, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				peer <- conn
				<-stop
			}))
			defer browser.Close()
			defer close(stop)
			cdp, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(browser.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cdp.Close()
			remote := <-peer
			worker := &windowsWorker{Worker: Worker{Token: "resize-test", RenderMode: mode, Adaptive: true},
				cdp: cdp, closed: make(chan struct{}), pending: make(map[int]chan cdpMessage),
				viewers: make(map[*windowsViewer]struct{}), captureWake: make(chan struct{}, 1), width: 1280, height: 720}
			go worker.readCDP()
			manager := &WindowsProcessManager{active: map[string]*windowsWorker{worker.Token: worker}}
			finished := make(chan struct{})
			gateway := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				defer close(finished)
				_ = manager.Serve(rw, r, "local-cdp://"+worker.Token)
			}))
			defer gateway.Close()
			viewer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer viewer.Close()
			send := func(value any) {
				t.Helper()
				if err := viewer.WriteJSON(value); err != nil {
					t.Fatal(err)
				}
			}
			read := func() cdpMessage {
				t.Helper()
				_ = remote.SetReadDeadline(time.Now().Add(time.Second))
				var msg cdpMessage
				if err := remote.ReadJSON(&msg); err != nil {
					t.Fatalf("input waited behind resize: %v", err)
				}
				return msg
			}
			ack := func(msg cdpMessage) {
				t.Helper()
				if err := remote.WriteJSON(map[string]any{"id": msg.ID, "result": map[string]any{}}); err != nil {
					t.Fatal(err)
				}
			}
			send(viewport.Size{Type: "resize", Width: 1600, Height: 800})
			first := read()
			if first.Method != "Emulation.setDeviceMetricsOverride" {
				t.Fatal(first.Method)
			}
			for _, width := range []int{1704, 1800, 1920} {
				send(viewport.Size{Type: "resize", Width: width, Height: 800})
			}
			send(map[string]any{"type": "up", "x": 10, "y": 10, "button": "left", "buttons": 0})
			send(map[string]any{"type": "keyUp", "key": "w", "code": "KeyW", "keyCode": 87})
			for _, method := range []string{"Input.dispatchMouseEvent", "Input.dispatchKeyEvent"} {
				msg := read()
				if msg.Method != method {
					t.Fatalf("lost release: %s, want %s", msg.Method, method)
				}
				ack(msg)
			}
			ack(first)
			latest := read()
			if latest.Method == "Runtime.evaluate" {
				if err := remote.WriteJSON(map[string]any{"id": latest.ID, "result": map[string]any{"result": map[string]any{"type": "boolean", "value": true}}}); err != nil {
					t.Fatal(err)
				}
				latest = read()
			}
			var size viewport.Size
			if err := json.Unmarshal(latest.Params, &size); err != nil {
				t.Fatal(err)
			}
			if latest.Method != "Emulation.setDeviceMetricsOverride" || size.Width != 1920 {
				t.Fatalf("stale resize replayed: %+v", latest)
			}
			// Disconnect while another resize is waiting for a reply.
			viewer.Close()
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("disconnect did not cancel resize")
			}
			deadline := time.Now().Add(time.Second)
			for {
				worker.mu.Lock()
				pending := len(worker.pending)
				worker.mu.Unlock()
				if pending == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("resize retained pending CDP reply after disconnect")
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}
