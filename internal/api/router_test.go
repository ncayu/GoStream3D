package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"gostream3d/internal/cpulimit"
	"gostream3d/internal/monitor"
	"gostream3d/internal/orchestrator"
	"gostream3d/internal/proxy"
)

type stubWorkers struct{}

func TestAdminCPULimitSettings(t *testing.T) {
	sessions, _ := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	wsProxy, _ := proxy.NewWebSocketProxy("internal")
	actual := 0
	control, err := cpulimit.New(filepath.Join(t.TempDir(), "cpu-settings.json"), 50, func(p int) error { actual = p; return nil })
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost:8888", Sessions: sessions, Proxy: wsProxy, CPUControl: control, CPUPercent: 50})
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		if authenticated {
			r.Header.Set("Authorization", "Bearer key")
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, r)
		return out
	}
	path := "/api/v1/admin/cpu/settings"
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		if out := call(method, path, `{"cpu_max_percent":40}`, false); out.Code != http.StatusUnauthorized {
			t.Fatalf("unprotected %s: %d", method, out.Code)
		}
	}
	if out := call(http.MethodPut, path, `{"cpu_max_percent":40}`, true); out.Code != http.StatusOK || actual != 40 {
		t.Fatalf("update failed: %d %s", out.Code, out.Body.String())
	}
	for _, body := range []string{`{}`, `{"cpu_max_percent":null}`, `{"cpu_max_percent":0}`, `{"cpu_max_percent":101}`, `{"cpu_max_percent":50.5}`, `{"cpu_max_percent":"40"}`} {
		if out := call(http.MethodPut, path, body, true); out.Code != http.StatusBadRequest || actual != 40 {
			t.Fatalf("invalid setting accepted: %s %d", body, out.Code)
		}
	}
	for _, p := range []string{path, "/api/v1/admin/resources"} {
		if out := call(http.MethodGet, p, "", true); out.Code != http.StatusOK || !bytes.Contains(out.Body.Bytes(), []byte(`"cpu_max_percent":40`)) {
			t.Fatalf("stale value: %s", out.Body.String())
		}
	}
}

func TestUnsupportedCPULimitCannotClaimSuccess(t *testing.T) {
	sessions, _ := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	wsProxy, _ := proxy.NewWebSocketProxy("internal")
	router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost:8888", Sessions: sessions, Proxy: wsProxy})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPut, "/api/v1/admin/cpu/settings", bytes.NewBufferString(`{"cpu_max_percent":40}`))
	r.Header.Set("Authorization", "Bearer key")
	r.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	router.ServeHTTP(out, r)
	if out.Code != http.StatusNotImplemented {
		t.Fatalf("unsupported node claimed success: %d", out.Code)
	}
}

