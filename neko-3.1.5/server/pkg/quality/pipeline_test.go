package quality

import (
	"strings"
	"testing"
)

func TestQualityBudgetFollowsResizedScreen(t *testing.T) {
	template := "bitrate={g3d_bitrate} max-bitrate={g3d_peak} vbv-buffer-size={g3d_vbv}"
	for _, tc := range []struct {
		w, h, fps int
		want      string
	}{
		{1920, 1080, 30, "bitrate=18000 max-bitrate=27000 vbv-buffer-size=1800"},
		{2560, 1440, 30, "bitrate=32000 max-bitrate=48000 vbv-buffer-size=3200"},
		{3840, 2160, 30, "bitrate=72000 max-bitrate=108000 vbv-buffer-size=7200"},
		{1920, 1080, 60, "bitrate=36000 max-bitrate=54000 vbv-buffer-size=1800"},
	} {
		if got := Apply(template, tc.w, tc.h, tc.fps); got != tc.want {
			t.Fatalf("%+v: %s", tc, got)
		}
	}
	custom := "vp8enc bitrate=6000"
	if Apply(custom, 3840, 2160, 30) != custom {
		t.Fatal("custom pipeline changed")
	}
	if strings.Contains(Apply(template, 0, 0, 0), "{g3d_") {
		t.Fatal("invalid screen left unresolved tokens")
	}
}
