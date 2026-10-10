package api

import (
	"encoding/json"
	"gostream3d/internal/orchestrator"
	"gostream3d/internal/proxy"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestManagementDiagnosticsAuthenticatedBackupPreviewAndRestore(t *testing.T) {
	sessions, _ := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	_ = sessions.EnableStore(filepath.Join(t.TempDir(), "sessions.json"))
	info, _ := sessions.CreatePublished(orchestrator.SessionOptions{TargetURL: "https://example.com/scene", Name: "Recover me"})
	ws, _ := proxy.NewWebSocketProxy("internal")
	router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost:8888", Sessions: sessions, Proxy: ws})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ method, path string }{{"GET", "store"}, {"POST", "store/backup"}, {"POST", "store/preview"}, {"POST", "store/restore"}, {"GET", "capacity"}, {"POST", "capacity"}, {"POST", "capacity/cancel"}} {
		if out := callChannelAPI(router, route.method, "/api/v1/admin/"+route.path, "{}", false); out.Code != http.StatusUnauthorized {
			t.Fatal(route, out.Code)
		}
	}
	out := callChannelAPI(router, "POST", "/api/v1/admin/store/backup", "{}", true)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	var saved struct {
		Data struct {
			ID string `json:"backup_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(out.Body.Bytes(), &saved)
	if err = sessions.Delete(t.Context(), info.Token); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"backup_id": saved.Data.ID})
	out = callChannelAPI(router, "POST", "/api/v1/admin/store/preview", string(body), true)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	var preview struct {
		Data orchestrator.RestorePreview `json:"data"`
	}
	_ = json.Unmarshal(out.Body.Bytes(), &preview)
	if len(preview.Data.Add) != 1 {
		t.Fatal(preview)
	}
	body, _ = json.Marshal(map[string]any{"backup_id": saved.Data.ID, "digest": preview.Data.Digest})
	out = callChannelAPI(router, "POST", "/api/v1/admin/store/restore", string(body), true)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	restored, ok := sessions.Lookup(info.Token)
	if !ok || !restored.RenderPaused {
		t.Fatal(restored)
	}
	if out = callChannelAPI(router, "POST", "/api/v1/admin/store/preview", `{"backup_id":"../sessions.json"}`, true); out.Code != 400 {
		t.Fatal(out.Code)
	}
	if out = callChannelAPI(router, "POST", "/api/v1/admin/capacity", "{}", true); out.Code != 501 {
		t.Fatal(out.Code)
	}
}
