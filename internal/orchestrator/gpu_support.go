package orchestrator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var renderDevicePattern = regexp.MustCompile(`^/dev/dri/renderD[0-9]+$`)

// The gateway can inspect its read-only host root without granting GPU access
// to the gateway itself. Only the selected worker receives the render device.
func resolveGPUBackend(requested, hostRoot, device string, nvidiaRuntime bool) (string, string, error) {
	if requested == "" {
		requested = "auto"
	}
	if requested != "auto" && requested != "intel" && requested != "nvidia" {
		return "", "", errors.New("GPU backend must be auto, intel or nvidia")
	}
	if hostRoot == "" {
		hostRoot = "/"
	}
	if device != "" && !renderDevicePattern.MatchString(device) {
		return "", "", errors.New("GPU render device must be /dev/dri/renderD<number>")
	}
	if requested == "nvidia" {
		if !nvidiaRuntime {
			return "", "", errors.New("NVIDIA GPU requires NVIDIA driver and Container Toolkit runtime")
		}
		return "nvidia", "", nil
	}
	devices, _ := filepath.Glob(filepath.Join(hostRoot, "sys/class/drm/renderD*"))
	for _, path := range devices {
		node := "/dev/dri/" + filepath.Base(path)
		if device != "" && device != node {
			continue
		}
		vendor, err := os.ReadFile(filepath.Join(path, "device/vendor"))
		if err == nil && strings.TrimSpace(string(vendor)) == "0x8086" {
			return "intel", node, nil
		}
	}
	if requested == "auto" && nvidiaRuntime {
		return "nvidia", "", nil
	}
	return "", "", errors.New("no supported GPU found: Intel requires i915/xe render device; NVIDIA requires driver and Container Toolkit")
}

func gpuVideoPipeline(display, screen, backend string) string {
	fps, _ := strconv.Atoi(strings.Split(screen, "@")[1])
	encoder := fmt.Sprintf("vah264lpenc rate-control=cqp qpi=22 qpp=22 b-frames=0 key-int-max=%d target-usage=4", fps*2)
	if backend == "nvidia" {
		// Neko resolves the quality budget using the actual screen on every
		// pipeline creation, including adaptive resizes. Keep low-delay ordering.
		encoder = fmt.Sprintf("nvh264enc preset=p4 tune=ultra-low-latency rc-mode=vbr bitrate={g3d_bitrate} max-bitrate={g3d_peak} const-quality=20 spatial-aq=true aq-strength=6 vbv-buffer-size={g3d_vbv} gop-size=%d rc-lookahead=0 bframes=0 zerolatency=true", fps)
	}
	return fmt.Sprintf("ximagesrc display-name=%s show-pointer=false use-damage=false ! video/x-raw,framerate=%d/1 ! queue leaky=downstream max-size-buffers=1 max-size-bytes=0 max-size-time=0 ! videoconvert ! video/x-raw,format=NV12 ! %s ! h264parse config-interval=-1 ! video/x-h264,stream-format=byte-stream,profile=constrained-baseline ! appsink name=appsink max-buffers=1 sync=false enable-last-sample=false", display, fps, encoder)
}

func hardwareRenderer(renderer string) bool {
	name := strings.ToLower(renderer)
	if name == "" {
		return false
	}
	for _, software := range []string{"swiftshader", "llvmpipe", "softpipe", "software", "lavapipe"} {
		if strings.Contains(name, software) {
			return false
		}
	}
	return strings.Contains(name, "intel") || strings.Contains(name, "nvidia")
}
