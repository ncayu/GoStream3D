package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gostream3d/internal/appearance"
	"gostream3d/internal/orchestrator"
	"gostream3d/internal/proxy"
)

type failedAppearanceStore struct{}

func (failedAppearanceStore) Theme(context.Context) (string, error) {
	return "", errors.New("read failure")
}
func (failedAppearanceStore) SaveTheme(context.Context, string) error {
	return errors.New("write failure")
}

func TestAppearanceAPI(t *testing.T) {
	store, err := appearance.Open(filepath.Join(t.TempDir(), "appearance.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sessions, _ := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	wsProxy, _ := proxy.NewWebSocketProxy("internal")
	cfg := RouterConfig{AdminAPIKey: "key", PublicBaseURL: "http://localhost:8888", Sessions: sessions, Proxy: wsProxy, Appearance: store}
	router, err := NewRouter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, body string, auth bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "/api/v1/admin/appearance", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		if auth {
			request.Header.Set("Authorization", "Bearer key")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	for _, method := range []string{"GET", "PUT"} {
		if out := call(method, `{"theme":"rose"}`, false); out.Code != http.StatusUnauthorized {
			t.Fatalf("unprotected %s: %d", method, out.Code)
		}
	}
	if out := call("GET", "", true); out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte(`"theme":"blue-tech"`)) || out.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("default: %d %s", out.Code, out.Body.String())
	}
	if out := call("PUT", `{"theme":"aurora"}`, true); out.Code != 200 {
		t.Fatalf("save: %d %s", out.Code, out.Body.String())
	}
	for _, body := range []string{`{}`, `null`, `{"theme":null}`, `{"theme":"invalid"}`, `{"theme":4}`, `{"theme":"rose","extra":true}`, `{"theme":"rose"} {}`, `{"theme":"rose"`} {
		if out := call("PUT", body, true); out.Code != 400 {
			t.Fatalf("accepted %s: %d", body, out.Code)
		}
	}
	if out := call("GET", "", true); out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte(`"theme":"aurora"`)) {
		t.Fatalf("invalid input changed saved value: %s", out.Body.String())
	}
	cfg.Appearance = failedAppearanceStore{}
	router, err = NewRouter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PUT"} {
		if out := call(method, `{"theme":"rose"}`, true); out.Code != 500 || bytes.Contains(out.Body.Bytes(), []byte(`"code":0`)) {
			t.Fatalf("failure claimed success: %d %s", out.Code, out.Body.String())
		}
	}
	cfg.Appearance = nil
	router, err = NewRouter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if out := call("PUT", `{"theme":"rose"}`, true); out.Code != 501 {
		t.Fatalf("unsupported save: %d", out.Code)
	}
}
