package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gostream3d/internal/orchestrator"
	"gostream3d/internal/proxy"
)

func channelTestPorts(t *testing.T) *orchestrator.PortAllocator {
	t.Helper()
	ports, err := orchestrator.NewPortAllocator(
		orchestrator.PortRange{Start: 30000, End: 30002},
		orchestrator.PortRange{Start: 31000, End: 31002},
		orchestrator.PortRange{Start: 52000, End: 52023}, 8,
	)
	if err != nil {
		t.Fatal(err)
	}
	return ports
}

func channelTestRouter(t *testing.T, ports *orchestrator.PortAllocator, control *orchestrator.ChannelSettingsService) http.Handler {
	t.Helper()
	sessions, err := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetChannelCapacitySource(ports.Capacity)
	wsProxy, err := proxy.NewWebSocketProxy("internal")
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost:8888", Sessions: sessions, Proxy: wsProxy, ChannelControl: control})
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func callChannelAPI(router http.Handler, method, path, body string, authenticated bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	if authenticated {
		request.Header.Set("Authorization", "Bearer key")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestSessionQuotaIsIndependentOfCapacityAndPreloadRespectsQuota(t *testing.T) {
	ports := channelTestPorts(t)
	control, err := orchestrator.NewChannelSettings(filepath.Join(t.TempDir(), "channels.json"), ports)
	if err != nil {
		t.Fatal(err)
	}
	router := channelTestRouter(t, ports, control)
	for _, body := range []string{
		`{"target_url":"https://example.com","concurrency_mode":"single","max_viewers":2}`,
		`{"target_url":"https://example.com","concurrency_mode":"single","reserved_preloads":2}`,
		`{"target_url":"https://example.com","concurrency_mode":"single","preload_count":2}`,
		`{"target_url":"https://example.com","concurrency_mode":"multi","max_viewers":101}`,
		`{"target_url":"https://example.com","concurrency_mode":"multi","max_viewers":3,"preload_count":4}`,
		`{"target_url":"https://example.com","concurrency_mode":"multi","max_viewers":3,"preload_count":2,"reserved_preloads":3}`,
	} {
		out := callChannelAPI(router, http.MethodPost, "/api/v1/sessions?async=1", body, true)
		if out.Code != http.StatusBadRequest {
			t.Fatalf("invalid routes accepted: %s: %d %s", body, out.Code, out.Body.String())
		}
	}
	if err := control.SetMaxChannels(2); err != nil {
		t.Fatal(err)
	}
	out := callChannelAPI(router, http.MethodPost, "/api/v1/sessions?async=1", `{"target_url":"https://example.com","concurrency_mode":"multi"}`, true)
	if out.Code != http.StatusAccepted || !bytes.Contains(out.Body.Bytes(), []byte(`"max_viewers":3`)) {
		t.Fatalf("default project quota should be independent of pool: %d %s", out.Code, out.Body.String())
	}
	var response struct {
		Data orchestrator.SessionInfo `json:"data"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &response); err != nil || response.Data.Token == "" {
		t.Fatalf("missing token: %s", out.Body.String())
	}
	out = callChannelAPI(router, http.MethodPut, "/api/v1/admin/sessions/"+response.Data.Token, `{"target_url":"https://example.com","concurrency_mode":"multi","max_viewers":8,"preload_count":6,"reserved_preloads":5}`, true)
	if out.Code != http.StatusOK {
		t.Fatalf("edit above server capacity rejected: %d %s", out.Code, out.Body.String())
	}
	out = callChannelAPI(router, http.MethodPut, "/api/v1/admin/sessions/"+response.Data.Token, `{"target_url":"https://example.com","concurrency_mode":"multi","max_viewers":3,"preload_count":4}`, true)
	if out.Code != http.StatusBadRequest {
		t.Fatalf("edit accepted preload above project quota: %d %s", out.Code, out.Body.String())
	}
	if err := control.SetMaxChannels(1); err != nil {
		t.Fatal(err)
	}
	for _, reserved := range []string{"0", "1"} {
		out := callChannelAPI(router, http.MethodPost, "/api/v1/sessions?async=1", `{"target_url":"https://example.com","concurrency_mode":"single","reserved_preloads":`+reserved+`}`, true)
		if out.Code != http.StatusAccepted {
			t.Fatalf("valid single reservation rejected: %d %s", out.Code, out.Body.String())
		}
	}
	out = callChannelAPI(router, http.MethodPost, "/api/v1/sessions?async=1", `{"target_url":"https://example.com","concurrency_mode":"multi","max_viewers":10,"preload_count":8,"reserved_preloads":7}`, true)
	if out.Code != http.StatusAccepted {
		t.Fatalf("multi quota above single-channel pool rejected: %d %s", out.Code, out.Body.String())
	}
}

func TestAdminChannelSettingsExpandAndPersist(t *testing.T) {
	ports := channelTestPorts(t)
	path := filepath.Join(t.TempDir(), "channel-settings.json")
	control, err := orchestrator.NewChannelSettings(path, ports)
	if err != nil {
		t.Fatal(err)
	}
	router := channelTestRouter(t, ports, control)
	endpoint := "/api/v1/admin/channels/settings"
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		if out := callChannelAPI(router, method, endpoint, `{"max_channels":10}`, false); out.Code != http.StatusUnauthorized {
			t.Fatalf("unprotected %s: %d", method, out.Code)
		}
	}
	initial := callChannelAPI(router, http.MethodGet, endpoint, "", true)
	if initial.Code != http.StatusOK || !bytes.Contains(initial.Body.Bytes(), []byte(`"max_channels":3`)) || initial.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("initial channel settings: %d %s", initial.Code, initial.Body.String())
	}
	updated := callChannelAPI(router, http.MethodPut, endpoint, `{"max_channels":10}`, true)
	if updated.Code != http.StatusOK || !bytes.Contains(updated.Body.Bytes(), []byte(`"max_channels":10`)) || ports.Capacity() != 10 {
		t.Fatalf("channel capacity did not expand: %d %s", updated.Code, updated.Body.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		MaxChannels int `json:"max_channels"`
	}
	if err := json.Unmarshal(data, &saved); err != nil || saved.MaxChannels != 10 {
		t.Fatalf("setting was not persisted: %s, %v", data, err)
	}
	for _, endpoint := range []string{"/api/v1/admin/resources", "/api/v1/admin/channels/settings"} {
		out := callChannelAPI(router, http.MethodGet, endpoint, "", true)
		var decoded struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		if out.Code != http.StatusOK || json.Unmarshal(out.Body.Bytes(), &decoded) != nil {
			t.Fatalf("settings read failed: %d %s", out.Code, out.Body.String())
		}
		key := "max_channels"
		if endpoint == "/api/v1/admin/resources" {
			key = "channel_capacity"
			if string(decoded.Data["channel_capacity_source"]) != `"configured"` || string(decoded.Data["hardware_capacity_detected"]) != "false" {
				t.Fatalf("configured capacity falsely claims hardware detection: %s", out.Body.String())
			}
		}
		if string(decoded.Data[key]) != "10" {
			t.Fatalf("stale capacity response: %s", out.Body.String())
		}
	}
	for _, body := range []string{`{}`, `{"max_channels":null}`, `{"max_channels":0}`, `{"max_channels":101}`, `{"max_channels":2.5}`, `{"max_channels":"10"}`, `{`} {
		out := callChannelAPI(router, http.MethodPut, endpoint, body, true)
		if out.Code != http.StatusBadRequest || ports.Capacity() != 10 {
			t.Fatalf("invalid setting accepted: %s %d", body, out.Code)
		}
	}
}

func TestAdminChannelSettingsRejectOccupiedShrink(t *testing.T) {
	ports := channelTestPorts(t)
	control, err := orchestrator.NewChannelSettings(filepath.Join(t.TempDir(), "channel-settings.json"), ports)
	if err != nil {
		t.Fatal(err)
	}
	leases := make([]orchestrator.PortLease, 3)
	for i := range leases {
		leases[i], err = ports.Allocate()
		if err != nil {
			t.Fatal(err)
		}
		defer leases[i].Release()
	}
	leases[0].Release()
	leases[1].Release()
	router := channelTestRouter(t, ports, control)
	endpoint := "/api/v1/admin/channels/settings"
	state := callChannelAPI(router, http.MethodGet, endpoint, "", true)
	if !bytes.Contains(state.Body.Bytes(), []byte(`"active_channels":1`)) {
		t.Fatalf("leased channels missing: %s", state.Body.String())
	}
	rejected := callChannelAPI(router, http.MethodPut, endpoint, `{"max_channels":2}`, true)
	if rejected.Code != http.StatusConflict || ports.Capacity() != 3 || !bytes.Contains(rejected.Body.Bytes(), []byte("停止对应渲染实例")) {
		t.Fatalf("occupied channel removed: %d %s", rejected.Code, rejected.Body.String())
	}
	leases[2].Release()
	updated := callChannelAPI(router, http.MethodPut, endpoint, `{"max_channels":2}`, true)
	if updated.Code != http.StatusOK || ports.Capacity() != 2 {
		t.Fatalf("safe shrink failed: %d %s", updated.Code, updated.Body.String())
	}
}

func TestAdminChannelSettingsUnsupported(t *testing.T) {
	router := channelTestRouter(t, channelTestPorts(t), nil)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		out := callChannelAPI(router, method, "/api/v1/admin/channels/settings", `{"max_channels":10}`, true)
		if out.Code != http.StatusNotImplemented {
			t.Fatalf("unsupported channel configuration claimed success: %s %d", method, out.Code)
		}
	}
}

func TestAdminChannelSettingsPortConflictAndPersistenceFailure(t *testing.T) {
	t.Run("port conflict", func(t *testing.T) {
		ports, err := orchestrator.NewPortAllocator(orchestrator.PortRange{Start: 30000, End: 30002}, orchestrator.PortRange{Start: 30004, End: 30006}, orchestrator.PortRange{Start: 52000, End: 52023}, 8)
		if err != nil {
			t.Fatal(err)
		}
		control, err := orchestrator.NewChannelSettings(filepath.Join(t.TempDir(), "channel-settings.json"), ports)
		if err != nil {
			t.Fatal(err)
		}
		out := callChannelAPI(channelTestRouter(t, ports, control), http.MethodPut, "/api/v1/admin/channels/settings", `{"max_channels":10}`, true)
		if out.Code != http.StatusBadRequest || ports.Capacity() != 3 {
			t.Fatalf("overlapping port ranges accepted: %d %s", out.Code, out.Body.String())
		}
	})
	t.Run("persistence failure", func(t *testing.T) {
		ports := channelTestPorts(t)
		blockedParent := filepath.Join(t.TempDir(), "blocked")
		control, err := orchestrator.NewChannelSettings(filepath.Join(blockedParent, "channel-settings.json"), ports)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(blockedParent, []byte("regular file prevents settings directory creation"), 0600); err != nil {
			t.Fatal(err)
		}
		out := callChannelAPI(channelTestRouter(t, ports, control), http.MethodPut, "/api/v1/admin/channels/settings", `{"max_channels":10}`, true)
		if out.Code != http.StatusInternalServerError || ports.Capacity() != 3 {
			t.Fatalf("unpersisted capacity became active: %d %s", out.Code, out.Body.String())
		}
	})
}