func TestAdminMonitorRequiresAuthentication(t *testing.T) {
	sessions, err := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wsProxy, err := proxy.NewWebSocketProxy("internal")
	if err != nil {
		t.Fatal(err)
	}
	metrics := monitor.New()
	metrics.Sample(context.Background())
	router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost:8080", Sessions: sessions, Proxy: wsProxy, Monitor: metrics})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/monitor"
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, path, nil))
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated monitor status: %d", denied.Code)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		url := path
		if method == http.MethodPost {
			url += "/refresh"
		}
		request := httptest.NewRequest(method, url, nil)
		request.Header.Set("Authorization", "Bearer key")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"sampled_at"`)) {
			t.Fatalf("%s monitor status: %d %s", method, response.Code, response.Body.String())
		}
	}
	settingsPath := "/api/v1/admin/monitor/settings"
	update := httptest.NewRequest(http.MethodPut, settingsPath, bytes.NewBufferString(`{"interval_seconds":60}`))
	update.Header.Set("Authorization", "Bearer key")
	update.Header.Set("Content-Type", "application/json")
	updated := httptest.NewRecorder()
	router.ServeHTTP(updated, update)
	if updated.Code != http.StatusOK || metrics.Settings().IntervalSeconds != 60 {
		t.Fatalf("monitor interval was not updated: %d %s", updated.Code, updated.Body.String())
	}
	invalid := httptest.NewRequest(http.MethodPut, settingsPath, bytes.NewBufferString(`{"interval_seconds":5}`))
	invalid.Header.Set("Authorization", "Bearer key")
	invalid.Header.Set("Content-Type", "application/json")
	rejected := httptest.NewRecorder()
	router.ServeHTTP(rejected, invalid)
	if rejected.Code != http.StatusBadRequest || metrics.Settings().IntervalSeconds != 60 {
		t.Fatalf("invalid monitor interval accepted: %d %s", rejected.Code, rejected.Body.String())
	}
}

func (stubWorkers) StartWorker(_ context.Context, spec orchestrator.WorkerSpec) (orchestrator.Worker, error) {
	return orchestrator.Worker{Token: spec.Token, Screen: spec.Screen}, nil
}

type fullChannelWorkers struct{ stubWorkers }

func (fullChannelWorkers) StartWorker(context.Context, orchestrator.WorkerSpec) (orchestrator.Worker, error) {
	return orchestrator.Worker{}, orchestrator.ErrPortsExhausted
}

func TestViewerLimitMessages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		workers orchestrator.WorkerController
		status  int
	}{
		{name: "project quota", workers: stubWorkers{}, status: http.StatusTooManyRequests},
		{name: "server channels", workers: fullChannelWorkers{}, status: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessions, err := orchestrator.NewSessionManager(tc.workers, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			info, err := sessions.CreatePublished(orchestrator.SessionOptions{TargetURL: "https://example.com/3d"})
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "project quota" {
				_, release, err := sessions.Acquire(context.Background(), info.Token)
				if err != nil {
					t.Fatal(err)
				}
				defer release()
			}
			wsProxy, err := proxy.NewWebSocketProxy("internal")
			if err != nil {
				t.Fatal(err)
			}
			router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost:8888", Sessions: sessions, Proxy: wsProxy})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/ws/"+info.Token, nil))
			if response.Code != tc.status || !bytes.Contains(response.Body.Bytes(), []byte("路数超出限额，请查看配置")) {
				t.Fatalf("wrong limit response: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestEmbeddedAdminAssets(t *testing.T) {
	sessions, err := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wsProxy, err := proxy.NewWebSocketProxy("internal")
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost:8080", WebDist: filepath.Join(t.TempDir(), "missing"), Sessions: sessions, Proxy: wsProxy})
	if err != nil {
		t.Fatal(err)
	}
	admin := httptest.NewRecorder()
	router.ServeHTTP(admin, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if admin.Code != http.StatusOK || !bytes.Contains(admin.Body.Bytes(), []byte("GoStream3D")) {
		t.Fatalf("embedded admin: %d %s", admin.Code, admin.Body.String())
	}
	asset := regexp.MustCompile(`/assets/[^" ]+\.js`).FindString(admin.Body.String())
	if asset == "" {
		t.Fatal("embedded script missing")
	}
	js := httptest.NewRecorder()
	router.ServeHTTP(js, httptest.NewRequest(http.MethodGet, asset, nil))
	if js.Code != http.StatusOK || js.Body.Len() == 0 {
		t.Fatalf("embedded asset: %d", js.Code)
	}
}
func (stubWorkers) Stop(context.Context, string) error { return nil }

