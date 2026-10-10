//go:build windows

package orchestrator

import (
	"context"
	"encoding/json"
	"math"
	"time"
)

type sentFrameSample struct {
	inputID uint64
	id      uint64
	at      time.Time
}

func smoothSample(previous, next float64) float64 {
	if previous == 0 {
		return next
	}
	return previous*0.8 + next*0.2
}

func (m *WindowsProcessManager) SetPerformanceSettingsSource(source func() PerformanceSettings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.performanceSource = source
}

func (m *WindowsProcessManager) PerformanceSnapshot() []StreamPerformance {
	m.mu.Lock()
	workers := make([]*windowsWorker, 0, len(m.active))
	for _, w := range m.active {
		workers = append(workers, w)
	}
	m.mu.Unlock()
	result := make([]StreamPerformance, 0, len(workers))
	for _, w := range workers {
		w.mu.Lock()
		stats := w.performance
		stats.Watching = w.hasVisibleViewerLocked()
		if time.Since(w.lastWriteAt) > 2*time.Second {
			stats.SendFPS = 0
		}
		if time.Since(stats.RenderSampleAt) > 5*time.Second {
			stats.RenderFPS = 0
		}
		if time.Since(w.lastFrameAt) > 2*time.Second {
			stats.CaptureFPS = 0
		}
		if time.Since(stats.FeedbackAt) > 10*time.Second {
			stats.DisplayFPS = 0
		}
		w.mu.Unlock()
		result = append(result, stats)
	}
	return result
}

// All GPU workers use one snapshot and one weighted allocation. Cache for half
// a second so independent pulse loops share the same soft budget decision.
func (m *WindowsProcessManager) gpuBudgetFor(token string) float64 {
	m.budgetMu.Lock()
	defer m.budgetMu.Unlock()
	if time.Since(m.budgetAt) < 500*time.Millisecond {
		if fps, ok := m.budgets[token]; ok {
			return fps
		}
	}
	m.mu.Lock()
	settings := DefaultPerformanceSettings()
	if m.performanceSource != nil {
		settings = m.performanceSource()
	}
	workers := make([]*windowsWorker, 0, len(m.active))
	for _, w := range m.active {
		if w.RenderMode == "nvidia" {
			workers = append(workers, w)
		}
	}
	m.mu.Unlock()
	demands := make([]GPUFrameDemand, 0, len(workers))
	for _, w := range workers {
		w.mu.Lock()
		p := w.performance
		d := GPUFrameDemand{Token: w.Token, TargetFPS: p.TargetFPS, Watching: w.hasVisibleViewerLocked(), Interactive: time.Since(w.lastInputAt) < 3*time.Second, Policy: p.Policy}
		if time.Since(p.FeedbackAt) < 10*time.Second && time.Since(w.lastFrameAt) < 2*time.Second {
			d.CaptureFPS, d.FrameAckMS, d.WriteMS, d.DecodeMS = p.CaptureFPS, p.FrameAckMS, p.WriteMS, p.DecodeMS
		}
		w.mu.Unlock()
		demands = append(demands, d)
	}
	m.budgets = AllocateGPUFrameBudgets(demands, float64(settings.GPUFPSBudget))
	m.budgetAt = time.Now()
	if fps, ok := m.budgets[token]; ok {
		return fps
	}
	return 2
}

func validFeedback(value float64, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= maximum
}

func (w *windowsWorker) recordFrameFeedback(viewer *windowsViewer, id uint64, decode, draw, fps float64, dropped uint64, inputLatency ...float64) {
	if !validFeedback(decode, 10000) || !validFeedback(draw, 10000) || !validFeedback(fps, 240) || id == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	sample := &viewer.sentFrames[id%uint64(len(viewer.sentFrames))]
	if sample.id != id || sample.at.IsZero() {
		return
	}
	latency := float64(time.Since(sample.at)) / float64(time.Millisecond)
	if sample.inputID > 0 && len(inputLatency) > 0 && inputLatency[0] > 0 && validFeedback(inputLatency[0], 30000) {
		w.performance.InputFrameMS = smoothSample(w.performance.InputFrameMS, inputLatency[0])
		w.performance.InputSampleAt = time.Now()
	}
	*sample = sentFrameSample{} // Duplicate acknowledgements cannot skew samples.
	p := &w.performance
	p.FrameAckMS = smoothSample(p.FrameAckMS, latency)
	p.DecodeMS = smoothSample(p.DecodeMS, decode)
	p.DrawMS = smoothSample(p.DrawMS, draw)
	p.DisplayFPS = fps
	p.ClientDropped = dropped
	p.FeedbackAt = time.Now()
}

// Counts distinct application rAF timestamps, not physical GPU presents. No
// synthetic animation is added: static/non-rAF applications can report zero.
const renderMeterScript = `(() => {
  const request = window.requestAnimationFrame.bind(window);
  let first=performance.now(), last=-1, count=0, value=0, sampled=0;
  window.requestAnimationFrame = cb => request(ts => {
    if (ts !== last) { last=ts; count++; }
    const now=performance.now();
    if (now-first>=1000) {value=count*1000/(now-first);first=now;count=0;sampled=now;}
    cb(ts);
  });
  Object.defineProperty(window,'__g3dRenderFPS',{get:()=>performance.now()-sampled>2000?0:value});
})();`

func (w *windowsWorker) sampleRenderPerformance() {
	clock := time.NewTicker(2 * time.Second)
	defer clock.Stop()
	for {
		select {
		case <-w.closed:
			return
		case <-clock.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		data, err := w.call(ctx, "Runtime.evaluate", map[string]any{"expression": "window.__g3dRenderFPS || 0", "returnByValue": true})
		cancel()
		var response struct {
			Result struct {
				Value float64 `json:"value"`
			} `json:"result"`
		}
		if err == nil && json.Unmarshal(data, &response) == nil && validFeedback(response.Result.Value, 1000) {
			w.mu.Lock()
			w.performance.RenderFPS = response.Result.Value
			w.performance.RenderSampleAt = time.Now()
			w.mu.Unlock()
		}
	}
}

func (w *windowsWorker) markInputSent(viewer *windowsViewer, id uint64) {
	if id == 0 || id > 9007199254740991 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if id > viewer.inputID {
		viewer.inputID = id
		viewer.inputFrame = w.frameCount
	}
}
