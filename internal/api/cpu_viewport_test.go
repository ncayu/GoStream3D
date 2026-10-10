package api

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"gostream3d/internal/orchestrator"
	"gostream3d/internal/proxy"
)

func TestCPUAdaptiveViewportPreservesFullResolution(t *testing.T) {
	for _, test := range []struct {
		mode, resolution, transport string
	}{
		{"cpu", "adaptive", "webrtc"}, {"nvidia", "adaptive", "webrtc"},
		{"cpu", "1920x1080", "webrtc"}, {"cpu", "adaptive", "jpeg"},
	} {
		t.Run(test.mode+test.resolution+test.transport, func(t *testing.T) {
			sessions, _ := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
			info, err := sessions.CreatePublished(orchestrator.SessionOptions{TargetURL: "https://example.com", RenderMode: test.mode, Resolution: test.resolution})
			if err != nil {
				t.Fatal(err)
			}
			ws, _ := proxy.NewWebSocketProxy("internal")
			router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost", Sessions: sessions, Proxy: ws, Transport: test.transport})
			if err != nil {
				t.Fatal(err)
			}
			out := callChannelAPI(router, http.MethodGet, "/api/v1/sessions/"+info.Token, "", false)
			if out.Code != 200 {
				t.Fatal(out.Body.String())
			}
			if !bytes.Contains(out.Body.Bytes(), []byte(`"max_width":3840`)) || !bytes.Contains(out.Body.Bytes(), []byte(`"max_height":2160`)) {
				t.Fatalf("unexpected limits: %s", out.Body.String())
			}
		})
	}
}