func TestAdminUpdatesExistingLink(t *testing.T) {
	sessions, err := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	info, err := sessions.CreatePublished(orchestrator.SessionOptions{TargetURL: "https://example.com/old"})
	if err != nil {
		t.Fatal(err)
	}
	proxyServer, err := proxy.NewWebSocketProxy("internal")
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost:8080", Sessions: sessions, Proxy: proxyServer})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/sessions/" + info.Token
	body := []byte(`{"target_url":"https://example.com/new","name":"Edited","resolution":"1280x720","fps":30}`)
	call := func(auth bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPut, "https://console.example:8443"+path, bytes.NewReader(body))
		if auth {
			request.Header.Set("Authorization", "Bearer key")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if response := call(false); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized edit: %d", response.Code)
	}
	if response := call(true); response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"stream_url":"https://console.example:8443/Stream3D/`+info.Token+`"`)) {
		t.Fatalf("edit returned %d: %s", response.Code, response.Body.String())
	}
	updated, ok := sessions.Lookup(info.Token)
	if !ok || updated.Name != "Edited" || updated.TargetURL != "https://example.com/new" {
		t.Fatalf("link not updated: %+v", updated)
	}
}

func TestHighFPSRequiresWindowsLocalTransport(t *testing.T) {
	for _, tc := range []struct {
		transport string
		fps       int
		want      int
	}{
		{"jpeg", 90, http.StatusCreated},
		{"jpeg", 120, http.StatusCreated},
		{"webrtc", 90, http.StatusBadRequest},
		{"webrtc", 120, http.StatusBadRequest},
	} {
		t.Run(tc.transport+"/"+strconv.Itoa(tc.fps), func(t *testing.T) {
			sessions, err := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			wsProxy, err := proxy.NewWebSocketProxy("internal")
			if err != nil {
				t.Fatal(err)
			}
			router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost:8080", Sessions: sessions, Proxy: wsProxy, Transport: tc.transport})
			if err != nil {
				t.Fatal(err)
			}
			body := []byte(fmt.Sprintf(`{"target_url":"https://example.com/scene","resolution":"1920x1080","fps":%d}`, tc.fps))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewReader(body))
			request.Header.Set("Authorization", "Bearer key")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("want %d, got %d: %s", tc.want, response.Code, response.Body.String())
			}
		})
	}
}

func TestAdminCanStopAndRestartRendering(t *testing.T) {
	sessions, err := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.EnableStore(filepath.Join(t.TempDir(), "sessions.json")); err != nil {
		t.Fatal(err)
	}
	info, err := sessions.CreatePublished(orchestrator.SessionOptions{TargetURL: "https://example.com/scene"})
	if err != nil {
		t.Fatal(err)
	}
	proxyServer, err := proxy.NewWebSocketProxy("internal")
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost:8080", Sessions: sessions, Proxy: proxyServer})
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, admin bool) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, nil)
		if admin {
			request.Header.Set("Authorization", "Bearer key")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	base := "/api/v1/admin/sessions/" + info.Token
	if response := call(http.MethodPost, base+"/stop", false); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized stop returned %d", response.Code)
	}
	if response := call(http.MethodPost, base+"/stop", true); response.Code != http.StatusOK {
		t.Fatalf("stop returned %d: %s", response.Code, response.Body.String())
	}
	if response := call(http.MethodPost, base+"/warm?async=1", true); response.Code != http.StatusConflict {
		t.Fatalf("stopped renderer accepted preload: %d", response.Code)
	}
	if response := call(http.MethodGet, "/api/v1/ws/"+info.Token, false); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("stopped viewer returned %d", response.Code)
	}
	if response := call(http.MethodPost, base+"/start", true); response.Code != http.StatusOK {
		t.Fatalf("start returned %d: %s", response.Code, response.Body.String())
	}
	if status := sessions.List()[0]; status.RenderPaused || !status.Running {
		t.Fatalf("start did not resume rendering: %+v", status)
	}
}

type delayedWorkers struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (w delayedWorkers) StartWorker(ctx context.Context, spec orchestrator.WorkerSpec) (orchestrator.Worker, error) {
	close(w.started)
	defer close(w.finished)
	select {
	case <-w.release:
		return orchestrator.Worker{Token: spec.Token, Screen: spec.Screen}, nil
	case <-ctx.Done():
		return orchestrator.Worker{}, ctx.Err()
	}
}

func (delayedWorkers) Stop(context.Context, string) error { return nil }

func TestAsyncCreatePersistsBeforePreloadAndSurvivesRequestCancel(t *testing.T) {
	workers := delayedWorkers{started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	sessions, err := orchestrator.NewSessionManager(workers, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(t.TempDir(), "sessions.json")
	if err := sessions.EnableStore(store); err != nil {
		t.Fatal(err)
	}
	wsProxy, err := proxy.NewWebSocketProxy("internal")
	if err != nil {
		t.Fatal(err)
	}
	serverCtx, stopServer := context.WithCancel(context.Background())
	defer stopServer()
	router, err := NewRouter(RouterConfig{AdminAPIKey: "secret", PublicBaseURL: "https://stream.example", Sessions: sessions, Proxy: wsProxy, PreloadContext: serverCtx})
	if err != nil {
		t.Fatal(err)
	}
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions?async=1", bytes.NewBufferString(`{"target_url":"https://example.com/3d"}`)).WithContext(requestCtx)
	req.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	responseDone := make(chan struct{})
	go func() { router.ServeHTTP(response, req); close(responseDone) }()
	select {
	case <-responseDone:
	case <-time.After(2 * time.Second):
		t.Fatal("create request waited for renderer")
	}
	if response.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data struct {
			Token     string `json:"token"`
			Preloaded bool   `json:"preloaded"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Token == "" || payload.Data.Preloaded {
		t.Fatalf("unexpected async response: %+v", payload)
	}
	data, err := os.ReadFile(store)
	if err != nil || !bytes.Contains(data, []byte(payload.Data.Token)) {
		t.Fatalf("link was not persisted before preload: %v", err)
	}
	select {
	case <-workers.started:
	case <-time.After(2 * time.Second):
		t.Fatal("background preload did not start")
	}
	cancelRequest()
	close(workers.release)
	select {
	case <-workers.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("preload was canceled with request")
	}
	if status := sessions.List(); len(status) != 1 || !status[0].Running {
		t.Fatalf("project did not become ready: %+v", status)
	}
}

