package monitor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGPUCommandUsesConfiguredWSLBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nvidia-smi")
	if err := os.WriteFile(path, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSTREAM3D_NVIDIA_SMI", path)
	got, err := nvidiaSMIPath()
	if err != nil || got != path {
		t.Fatalf("got %q: %v", got, err)
	}
	t.Setenv("GOSTREAM3D_NVIDIA_SMI", path+"-missing")
	if _, err := nvidiaSMIPath(); err == nil {
		t.Fatal("missing configured binary accepted")
	}
}

func TestGPUParsePreservesUnavailableAndMultipleDevices(t *testing.T) {
	cards, err := parseGPUs("GPU-a, NVIDIA A, 12, 0, 512, 8192\nGPU-b, NVIDIA B, N/A, [Not Supported], N/A, 4096\n")
	if err != nil || len(cards) != 2 {
		t.Fatal(cards, err)
	}
	if *cards[0].MemoryUsed != 512*1024*1024 || cards[1].Utilization != nil || cards[1].Encoder != nil || cards[1].MemoryUsed != nil {
		t.Fatal(cards)
	}
	if _, err := parseGPUs("broken,record"); err == nil {
		t.Fatal("invalid CSV accepted")
	}
}
func TestGPURealSamplingBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cards, msg := SampleGPUs(ctx)
	t.Logf("devices=%+v unavailable=%s", cards, msg)
}
