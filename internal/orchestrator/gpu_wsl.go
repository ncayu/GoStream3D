package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

// Inspect the daemon host, not the gateway's container filesystem. WSL exposes
// graphics through DXG/D3D12; CUDA availability alone does not provide Vulkan.
func nvidiaGraphicsEnvironment(hostRoot string) ([]string, bool, error) {
	if hostRoot == "" {
		hostRoot = "/"
	}
	kernel, err := os.ReadFile(filepath.Join(hostRoot, "proc/sys/kernel/osrelease"))
	if err != nil || !strings.Contains(strings.ToLower(string(kernel)), "microsoft") {
		return nil, false, nil
	}
	for _, path := range []string{"dev/dxg", "usr/lib/wsl/lib/libd3d12.so", "usr/lib/wsl/lib/libd3d12core.so", "usr/lib/wsl/lib/libdxcore.so", "usr/lib/wsl/drivers"} {
		info, err := os.Stat(filepath.Join(hostRoot, path))
		if err != nil || (path == "usr/lib/wsl/drivers" && !info.IsDir()) {
			return nil, true, fmt.Errorf("WSL NVIDIA graphics requires /%s on the Docker host; update WSL and the Windows NVIDIA driver", path)
		}
	}
	return []string{"GOSTREAM3D_GPU_GRAPHICS_API=opengl", "GALLIUM_DRIVER=d3d12", "MESA_D3D12_DEFAULT_ADAPTER_NAME=NVIDIA", "LD_LIBRARY_PATH=/usr/lib/wsl/lib"}, true, nil
}

func configureNvidiaGPU(hostRoot string, config *container.HostConfig) ([]string, error) {
	env, wsl, err := nvidiaGraphicsEnvironment(hostRoot)
	if err != nil {
		return nil, err
	}
	count := 1
	if wsl {
		count = -1 // NVIDIA Container Toolkit on WSL supports --gpus all.
		config.Devices = append(config.Devices, container.DeviceMapping{PathOnHost: "/dev/dxg", PathInContainer: "/dev/dxg", CgroupPermissions: "rwm"})
		// Source paths are resolved by Docker on the host, never under /host/root.
		config.Mounts = append(config.Mounts, mount.Mount{Type: mount.TypeBind, Source: "/usr/lib/wsl", Target: "/usr/lib/wsl", ReadOnly: true})
	}
	config.DeviceRequests = []container.DeviceRequest{{Driver: "nvidia", Count: count, Capabilities: [][]string{{"gpu"}}}}
	return append(env, "NVIDIA_DRIVER_CAPABILITIES=graphics,video,compute,utility,display", "NVIDIA_VISIBLE_DEVICES=all"), nil
}
