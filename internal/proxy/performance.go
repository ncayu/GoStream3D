package proxy

import (
	"context"
	"encoding/json"
	"gostream3d/internal/orchestrator"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"
)

type performanceKey struct{}

// The route binds metadata to the worker acquired for this connection. Never
// accept project/worker identity from a viewer's untrusted feedback payload.
func (p *WebSocketProxy) ServeWorker(w http.ResponseWriter, r *http.Request, worker orchestrator.Worker, info orchestrator.SessionInfo) error {
	seed := orchestrator.StreamPerformance{Token: worker.Token, ProjectToken: info.Token, ProjectName: info.Name, RenderMode: info.RenderMode, Policy: info.FramePolicy, TargetFPS: float64(info.FPS)}
	return p.Serve(w, r.WithContext(context.WithValue(r.Context(), performanceKey{}, seed)), worker.SignalURL)
}

func (p *WebSocketProxy) openPerformance(seed orchestrator.StreamPerformance) string {
	p.performanceMu.Lock()
	defer p.performanceMu.Unlock()
	if p.performance == nil {
		p.performance = make(map[string]orchestrator.StreamPerformance)
	}
	p.performanceSequence++
	id := strconv.FormatUint(p.performanceSequence, 10)
	seed.Transport = "webrtc"
	seed.ViewerID = id
	seed.Watching = true
	p.performance[id] = seed
	return id
}
func (p *WebSocketProxy) closePerformance(id string) {
	p.performanceMu.Lock()
	defer p.performanceMu.Unlock()
	delete(p.performance, id)
}

func validWebRTCPerformance(m orchestrator.WebRTCPerformance) bool {
	if m.Width < 1 || m.Width > 8192 || m.Height < 1 || m.Height > 8192 || math.IsNaN(m.SampleSeconds) || math.IsInf(m.SampleSeconds, 0) || m.SampleSeconds < .25 || m.SampleSeconds > 30 {
		return false
	}
	switch m.Codec {
	case "", "video/H264", "video/VP8", "video/VP9", "video/AV1":
	default:
		return false
	}
	for _, field := range []struct {
		value *float64
		max   float64
	}{{m.ReceiveFPS, 240}, {m.DisplayFPS, 240}, {m.BitrateMbps, 1000}, {m.DecodeMS, 10000}, {m.BufferMS, 10000}, {m.RTTMS, 10000}, {m.PacketsLost, 1e9}, {m.FramesDropped, 1e9}} {
		if field.value != nil && (math.IsNaN(*field.value) || math.IsInf(*field.value, 0) || *field.value < 0 || *field.value > field.max) {
			return false
		}
	}
	for _, v := range []*float64{m.PacketsLost, m.FramesDropped} {
		if v != nil && math.Trunc(*v) != *v {
			return false
		}
	}
	return true
}

func (p *WebSocketProxy) recordPerformance(id string, payload []byte, now time.Time) bool {
	if len(payload) > 4096 || id == "" {
		return false
	}
	var report struct {
		Metrics *orchestrator.WebRTCPerformance `json:"metrics"`
	}
	if json.Unmarshal(payload, &report) != nil || report.Metrics == nil || !validWebRTCPerformance(*report.Metrics) {
		return false
	}
	p.performanceMu.Lock()
	defer p.performanceMu.Unlock()
	s, ok := p.performance[id]
	if !ok || (!s.FeedbackAt.IsZero() && now.Sub(s.FeedbackAt) < 500*time.Millisecond) {
		return false
	}
	s.WebRTC = report.Metrics
	s.FeedbackAt = now
	p.performance[id] = s
	return true
}

func (p *WebSocketProxy) PerformanceSnapshot() []orchestrator.StreamPerformance {
	p.performanceMu.Lock()
	defer p.performanceMu.Unlock()
	result := make([]orchestrator.StreamPerformance, 0, len(p.performance))
	for _, s := range p.performance {
		if time.Since(s.FeedbackAt) > 10*time.Second {
			s.WebRTC = nil
		}
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Token == result[j].Token {
			return result[i].ViewerID < result[j].ViewerID
		}
		return result[i].Token < result[j].Token
	})
	return result
}
