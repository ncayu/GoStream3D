package api

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"gostream3d/internal/orchestrator"
	"gostream3d/internal/proxy"
)

func TestPresentationAPIAuthenticatedAndPublicVisibility(t *testing.T) {
	sessions, _ := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	info, err := sessions.CreatePublished(orchestrator.SessionOptions{TargetURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	wsProxy, _ := proxy.NewWebSocketProxy("internal")
	router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost", Sessions: sessions, Proxy: wsProxy})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/sessions/" + info.Token + "/presentation"
	if out := callChannelAPI(router, http.MethodPut, path, `{"show_fps":false}`, false); out.Code != http.StatusUnauthorized {
		t.Fatalf("unprotected toggle: %d", out.Code)
	}
	for _, body := range []string{`{}`, `{"show_fps":null}`, `{"show_fps":"false"}`, `{"show_fps":0}`, `{"show_fps":false,"unknown":1}`, `{"show_fps":false} {}`} {
		if out := callChannelAPI(router, http.MethodPut, path, body, true); out.Code != http.StatusBadRequest {
			t.Fatalf("invalid toggle: %s: %d", body, out.Code)
		}
	}
	for _, value := range []string{"false", "true"} {
		out := callChannelAPI(router, http.MethodPut, path, `{"show_fps":`+value+`}`, true)
		if out.Code != http.StatusOK || !bytes.Contains(out.Body.Bytes(), []byte(`"show_fps":`+value)) {
			t.Fatalf("save response: %d %s", out.Code, out.Body.String())
		}
		out = callChannelAPI(router, http.MethodGet, "/api/v1/sessions/"+info.Token, "", false)
		if out.Code != http.StatusOK || out.Header().Get("Cache-Control") != "no-store" || !bytes.Contains(out.Body.Bytes(), []byte(`"show_fps":`+value)) {
			t.Fatalf("viewer setting: %d %s", out.Code, out.Body.String())
		}
	}
	if out := callChannelAPI(router, http.MethodPut, "/api/v1/admin/sessions/missing/presentation", `{"show_fps":false}`, true); out.Code != http.StatusNotFound {
		t.Fatalf("missing project: %d", out.Code)
	}
	out := callChannelAPI(router, http.MethodPost, "/api/v1/sessions?async=1", `{"target_url":"https://example.com","show_fps":false}`, true)
	if out.Code != http.StatusAccepted {
		t.Fatalf("create with FPS choice: %d %s", out.Code, out.Body.String())
	}
}
