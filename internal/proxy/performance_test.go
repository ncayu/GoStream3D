package proxy

import (
	"encoding/json"
	"gostream3d/internal/orchestrator"
	"math"
	"testing"
	"time"
)

func TestWebRTCPerformanceIsolatedAndExpires(t *testing.T) {
	p, _ := NewWebSocketProxy("secret")
	seed := orchestrator.StreamPerformance{Token: "worker", ProjectToken: "project", RenderMode: "nvidia", TargetFPS: 30}
	a := p.openPerformance(seed)
	b := p.openPerformance(seed)
	m := orchestrator.WebRTCPerformance{Width: 1920, Height: 1080, Codec: "video/H264", SampleSeconds: 3, DisplayFPS: floatPointer(29), BitrateMbps: floatPointer(6)}
	data, _ := json.Marshal(map[string]any{"type": "webrtc-performance", "metrics": m, "token": "other"})
	if !p.recordPerformance(a, data, time.Now()) {
		t.Fatal("valid feedback rejected")
	}
	all := p.PerformanceSnapshot()
	if len(all) != 2 {
		t.Fatal(all)
	}
	for _, s := range all {
		if s.Token != "worker" || s.ProjectToken != "project" {
			t.Fatal("client-controlled identity", s)
		}
		if s.ViewerID == a && (s.WebRTC == nil || *s.WebRTC.DisplayFPS != 29) {
			t.Fatal(s)
		}
		if s.ViewerID == b && s.WebRTC != nil {
			t.Fatal("mixed viewers", s)
		}
	}
	p.performanceMu.Lock()
	s := p.performance[a]
	s.FeedbackAt = time.Now().Add(-11 * time.Second)
	p.performance[a] = s
	p.performanceMu.Unlock()
	for _, s := range p.PerformanceSnapshot() {
		if s.ViewerID == a && s.WebRTC != nil {
			t.Fatal("stale measurement shown as current")
		}
	}
	p.closePerformance(a)
	p.closePerformance(b)
	if len(p.PerformanceSnapshot()) != 0 {
		t.Fatal("disconnected connections leaked")
	}
}

func floatPointer(n float64) *float64 { return &n }
func TestWebRTCPerformanceRejectsInvalidAndPreservesZero(t *testing.T) {
	for _, n := range []float64{-1, 10001, math.Inf(1), math.NaN()} {
		m := orchestrator.WebRTCPerformance{Width: 1920, Height: 1080, SampleSeconds: 3, DisplayFPS: floatPointer(n)}
		if validWebRTCPerformance(m) {
			t.Fatalf("invalid measurement accepted: %v", n)
		}
	}
	if !validWebRTCPerformance(orchestrator.WebRTCPerformance{Width: 1920, Height: 1080, SampleSeconds: 3, DisplayFPS: floatPointer(0), RTTMS: floatPointer(0)}) {
		t.Fatal("real zero must be valid")
	}
	p, _ := NewWebSocketProxy("secret")
	id := p.openPerformance(orchestrator.StreamPerformance{Token: "worker"})
	for _, input := range []string{`{"type":"webrtc-performance","metrics":{"width":999999,"height":1080,"sample_seconds":3}}`, `{"type":"webrtc-performance","metrics":{"width":1920,"height":1080,"sample_seconds":0}}`, `{"type":"webrtc-performance","metrics":null}`} {
		if p.recordPerformance(id, []byte(input), time.Now()) {
			t.Fatal("bad payload accepted")
		}
	}
}
