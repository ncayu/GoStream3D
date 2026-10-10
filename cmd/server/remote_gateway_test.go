package main

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gostream3d/internal/api"
	"gostream3d/internal/config"
	"gostream3d/internal/orchestrator"
	"gostream3d/internal/proxy"
)

type remoteTestWorkers struct{}

func (remoteTestWorkers) StartWorker(_ context.Context, spec orchestrator.WorkerSpec) (orchestrator.Worker, error) {
	return orchestrator.Worker{Token: spec.Token, Screen: spec.Screen}, nil
}
func (remoteTestWorkers) Stop(context.Context, string) error { return nil }

func TestRemoteGatewayServesAdminWhenUpstreamIsNeko(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer upstream.Close()
	handler, err := newRemoteGatewayHandler(config.Config{
		RemoteBaseURL: upstream.URL,
		WebDist:       filepath.Join(t.TempDir(), "missing"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	admin := httptest.NewRecorder()
	handler.ServeHTTP(admin, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if admin.Code != http.StatusOK || !strings.Contains(admin.Body.String(), "GoStream3D") {
		t.Fatalf("admin: %d %s", admin.Code, admin.Body.String())
	}
	asset := regexp.MustCompile(`/assets/[^" ]+\.js`).FindString(admin.Body.String())
	if asset == "" {
		t.Fatal("admin script missing")
	}
	js := httptest.NewRecorder()
	handler.ServeHTTP(js, httptest.NewRequest(http.MethodGet, asset, nil))
	if js.Code != http.StatusOK || js.Body.Len() == 0 {
		t.Fatalf("admin asset: %d", js.Code)
	}
	api := httptest.NewRecorder()
	handler.ServeHTTP(api, httptest.NewRequest(http.MethodGet, "/api/v1/admin/sessions", nil))
	if api.Code != http.StatusBadGateway || !strings.Contains(api.Body.String(), "不能指向 Neko 服务") {
		t.Fatalf("wrong upstream: %d %s", api.Code, api.Body.String())
	}
}

func TestRemoteGatewayForwardsAccountLogin(t *testing.T) {
	sessions, err := orchestrator.NewSessionManager(remoteTestWorkers{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wsProxy, err := proxy.NewWebSocketProxy("internal")
	if err != nil {
		t.Fatal(err)
	}
	linuxRouter, err := api.NewRouter(api.RouterConfig{
		AdminUsername: "operator", AdminPassword: "long-private-password",
		PublicBaseURL: "http://linux.example", WebDist: filepath.Join(t.TempDir(), "missing"),
		Sessions: sessions, Proxy: wsProxy,
	})
	if err != nil {
		t.Fatal(err)
	}
	linux := httptest.NewServer(linuxRouter)
	defer linux.Close()
	handler, err := newRemoteGatewayHandler(config.Config{
		RemoteBaseURL: linux.URL, WebDist: filepath.Join(t.TempDir(), "missing"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	windows := httptest.NewServer(handler)
	defer windows.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	login, err := http.NewRequest(http.MethodPost, windows.URL+"/api/v1/auth/login", strings.NewReader(`{"username":"operator","password":"long-private-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Header.Set("Origin", windows.URL)
	login.Header.Set("Content-Type", "application/json")
	response, err := client.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("remote login status: %d", response.StatusCode)
	}
	me, err := client.Get(windows.URL + "/api/v1/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	me.Body.Close()
	if me.StatusCode != http.StatusOK {
		t.Fatalf("remote session status: %d", me.StatusCode)
	}
}
