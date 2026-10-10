package orchestrator

import (
	"fmt"
	"strconv"
	"strings"
)

// Keep capture dimensions dynamic so Neko can rebuild this pipeline on resize.
// Drop stale raw frames before conversion and encoding to bound input latency.
func cpuVideoPipeline(display, screen string) string {
	fps := strings.Split(screen, "@")[1]
	rate, _ := strconv.Atoi(fps)
	return fmt.Sprintf("ximagesrc display-name=%s show-pointer=false use-damage=false ! video/x-raw,framerate=%s/1 ! queue leaky=downstream max-size-buffers=1 max-size-bytes=0 max-size-time=0 ! videoconvert ! video/x-raw,format=I420 ! vp8enc threads=2 cpu-used=8 deadline=1 end-usage=cbr target-bitrate=2500000 keyframe-max-dist=%d min-quantizer=4 max-quantizer=40 lag-in-frames=0 static-threshold=100 ! appsink name=appsink", display, fps, rate*2)
}
