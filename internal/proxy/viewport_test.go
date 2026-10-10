package proxy

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"gostream3d/internal/viewport"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdaptiveViewportUsesPrivateAdminConnection(t *testing.T) {
	requests := make(chan viewport.Size, 8)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		admin := r.URL.Query().Get("password") == "private-admin"
		for {
			kind, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if admin {
				var request struct {
					Event               string `json:"event"`
					Width, Height, Rate int
				}
				if json.Unmarshal(data, &request) != nil || request.Event != "screen/set" || request.Rate != 60 {
					t.Error("unexpected admin operation")
					return
				}
				requests <- viewport.Size{Type: "resize", Width: request.Width, Height: request.Height}
				_ = conn.WriteJSON(map[string]any{"event": "screen/resolution", "width": request.Width, "height": request.Height})
			} else {
				_ = conn.WriteMessage(kind, data)
			}
		}
	}))
	defer upstream.Close()
	p, _ := NewWebSocketProxy("ordinary-user", "private-admin")
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := viewport.WithSettings(r, r.URL.Path != "/fixed", 60)
		_ = p.Serve(w, request, upstream.URL)
	}))
	defer gateway.Close()
	for _, path := range []string{"/adaptive", "/cpu", "/fixed"} {
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		width, height := 1280, 800
		if path == "/cpu" {
			width, height = 1920, 1080
		}
		_ = conn.WriteJSON(viewport.Size{Type: "resize", Width: width, Height: height})
		_ = conn.WriteJSON(viewport.Size{Type: "resize", Width: 20000, Height: 800})
		// Ordinary signaling retains user permissions and proceeds while resizing.
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"client/heartbeat"}`))
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil || string(data) != `{"event":"client/heartbeat"}` {
			t.Fatalf("signaling altered: %s %v", data, err)
		}
		if path != "/fixed" {
			select {
			case size := <-requests:
				if size.Width != width || size.Height != height {
					t.Fatal(size)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("viewport was not applied")
			}
		} else {
			select {
			case size := <-requests:
				t.Fatalf("fixed viewer resized: %+v", size)
			case <-time.After(450 * time.Millisecond):
			}
		}
		conn.Close()
	}
}
