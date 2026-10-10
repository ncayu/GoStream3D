package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVideoPipelinesKeepOneLocalCursorForLegacyViewers(t *testing.T) {
	for _, pipeline := range []string{cpuVideoPipeline(":99.0", "1920x1080@30"), gpuVideoPipeline(":99.0", "1920x1080@30", "nvidia")} {
		env := videoPipelineEnvironment(pipeline)
		var config map[string]struct {
			Pipeline string `json:"gst_pipeline"`
		}
		found := false
		for _, entry := range env {
			if strings.HasPrefix(entry, "NEKO_CAPTURE_VIDEO_PIPELINES=") {
				if err := json.Unmarshal([]byte(strings.TrimPrefix(entry, "NEKO_CAPTURE_VIDEO_PIPELINES=")), &config); err != nil {
					t.Fatal(err)
				}
				found = true
			}
		}
		if !found || len(config) != 2 {
			t.Fatal("explicit legacy pipeline is required to prevent Neko from enabling its remote cursor", env)
		}
		for _, id := range []string{"main", "legacy"} {
			if config[id].Pipeline != pipeline || !strings.Contains(config[id].Pipeline, "show-pointer=false") {
				t.Fatal(id, config)
			}
		}
	}
}

func TestNvidiaEncodingBoundsLatencyWithoutScaling(t *testing.T) {
	for _, screen := range []string{"1920x1080@30", "1920x1080@60"} {
		pipeline := gpuVideoPipeline(":99.0", screen, "nvidia")
		for _, want := range []string{"preset=p4", "tune=ultra-low-latency", "rc-mode=vbr", "const-quality=20", "spatial-aq=true", "bitrate={g3d_bitrate}", "max-bitrate={g3d_peak}", "rc-lookahead=0", "bframes=0", "zerolatency=true", "vbv-buffer-size={g3d_vbv}", "max-buffers=1", "sync=false"} {
			if !strings.Contains(pipeline, want) {
				t.Fatalf("missing %s in %s", want, pipeline)
			}
		}
		if strings.Contains(pipeline, "videoscale") {
			t.Fatal("resolution must be preserved")
		}
	}
}
