package api

import (
	"encoding/json"
	"gostream3d/internal/orchestrator"
	"gostream3d/internal/proxy"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebRTCPerformanceCapabilityIsNotJPEG(t *testing.T) {
	sessions, _ := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	p, _ := proxy.NewWebSocketProxy("secret")
	r, e := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost", Sessions: sessions, Proxy: p, Transport: "webrtc"})
	if e != nil {
		t.Fatal(e)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/performance", nil)
	out := httptest.NewRecorder()
	r.ServeHTTP(out, req)
	if out.Code != http.StatusUnauthorized {
		t.Fatal("metrics must require admin")
	}
	req.Header.Set("Authorization", "Bearer key")
	out = httptest.NewRecorder()
	r.ServeHTTP(out, req)
	var body struct {
		Data struct {
			JPEG    bool                             `json:"jpeg_metrics_supported"`
			WebRTC  bool                             `json:"webrtc_metrics_supported"`
			Streams []orchestrator.StreamPerformance `json:"streams"`
		}
	}
	if json.Unmarshal(out.Body.Bytes(), &body) != nil || out.Code != 200 || body.Data.JPEG || !body.Data.WebRTC || body.Data.Streams == nil {
		t.Fatal(out.Body.String())
	}
}
