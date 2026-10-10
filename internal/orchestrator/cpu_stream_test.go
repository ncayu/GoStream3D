package orchestrator

import (
	"strings"
	"testing"
)

func TestCPUVideoPipelineUsesBoundedRawQueueAndRequestedRate(t *testing.T) {
	for _, rate := range []string{"30", "60"} {
		got := cpuVideoPipeline(":101", "1280x720@"+rate)
		for _, expected := range []string{"display-name=:101", "framerate=" + rate + "/1", "queue leaky=downstream max-size-buffers=1", "vp8enc threads=2 cpu-used=8", "deadline=1", "lag-in-frames=0", "appsink name=appsink"} {
			if !strings.Contains(got, expected) {
				t.Fatalf("missing %s in %s", expected, got)
			}
		}
		if strings.Index(got, "queue ") > strings.Index(got, "videoconvert") {
			t.Fatal("raw frames must be dropped before conversion")
		}
	}
}
