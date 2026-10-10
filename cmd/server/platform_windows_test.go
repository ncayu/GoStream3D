//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gostream3d/internal/config"
)

func TestPrepareConfigStartsWithoutSetup(t *testing.T) {
	t.Setenv("GOSTREAM3D_RUNTIME", "")
	path := filepath.Join(t.TempDir(), "config.env")
	prepared, err := platformPrepareConfig(path, false)
	if err != nil || prepared != path {
		t.Fatalf("prepare default configuration: path=%q err=%v", prepared, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			values[key] = value
		}
	}
	for key, expected := range map[string]string{
		"GOSTREAM3D_RUNTIME":         "native",
		"GOSTREAM3D_HTTP_HOST":       "0.0.0.0",
		"GOSTREAM3D_HTTP_PORT":       "8888",
		"GOSTREAM3D_ADMIN_USERNAME":  "admin",
		"GOSTREAM3D_ADMIN_PASSWORD":  "Admin@2026.com",
		"GOSTREAM3D_CPU_MAX_PERCENT": "50",
	} {
		if values[key] != expected {
			t.Errorf("unexpected default %s", key)
		}
	}
	if len(values["GOSTREAM3D_ADMIN_PASSWORD"]) < 12 {
		t.Fatal("generated administrator password is missing or too short")
	}
	if _, err := platformPrepareConfig(path, false); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(data) {
		t.Fatal("subsequent startup changed the saved configuration")
	}
}

func TestPrepareConfigPreservesExistingCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	original := "GOSTREAM3D_ADMIN_USERNAME=operator\nGOSTREAM3D_ADMIN_PASSWORD=existing-password-123\nGOSTREAM3D_HTTP_PORT=9123\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := platformPrepareConfig(path, false); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultLocalConfig(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != original {
		t.Fatal("existing credentials or port were overwritten")
	}
}

func TestPrepareExplicitMissingConfigDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.env")
	prepared, err := platformPrepareConfig(path, true)
	if err != nil || prepared != path {
		t.Fatalf("explicit config path: %q, %v", prepared, err)
	}
	if err := config.LoadFile(prepared); err == nil {
		t.Fatal("explicitly missing configuration must report an error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("explicit configuration was unexpectedly created")
	}
}

func TestPrepareConfigKeepsEnvironmentOnlyDeployment(t *testing.T) {
	t.Setenv("GOSTREAM3D_RUNTIME", "native")
	path := filepath.Join(t.TempDir(), "config.env")
	prepared, err := platformPrepareConfig(path, false)
	if err != nil || prepared != "" {
		t.Fatalf("environment-only deployment: path=%q err=%v", prepared, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("environment-only deployment wrote a configuration file")
	}
}

func TestPrepareConfigRejectsDirectoryOrUnwritablePath(t *testing.T) {
	t.Setenv("GOSTREAM3D_RUNTIME", "")
	if _, err := platformPrepareConfig(t.TempDir(), false); err == nil {
		t.Fatal("configuration path cannot be a directory")
	}
	missingParent := filepath.Join(t.TempDir(), "missing", "config.env")
	if _, err := platformPrepareConfig(missingParent, false); err == nil {
		t.Fatal("configuration creation failure must be reported")
	}
}

func TestDefaultLocalConfigUsesRequestedInitialCredentials(t *testing.T) {
	first := filepath.Join(t.TempDir(), "config.env")
	second := filepath.Join(t.TempDir(), "config.env")
	for _, path := range []string{first, second} {
		if err := writeDefaultLocalConfig(path); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := os.ReadFile(first)
	b, _ := os.ReadFile(second)
	if string(a) != string(b) || !strings.Contains(string(a), "GOSTREAM3D_ADMIN_PASSWORD=Admin@2026.com\n") {
		t.Fatal("independent installations must receive the configured default password")
	}
}
