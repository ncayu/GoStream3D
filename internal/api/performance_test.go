package api

import (
	"bytes"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"gostream3d/internal/orchestrator"
	"gostream3d/internal/proxy"
)

func TestPerformanceAPIRequiresAuthValidatesAndPersists(t *testing.T) {
	sessions, _ := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	control, err := orchestrator.NewPerformanceSettings(filepath.Join(t.TempDir(), "performance.json"))
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetPerformanceSettingsSource(control.Settings)
	wsProxy, _ := proxy.NewWebSocketProxy("internal")
	router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost:8888", Sessions: sessions, Proxy: wsProxy, PerformanceControl: control, Transport: "jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ method, path string }{{http.MethodGet, "/api/v1/admin/performance"}, {http.MethodPut, "/api/v1/admin/performance/settings"}} {
		if out := callChannelAPI(router, entry.method, entry.path, "{}", false); out.Code != http.StatusUnauthorized {
			t.Fatal(out.Code)
		}
	}
	path := "/api/v1/admin/performance/settings"
	for _, body := range []string{`{}`, `{"gpu_fps_budget":29,"reclaim_idle_preloads":true}`, `{"gpu_fps_budget":1201,"reclaim_idle_preloads":true}`} {
		if out := callChannelAPI(router, http.MethodPut, path, body, true); out.Code != http.StatusBadRequest {
			t.Fatal(out.Code, out.Body.String())
		}
	}
	if out := callChannelAPI(router, http.MethodPut, path, `{"gpu_fps_budget":90,"reclaim_idle_preloads":false}`, true); out.Code != http.StatusOK {
		t.Fatal(out.Code, out.Body.String())
	}
	if got := sessions.PerformanceSettings(); got.GPUFPSBudget != 90 || got.ReclaimIdlePreloads {
		t.Fatal(got)
	}
	if out := callChannelAPI(router, http.MethodGet, "/api/v1/admin/performance", "", true); out.Code != http.StatusOK || !bytes.Contains(out.Body.Bytes(), []byte(`"gpu_fps_budget":90`)) {
		t.Fatal(out.Code, out.Body.String())
	}
	// Creation and editing share the validated per-project options.
	for _, body := range []string{
		`{"target_url":"https://example.com/scene","render_mode":"nvidia","frame_policy":"animation","preload_count":1,"reserved_preloads":1}`,
		`{"target_url":"https://example.com/scene","render_mode":"nvidia","frame_policy":"invalid"}`,
		`{"target_url":"https://example.com/scene","preload_count":1,"reserved_preloads":2}`,
	} {
		out := callChannelAPI(router, http.MethodPost, "/api/v1/sessions?async=1", body, true)
		if bytes.Contains([]byte(body), []byte(`"frame_policy":"animation"`)) {
			if out.Code != http.StatusAccepted {
				t.Fatal(out.Code, out.Body.String())
			}
			if got := sessions.List()[0]; got.FramePolicy != "animation" || got.ReservedPreloads != 1 {
				t.Fatal(got)
			}
		} else if out.Code != http.StatusBadRequest {
			t.Fatal(out.Code, out.Body.String())
		}
	}
}
