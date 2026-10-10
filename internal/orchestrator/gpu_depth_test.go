package orchestrator

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGPUDepthProfileScope(t *testing.T) {
	for _, tc := range []struct {
		mode, target string
		want         bool
	}{
		{"nvidia", "https://campus.thingjs.com/s/p/scene", true},
		{"cpu", "https://campus.thingjs.com/s/p/scene", false},
		{"nvidia", "https://example.com/scene", true},
		{"nvidia", "https://campus.thingjs.com.example.org/scene", true},
		{"nvidia", "http://192.168.1.100:8080/demo", true},
		{"nvidia", "http://localhost/demo", true},
		{"cpu", "https://example.com/scene", false},
		{"nvidia", "file:///scene.html", false},
		{"nvidia", "data:text/html,scene", false},
		{"nvidia", "/scene", false},
		{"nvidia", "https:///scene", false},
		{"nvidia", "https://%/scene", false},
	} {
		if got := gpuDepthProfileRequired(tc.mode, tc.target); got != tc.want {
			t.Errorf("%s %s: got %v", tc.mode, tc.target, got)
		}
	}
}

func TestGPUPageInstallsPrecisionBeforeFirstNavigation(t *testing.T) {
	const target = "https://other-3d.example/scene"
	var installed, navigated atomic.Bool
	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]devtoolsTarget{{Type: "page", URL: "about:blank", WebSocketDebuggerURL: "ws" + strings.TrimPrefix(server.URL, "http") + "/page"}})
	})
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close()
		for {
			var req struct {
				ID     int            `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if err := c.ReadJSON(&req); err != nil {
				return
			}
			switch req.Method {
			case "Page.addScriptToEvaluateOnNewDocument":
				installed.Store(strings.Contains(req.Params["source"].(string), "DEPTH_COMPONENT24"))
			case "Page.navigate":
				if !installed.Load() {
					t.Error("scene navigated before precision profile was installed")
				}
				navigated.Store(req.Params["url"] == target)
			}
			_ = c.WriteJSON(map[string]any{"id": req.ID, "result": map[string]any{}})
		}
	})
	server = httptest.NewServer(mux)
	defer server.Close()
	host, portString, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(portString)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	control, err := initializeGPUPage(ctx, host, port, target)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	if !navigated.Load() {
		t.Fatal("GPU scene was not navigated")
	}
}

func TestGPUFrameProfileResumesAfterInitialization(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(strconv.FormatBool(fail), func(t *testing.T) {
			resumed := make(chan bool, 1)
			mux := http.NewServeMux()
			var server *httptest.Server
			mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
				c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer c.Close()
				installed := false
				for {
					var req struct {
						ID      int            `json:"id"`
						Session string         `json:"sessionId"`
						Method  string         `json:"method"`
						Params  map[string]any `json:"params"`
					}
					if c.ReadJSON(&req) != nil {
						return
					}
					if req.Method == "Page.navigate" {
						// Deliver the attachment before the navigation response to
						// exercise concurrent initialization and response dispatch.
						_ = c.WriteJSON(map[string]any{"method": "Target.attachedToTarget", "params": map[string]any{"sessionId": "child", "targetInfo": map[string]any{"type": "iframe"}}})
					}
					response := map[string]any{"id": req.ID, "result": map[string]any{}}
					if req.Session == "child" && req.Method == "Page.addScriptToEvaluateOnNewDocument" {
						if fail {
							response = map[string]any{"id": req.ID, "error": map[string]any{"code": -32000, "message": "frame detached"}}
						} else {
							installed = strings.Contains(req.Params["source"].(string), "DEPTH_COMPONENT24")
						}
					}
					if req.Session == "child" && req.Method == "Runtime.runIfWaitingForDebugger" {
						resumed <- installed
					}
					_ = c.WriteJSON(response)
				}
			})
			server = httptest.NewServer(mux)
			defer server.Close()
			host, portString, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
			port, _ := strconv.Atoi(portString)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			control, err := navigateGPUPage(ctx, host, port, "ws"+strings.TrimPrefix(server.URL, "http")+"/page", "https://other-3d.example/scene")
			if err != nil {
				t.Fatal(err)
			}
			defer control.Close()
			select {
			case installed := <-resumed:
				if installed == fail {
					t.Fatalf("frame installation result: %v, fail: %v", installed, fail)
				}
			case <-ctx.Done():
				t.Fatal("iframe was not resumed")
			}
		})
	}
}
