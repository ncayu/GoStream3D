package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadFileAndEnvironmentOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(path, []byte("\ufeff# comment\r\nGOSTREAM3D_HTTP_ADDR=:9000\r\nGOSTREAM3D_ADMIN_API_KEY='file secret'\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSTREAM3D_HTTP_ADDR", ":9999")
	if err := LoadFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("GOSTREAM3D_HTTP_ADDR"); got != ":9999" {
		t.Fatalf("environment override lost: %q", got)
	}
	if got := os.Getenv("GOSTREAM3D_ADMIN_API_KEY"); got != "file secret" {
		t.Fatalf("quoted value: %q", got)
	}
}

func TestHTTPPortDefaultsAndOverridesLegacyAddress(t *testing.T) {
	t.Setenv("GOSTREAM3D_HTTP_ADDR", "")
	t.Setenv("GOSTREAM3D_HTTP_HOST", "")
	t.Setenv("GOSTREAM3D_HTTP_PORT", "")
	addr, base, err := httpEndpoints()
	if err != nil || addr != ":8888" || base != "http://localhost:8888" {
		t.Fatalf("default endpoint: addr=%q base=%q err=%v", addr, base, err)
	}
	t.Setenv("GOSTREAM3D_HTTP_ADDR", "127.0.0.1:8081")
	t.Setenv("GOSTREAM3D_HTTP_PORT", "9000")
	addr, base, err = httpEndpoints()
	if err != nil || addr != "127.0.0.1:9000" || base != "http://127.0.0.1:9000" {
		t.Fatalf("port override: addr=%q base=%q err=%v", addr, base, err)
	}
	t.Setenv("GOSTREAM3D_HTTP_HOST", "0.0.0.0")
	addr, base, err = httpEndpoints()
	if err != nil || addr != "0.0.0.0:9000" || base != "http://localhost:9000" {
		t.Fatalf("host override: addr=%q base=%q err=%v", addr, base, err)
	}
	t.Setenv("GOSTREAM3D_HTTP_PORT", "70000")
	if _, _, err := httpEndpoints(); err == nil || !strings.Contains(err.Error(), "GOSTREAM3D_HTTP_PORT") {
		t.Fatalf("expected invalid port error, got %v", err)
	}
}

func TestLoadDerivesPublicURLFromConfiguredPort(t *testing.T) {
	t.Setenv("GOSTREAM3D_RUNTIME", "native")
	t.Setenv("GOSTREAM3D_ADMIN_API_KEY", "test-secret")
	t.Setenv("GOSTREAM3D_HTTP_ADDR", "")
	t.Setenv("GOSTREAM3D_HTTP_HOST", "127.0.0.1")
	t.Setenv("GOSTREAM3D_HTTP_PORT", "9123")
	t.Setenv("GOSTREAM3D_PUBLIC_BASE_URL", "")
	cfg, err := Load()
	if err != nil || cfg.HTTPAddr != "127.0.0.1:9123" || cfg.PublicBaseURL != "http://127.0.0.1:9123" {
		t.Fatalf("configured endpoint: addr=%q base=%q err=%v", cfg.HTTPAddr, cfg.PublicBaseURL, err)
	}
}

func TestLoadCPUEffects(t *testing.T) {
	t.Setenv("GOSTREAM3D_RUNTIME", "native")
	t.Setenv("GOSTREAM3D_ADMIN_API_KEY", "test-secret")
	for _, profile := range []string{"", "original", "simplified"} {
		t.Setenv("GOSTREAM3D_CPU_EFFECTS", profile)
		cfg, err := Load()
		if profile == "simplified" && runtime.GOOS != "windows" {
			if err == nil || !strings.Contains(err.Error(), "requires Windows native mode") {
				t.Fatalf("expected unsupported platform error: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		expected := profile
		if expected == "" {
			expected = "original"
		}
		if cfg.Native.CPUEffects != expected {
			t.Fatalf("profile %q became %q", profile, cfg.Native.CPUEffects)
		}
	}
	t.Setenv("GOSTREAM3D_CPU_EFFECTS", "invalid")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOSTREAM3D_CPU_EFFECTS") {
		t.Fatalf("expected invalid profile error: %v", err)
	}
}

func TestLoadCPURenderer(t *testing.T) {
	t.Setenv("GOSTREAM3D_RUNTIME", "native")
	t.Setenv("GOSTREAM3D_ADMIN_API_KEY", "test-secret")
	for _, renderer := range []string{"", "swiftshader", "warp"} {
		t.Setenv("GOSTREAM3D_CPU_RENDERER", renderer)
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		expected := renderer
		if expected == "" {
			expected = "swiftshader"
		}
		if cfg.Native.CPURenderer != expected {
			t.Fatalf("renderer %q became %q", renderer, cfg.Native.CPURenderer)
		}
	}
	t.Setenv("GOSTREAM3D_CPU_RENDERER", "hardware")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOSTREAM3D_CPU_RENDERER") {
		t.Fatalf("expected invalid renderer error: %v", err)
	}
}

func TestLoadFileRejectsMalformedEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.env")
	if err := os.WriteFile(path, []byte("OTHER_KEY=123\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("expected line error, got %v", err)
	}
}

func TestRemoteRuntimeNeedsUpstreamButNoLocalAdminKey(t *testing.T) {
	t.Setenv("GOSTREAM3D_RUNTIME", "remote")
	t.Setenv("GOSTREAM3D_REMOTE_BASE_URL", "http://127.0.0.1:8080")
	t.Setenv("GOSTREAM3D_ADMIN_API_KEY", "")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSTREAM3D_REMOTE_BASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected missing remote upstream error")
	}
}

func TestNativeRuntimeAcceptsAccountPassword(t *testing.T) {
	t.Setenv("GOSTREAM3D_RUNTIME", "native")
	t.Setenv("GOSTREAM3D_ADMIN_API_KEY", "")
	t.Setenv("GOSTREAM3D_ADMIN_USERNAME", "operator")
	t.Setenv("GOSTREAM3D_ADMIN_PASSWORD", "long-private-password")
	cfg, err := Load()
	if err != nil || cfg.AdminUsername != "operator" {
		t.Fatalf("account configuration: %+v, %v", cfg, err)
	}
	t.Setenv("GOSTREAM3D_ADMIN_PASSWORD", "short")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "at least 12") {
		t.Fatalf("expected short password error, got %v", err)
	}
	t.Setenv("GOSTREAM3D_ADMIN_PASSWORD", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "configured together") {
		t.Fatalf("expected account pair error, got %v", err)
	}
}