func TestCreateAndOpenShortLink(t *testing.T) {
	dist := t.TempDir()
	if err := os.Mkdir(filepath.Join(dist, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>player</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	sessions, err := orchestrator.NewSessionManager(stubWorkers{}, 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	wsProxy, err := proxy.NewWebSocketProxy("internal")
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(RouterConfig{
		AdminAPIKey: "admin-secret", PublicBaseURL: "https://stream.example",
		WebDist: dist, Sessions: sessions, Proxy: wsProxy,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"target_url":"https://example.com/scene","resolution":"1280x720","fps":30,"name":"园区大屏","concurrency_mode":"single","render_mode":"cpu"}`)
	unauth := httptest.NewRecorder()
	router.ServeHTTP(unauth, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewReader(body)))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauth.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "http://192.168.1.100:8888/api/v1/sessions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-secret")
	created := httptest.NewRecorder()
	router.ServeHTTP(created, req)
	if created.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", created.Code, created.Body.String())
	}
	var payload struct {
		Data struct {
			Token     string `json:"token"`
			StreamURL string `json:"stream_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.StreamURL != "http://192.168.1.100:8888/Stream3D/"+payload.Data.Token {
		t.Fatalf("wrong short URL: %s", payload.Data.StreamURL)
	}
	view := httptest.NewRecorder()
	router.ServeHTTP(view, httptest.NewRequest(http.MethodGet, "/Stream3D/"+payload.Data.Token, nil))
	if view.Code != http.StatusOK || view.Body.String() != "<html>player</html>" {
		t.Fatalf("viewer response: %d %q", view.Code, view.Body.String())
	}
	legacy := httptest.NewRecorder()
	router.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/v/"+payload.Data.Token+"?x=1", nil))
	if legacy.Code != http.StatusTemporaryRedirect || legacy.Header().Get("Location") != "/Stream3D/"+payload.Data.Token+"?x=1" {
		t.Fatalf("legacy redirect: %d %s", legacy.Code, legacy.Header().Get("Location"))
	}
	listRequest := httptest.NewRequest(http.MethodGet, "http://192.168.1.100:8888/api/v1/admin/sessions", nil)
	listRequest.Header.Set("Authorization", "Bearer admin-secret")
	listed := httptest.NewRecorder()
	router.ServeHTTP(listed, listRequest)
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte(`"max_viewers":1`)) ||
		!bytes.Contains(listed.Body.Bytes(), []byte(`"stream_url":"http://192.168.1.100:8888/Stream3D/`+payload.Data.Token+`"`)) {
		t.Fatalf("admin list: %d %s", listed.Code, listed.Body.String())
	}
	warmRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/sessions/"+payload.Data.Token+"/warm", nil)
	warmRequest.Header.Set("Authorization", "Bearer admin-secret")
	warmed := httptest.NewRecorder()
	router.ServeHTTP(warmed, warmRequest)
	if warmed.Code != http.StatusOK {
		t.Fatalf("warm returned %d: %s", warmed.Code, warmed.Body.String())
	}
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/sessions/"+payload.Data.Token, nil)
	deleteRequest.Header.Set("Authorization", "Bearer admin-secret")
	deleted := httptest.NewRecorder()
	router.ServeHTTP(deleted, deleteRequest)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete returned %d", deleted.Code)
	}
	for _, prefix := range []string{"/Stream3D/", "/v/"} {
		missing := httptest.NewRecorder()
		router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, prefix+payload.Data.Token, nil))
		if missing.Code != http.StatusNotFound {
			t.Fatalf("deleted link %s returned %d", prefix, missing.Code)
		}
	}
}

