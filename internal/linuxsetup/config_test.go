package linuxsetup

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPreparePreservesCredentialsAndCreatesDistinctWorkerSecrets(t *testing.T) {
	t.Setenv("GOSTREAM3D_RUNTIME", "")
	path := filepath.Join(t.TempDir(), "config.env")
	if _, err := Prepare(path, false); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	secrets := map[string]bool{}
	for _, line := range strings.Split(string(original), "\n") {
		key, value, _ := strings.Cut(line, "=")
		if key == "GOSTREAM3D_ADMIN_PASSWORD" {
			if value != "Admin@2026.com" {
				t.Fatal("unexpected default administrator password")
			}
			continue
		}
		if strings.HasSuffix(key, "PASSWORD") {
			if len(value) != 48 || secrets[value] {
				t.Fatal("missing, weak, or repeated credential")
			}
			secrets[value] = true
		}
	}
	if len(secrets) != 2 || !strings.Contains(string(original), "GOSTREAM3D_ADMIN_PASSWORD=Admin@2026.com\n") || strings.Contains(string(original), "CPU_MAX_PERCENT") {
		t.Fatal("Linux configuration missing secrets or contains a Windows CPU cap")
	}
	if _, err := Prepare(path, false); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Fatal("restart overwrote existing credentials")
	}
}

func TestPrepareExplicitAndEnvironmentOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	if _, err := Prepare(path, true); err == nil {
		t.Fatal("explicit missing config must fail, including config.env")
	}
	t.Setenv("GOSTREAM3D_RUNTIME", "docker")
	if prepared, err := Prepare(path, false); prepared != "" || err != nil {
		t.Fatalf("environment-only deployment changed: %q %v", prepared, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("environment-only deployment created a file")
	}
}

func TestDefaultConfigKeepsExistingWorkingDirectory(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(t.TempDir(), "gostream3d")
	if got := DefaultConfig(executable, directory); got != filepath.Join(filepath.Dir(executable), "config.env") {
		t.Fatal(got)
	}
	local := filepath.Join(directory, "config.env")
	if err := os.WriteFile(local, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := DefaultConfig(executable, directory); got != local {
		t.Fatal("existing working-directory configuration was ignored")
	}
}

func TestCreateDockerConfigPreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	if err := Create(path, "docker", "192.168.10.20"); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	if !strings.Contains(string(original), "GOSTREAM3D_RUNTIME=docker\n") ||
		!strings.Contains(string(original), "GOSTREAM3D_ICE_PUBLIC_IP=192.168.10.20\n") {
		t.Fatal("installer options were not applied")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatal("configuration is readable by other users")
		}
	}
	if err := Create(path, "docker", "203.0.113.42"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Fatal("installer overwrote existing credentials or network settings")
	}
}

func TestCreateRejectsInvalidOptionsWithoutWriting(t *testing.T) {
	for _, options := range [][2]string{{"remote", "127.0.0.1"}, {"docker", "https://server:8888"}} {
		path := filepath.Join(t.TempDir(), "config.env")
		if err := Create(path, options[0], options[1]); err == nil {
			t.Fatal("invalid installer option accepted")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("invalid setup wrote a configuration")
		}
	}
}
