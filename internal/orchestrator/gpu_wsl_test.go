package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
)

func wslGPUHost(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, path := range []string{"proc/sys/kernel/osrelease", "dev/dxg", "usr/lib/wsl/lib/libd3d12.so", "usr/lib/wsl/lib/libd3d12core.so", "usr/lib/wsl/lib/libdxcore.so"} {
		name := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("6.18-microsoft-standard-WSL2"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "usr/lib/wsl/drivers"), 0755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestNvidiaWSLUsesHardwareBridge(t *testing.T) {
	host := wslGPUHost(t)
	config := &container.HostConfig{}
	env, err := configureNvidiaGPU(host, config)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"GOSTREAM3D_GPU_GRAPHICS_API=opengl", "GALLIUM_DRIVER=d3d12", "MESA_D3D12_DEFAULT_ADAPTER_NAME=NVIDIA", "LD_LIBRARY_PATH=/usr/lib/wsl/lib"} {
		if !strings.Contains(strings.Join(env, "\n"), want) {
			t.Fatalf("missing %s: %v", want, env)
		}
	}
	if len(config.DeviceRequests) != 1 || config.DeviceRequests[0].Count != -1 {
		t.Fatal("WSL requires the all-GPU request", config.DeviceRequests)
	}
	if len(config.Devices) != 1 || config.Devices[0].PathOnHost != "/dev/dxg" || config.Devices[0].PathInContainer != "/dev/dxg" {
		t.Fatal(config.Devices)
	}
	if len(config.Mounts) != 1 || config.Mounts[0].Source != "/usr/lib/wsl" || config.Mounts[0].Target != "/usr/lib/wsl" || !config.Mounts[0].ReadOnly {
		t.Fatal(config.Mounts)
	}
	if config.Privileged {
		t.Fatal("GPU bridge must not require privileged containers")
	}
}

func TestNvidiaNativeLinuxKeepsExistingRuntime(t *testing.T) {
	config := &container.HostConfig{}
	env, err := configureNvidiaGPU(t.TempDir(), config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(env, "\n"), "d3d12") || len(config.Mounts) != 0 || len(config.Devices) != 0 || config.DeviceRequests[0].Count != 1 {
		t.Fatal(env, config)
	}
}

func TestNvidiaWSLMissingBridgeFailsBeforeLaunch(t *testing.T) {
	for _, path := range []string{"dev/dxg", "usr/lib/wsl/lib/libd3d12.so", "usr/lib/wsl/lib/libd3d12core.so", "usr/lib/wsl/lib/libdxcore.so", "usr/lib/wsl/drivers"} {
		t.Run(path, func(t *testing.T) {
			root := wslGPUHost(t)
			if err := os.Remove(filepath.Join(root, path)); err != nil {
				t.Fatal(err)
			}
			if _, err := configureNvidiaGPU(root, &container.HostConfig{}); err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("missing bridge must fail with actionable path: %v", err)
			}
		})
	}
}
