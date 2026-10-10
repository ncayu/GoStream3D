// Package quality resolves GoStream3D's optional bitrate tokens each time a
// capture pipeline is created, including after an adaptive desktop resize.
package quality

import (
	"math"
	"strconv"
	"strings"
)

func Apply(pipeline string, width, height, fps int) string {
	if !strings.Contains(pipeline, "{g3d_") {
		return pipeline
	}
	if width < 1 || height < 1 || fps < 1 {
		width, height, fps = 1920, 1080, 30
	}
	// 18 Mbps at 1080p30; preserve the same per-pixel budget on larger screens.
	rate := int(math.Round(18000*float64(width)*float64(height)*float64(fps)/(1920*1080*30)/1000)) * 1000
	if rate < 6000 {
		rate = 6000
	}
	if rate > 144000 {
		rate = 144000
	}
	peak := rate * 3 / 2
	return strings.NewReplacer("{g3d_bitrate}", strconv.Itoa(rate), "{g3d_peak}", strconv.Itoa(peak), "{g3d_vbv}", strconv.Itoa(peak*2/fps)).Replace(pipeline)
}
