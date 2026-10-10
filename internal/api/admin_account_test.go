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

	"github.com/gin-gonic/gin"
	"gostream3d/internal/orchestrator"
	"gostream3d/internal/proxy"
)

func accountTestRouter(t *testing.T, path string) *gin.Engine {
	t.Helper()
	sessions, err := orchestrator.NewSessionManager(stubWorkers{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := proxy.NewWebSocketProxy("internal")
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(RouterConfig{AdminUsername: "admin", AdminPassword: "initial-private-password", AdminAPIKey: "legacy-key", AdminCredentialsPath: path, PublicBaseURL: "http://gateway.example", Sessions: sessions, Proxy: ws})
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func accountRequest(router http.Handler, method, path, body string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://gateway.example"+path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func accountLogin(t *testing.T, router http.Handler, username, password string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	w := accountRequest(router, "POST", "/api/v1/auth/login", string(body), nil, "")
	if w.Code != http.StatusOK || len(w.Result().Cookies()) != 1 {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}

func TestAdminAccountRotationAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin-account.json")
	router := accountTestRouter(t, path)
	cookie := accountLogin(t, router, "admin", "initial-private-password")
	otherCookie := accountLogin(t, router, "admin", "initial-private-password")
	body := `{"username":"operator","current_password":"initial-private-password","new_password":"updated-private-password"}`
	if w := accountRequest(router, "PUT", "/api/v1/auth/account", body, nil, ""); w.Code != 401 {
		t.Fatalf("anonymous update: %d", w.Code)
	}
	if w := accountRequest(router, "PUT", "/api/v1/auth/account", body, cookie, "http://other.example"); w.Code != 403 {
		t.Fatalf("cross-origin update: %d", w.Code)
	}
	for _, invalid := range []string{`{}`, `{"username":"bad name","current_password":"initial-private-password","new_password":"updated-private-password"}`, `{"username":"operator","current_password":"initial-private-password","new_password":"short"}`, body + `{}`, `{"username":"operator","current_password":"initial-private-password","new_password":"updated-private-password","extra":1}`} {
		if w := accountRequest(router, "PUT", "/api/v1/auth/account", invalid, cookie, ""); w.Code != 400 {
			t.Fatalf("invalid update accepted: %d", w.Code)
		}
	}
	wrong := `{"username":"operator","current_password":"wrong-password","new_password":"updated-private-password"}`
	if w := accountRequest(router, "PUT", "/api/v1/auth/account", wrong, cookie, ""); w.Code != 403 {
		t.Fatalf("wrong current password: %d", w.Code)
	}
	if w := accountRequest(router, "PUT", "/api/v1/auth/account", body, cookie, ""); w.Code != 200 {
		t.Fatalf("update failed: %d %s", w.Code, w.Body.String())
	}
	for _, oldCookie := range []*http.Cookie{cookie, otherCookie} {
		if w := accountRequest(router, "GET", "/api/v1/auth/me", "", oldCookie, ""); w.Code != 401 {
			t.Fatalf("old session remains valid: %d", w.Code)
		}
	}
	if w := accountRequest(router, "PUT", "/api/v1/auth/account", body, cookie, ""); w.Code != 401 {
		t.Fatal("revoked session changed account")
	}
	data, _ := os.ReadFile(path)
	if bytes.Contains(data, []byte("updated-private-password")) || bytes.Contains(data, []byte("initial-private-password")) {
		t.Fatal("plaintext credentials stored")
	}
	for _, server := range []http.Handler{router, accountTestRouter(t, path)} {
		oldLogin := `{"username":"admin","password":"initial-private-password"}`
		if w := accountRequest(server, "POST", "/api/v1/auth/login", oldLogin, nil, ""); w.Code != 401 {
			t.Fatal("initial credentials restored")
		}
		newCookie := accountLogin(t, server, "operator", "updated-private-password")
		me := accountRequest(server, "GET", "/api/v1/auth/me", "", newCookie, "")
		if !bytes.Contains(me.Body.Bytes(), []byte(`"username":"operator"`)) {
			t.Fatalf("stale username: %s", me.Body.String())
		}
		r := httptest.NewRequest("GET", "http://gateway.example/api/v1/auth/me", nil)
		r.Header.Set("Authorization", "Bearer legacy-key")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("legacy API key bypasses rotated credentials")
		}
	}
}

func TestAdminAccountSaveFailurePreservesLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin-account.json")
	router := accountTestRouter(t, path)
	cookie := accountLogin(t, router, "admin", "initial-private-password")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	body := `{"username":"operator","current_password":"initial-private-password","new_password":"updated-private-password"}`
	if w := accountRequest(router, "PUT", "/api/v1/auth/account", body, cookie, ""); w.Code != 500 {
		t.Fatalf("failed persistence claimed success: %d", w.Code)
	}
	accountLogin(t, router, "admin", "initial-private-password")
	if w := accountRequest(router, "GET", "/api/v1/auth/me", "", cookie, ""); w.Code != 200 {
		t.Fatal("failed write revoked original session")
	}
}

func TestAdminAccountCurrentPasswordRateLimit(t *testing.T) {
	router := accountTestRouter(t, filepath.Join(t.TempDir(), "admin-account.json"))
	cookie := accountLogin(t, router, "admin", "initial-private-password")
	body := `{"username":"admin","current_password":"wrong","new_password":"updated-private-password"}`
	for i := 0; i < 5; i++ {
		if w := accountRequest(router, "PUT", "/api/v1/auth/account", body, cookie, ""); w.Code != 403 {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	if w := accountRequest(router, "PUT", "/api/v1/auth/account", body, cookie, ""); w.Code != 429 {
		t.Fatal("current password attempts are unlimited")
	}
}
