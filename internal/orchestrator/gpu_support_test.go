package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGPUBackendUsesHostIntelDevice(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "sys/class/drm/renderD128/device")
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "vendor"), []byte("0x8086\n"), 0644); err != nil {
		t.Fatal(err)
	}
	backend, device, err := resolveGPUBackend("auto", root, "", false)
	if err != nil || backend != "intel" || device != "/dev/dri/renderD128" {
		t.Fatalf("%s %s %v", backend, device, err)
	}
	if _, _, err := resolveGPUBackend("auto", t.TempDir(), "", false); err == nil {
		t.Fatal("GPU mode must not silently fall back to CPU")
	}
	if _, _, err := resolveGPUBackend("nvidia", root, "", false); err == nil {
		t.Fatal("NVIDIA requires its container runtime")
	}
	if backend, _, err := resolveGPUBackend("nvidia", root, "", true); err != nil || backend != "nvidia" {
		t.Fatal(backend, err)
	}
}

func TestGPUHardwarePipelineRetainsResolutionAndUsesH264(t *testing.T) {
	p := gpuVideoPipeline(":99.0", "1920x1080@60", "intel")
	for _, want := range []string{"framerate=60/1", "queue leaky=downstream max-size-buffers=1", "vah264lpenc", "rate-control=cqp", "qpi=22", "qpp=22", "key-int-max=120", "profile=constrained-baseline", "appsink name=appsink"} {
		if !strings.Contains(p, want) {
			t.Fatal("missing", want, p)
		}
	}
	if strings.Contains(p, "videoscale") || strings.Contains(p, "vp8enc") {
		t.Fatal("GPU mode must keep dimensions and use hardware encoding")
	}
	if p := gpuVideoPipeline(":99.0", "1920x1080@30", "nvidia"); !strings.Contains(p, "nvh264enc") {
		t.Fatal(p)
	}
	for _, renderer := range []string{"Google SwiftShader", "llvmpipe", "softpipe", ""} {
		if hardwareRenderer(renderer) {
			t.Fatal("software renderer accepted", renderer)
		}
	}
	if !hardwareRenderer("ANGLE (Intel, Vulkan Intel Iris Xe)") {
		t.Fatal("Intel renderer rejected")
	}
}