func TestSessionListUsesCurrentOrigin(t *testing.T) {
	sessions, err := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	info, err := sessions.CreatePublished(orchestrator.SessionOptions{TargetURL: "https://example.com/scene"})
	if err != nil {
		t.Fatal(err)
	}
	wsProxy, err := proxy.NewWebSocketProxy("internal")
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost:8888", Sessions: sessions, Proxy: wsProxy})
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"http://127.0.0.1:8888", "http://192.168.1.100:8888", "https://platform.example:8443", "http://[::1]:8888"} {
		t.Run(origin, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, origin+"/api/v1/admin/sessions", nil)
			request.Header.Set("Authorization", "Bearer key")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"stream_url":"`+origin+`/Stream3D/`+info.Token+`"`)) {
				t.Fatalf("origin mismatch: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAdminAccountPasswordSession(t *testing.T) {
	sessions, err := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wsProxy, err := proxy.NewWebSocketProxy("internal")
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(RouterConfig{
		AdminUsername: "operator", AdminPassword: "long-private-password",
		PublicBaseURL: "http://gateway.example", WebDist: filepath.Join(t.TempDir(), "missing"),
		Sessions: sessions, Proxy: wsProxy,
	})
	if err != nil {
		t.Fatal(err)
	}
	postLogin := func(username, password, origin string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": username, "password": password})
		request := httptest.NewRequest(http.MethodPost, "http://gateway.example/api/v1/auth/login", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if got := postLogin("operator", "wrong-password", "http://gateway.example"); got.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status: %d", got.Code)
	}
	if got := postLogin("operator", "long-private-password", "http://other.example"); got.Code != http.StatusForbidden {
		t.Fatalf("foreign origin status: %d", got.Code)
	}
	if got := postLogin("operator", "long-private-password", "https://gateway.example"); got.Code != http.StatusForbidden {
		t.Fatalf("cross-scheme origin status: %d", got.Code)
	}
	login := postLogin("operator", "long-private-password", "http://gateway.example")
	if login.Code != http.StatusOK || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login status: %d, cookies: %d", login.Code, len(login.Result().Cookies()))
	}
	cookie := login.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api/v1" {
		t.Fatalf("insecure session cookie: %+v", cookie)
	}
	me := httptest.NewRequest(http.MethodGet, "http://gateway.example/api/v1/auth/me", nil)
	me.AddCookie(cookie)
	meResponse := httptest.NewRecorder()
	router.ServeHTTP(meResponse, me)
	if meResponse.Code != http.StatusOK || !bytes.Contains(meResponse.Body.Bytes(), []byte(`"operator"`)) {
		t.Fatalf("session check: %d %s", meResponse.Code, meResponse.Body.String())
	}
	createBody := []byte(`{"target_url":"https://example.com/scene","resolution":"1280x720","fps":30,"concurrency_mode":"single","render_mode":"cpu"}`)
	create := httptest.NewRequest(http.MethodPost, "http://gateway.example/api/v1/sessions", bytes.NewReader(createBody))
	create.Header.Set("Origin", "http://gateway.example")
	create.AddCookie(cookie)
	created := httptest.NewRecorder()
	router.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("cookie-authenticated create: %d %s", created.Code, created.Body.String())
	}
	var payload struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	view := httptest.NewRecorder()
	router.ServeHTTP(view, httptest.NewRequest(http.MethodGet, "/Stream3D/"+payload.Data.Token, nil))
	if view.Code != http.StatusOK {
		t.Fatalf("public viewer: %d", view.Code)
	}
	logout := httptest.NewRequest(http.MethodPost, "http://gateway.example/api/v1/auth/logout", nil)
	logout.AddCookie(cookie)
	loggedOut := httptest.NewRecorder()
	router.ServeHTTP(loggedOut, logout)
	if loggedOut.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", loggedOut.Code)
	}
	again := httptest.NewRequest(http.MethodGet, "http://gateway.example/api/v1/auth/me", nil)
	again.AddCookie(cookie)
	afterLogout := httptest.NewRecorder()
	router.ServeHTTP(afterLogout, again)
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("session survived logout: %d", afterLogout.Code)
	}
}

func TestLegacyKeyCanLogInAsAdmin(t *testing.T) {
	sessions, err := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wsProxy, err := proxy.NewWebSocketProxy("internal")
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(RouterConfig{
		AdminAPIKey: "existing-api-key", PublicBaseURL: "http://gateway.example",
		WebDist: filepath.Join(t.TempDir(), "missing"), Sessions: sessions, Proxy: wsProxy,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://gateway.example/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"existing-api-key"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || len(response.Result().Cookies()) != 1 {
		t.Fatalf("legacy login: %d %s", response.Code, response.Body.String())
	}
}
