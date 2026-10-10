//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"

	"gostream3d/internal/config"
)

func TestLinuxDataPathsFollowConfigDirectory(t *testing.T) {
	before := linuxConfigDirectory
	t.Cleanup(func() { linuxConfigDirectory = before })
	base := t.TempDir()
	path := filepath.Join(base, "config.env")
	if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := platformPrepareConfig(path, true); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{SessionStore: "data/sessions.json", WebDist: "web/dist"}
	cfg.Native.WorkRoot = "data/workers"
	if err := platformResolveDataPaths(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.SessionStore != filepath.Join(base, "data/sessions.json") ||
		cfg.Native.WorkRoot != filepath.Join(base, "data/workers") ||
		cfg.Native.WebDist != filepath.Join(base, "web/dist") {
		t.Fatalf("paths drifted from configuration directory: %+v", cfg)
	}
}

func TestLinuxEnvironmentOnlyKeepsRelativePaths(t *testing.T) {
	before := linuxConfigDirectory
	t.Cleanup(func() { linuxConfigDirectory = before })
	linuxConfigDirectory = ""
	t.Setenv("GOSTREAM3D_RUNTIME", "docker")
	path := filepath.Join(t.TempDir(), "config.env")
	if prepared, err := platformPrepareConfig(path, false); err != nil || prepared != "" {
		t.Fatalf("environment-only prepare: %q, %v", prepared, err)
	}
	cfg := config.Config{SessionStore: "data/sessions.json"}
	if err := platformResolveDataPaths(&cfg); err != nil || cfg.SessionStore != "data/sessions.json" {
		t.Fatal("existing environment-only data location changed")
	}
}
