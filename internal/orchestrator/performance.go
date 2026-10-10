package orchestrator

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type PerformanceSettings struct {
	GPUFPSBudget        int  `json:"gpu_fps_budget"`
	ReclaimIdlePreloads bool `json:"reclaim_idle_preloads"`
}

func DefaultPerformanceSettings() PerformanceSettings {
	return PerformanceSettings{GPUFPSBudget: 120, ReclaimIdlePreloads: true}
}

type PerformanceSettingsService struct {
	mu    sync.RWMutex
	path  string
	value PerformanceSettings
}

func NewPerformanceSettings(path string) (*PerformanceSettingsService, error) {
	if path == "" {
		return nil, errors.New("performance settings path is required")
	}
	s := &PerformanceSettingsService{path: path, value: DefaultPerformanceSettings()}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.value); err != nil {
		return nil, err
	}
	if err := validatePerformanceSettings(s.value); err != nil {
		return nil, err
	}
	return s, nil
}

func validatePerformanceSettings(value PerformanceSettings) error {
	if value.GPUFPSBudget < 30 || value.GPUFPSBudget > 1200 {
		return errors.New("GPU 总帧率预算须为 30–1200 的整数")
	}
	return nil
}

func (s *PerformanceSettingsService) Settings() PerformanceSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

func (s *PerformanceSettingsService) Update(value PerformanceSettings) error {
	if err := validatePerformanceSettings(value); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".gostream3d-performance-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), s.path); err != nil {
		return fmt.Errorf("save performance settings: %w", err)
	}
	s.value = value
	return nil
}

type StreamPerformance struct {
	Transport         string             `json:"transport,omitempty"`
	ViewerID          string             `json:"viewer_id,omitempty"`
	ProjectName       string             `json:"project_name,omitempty"`
	WebRTC            *WebRTCPerformance `json:"webrtc,omitempty"`
	RenderFPS         float64            `json:"render_fps"`
	SendFPS           float64            `json:"send_fps"`
	RenderSampleAt    time.Time          `json:"render_sample_at"`
	InputFrameMS      float64            `json:"input_frame_ms"`
	InputSampleAt     time.Time          `json:"input_sample_at"`
	Token             string             `json:"token"`
	ProjectToken      string             `json:"project_token"`
	RenderMode        string             `json:"render_mode"`
	Policy            string             `json:"frame_policy"`
	Watching          bool               `json:"watching"`
	TargetFPS         float64            `json:"target_fps"`
	BudgetFPS         float64            `json:"budget_fps"`
	CaptureFPS        float64            `json:"capture_fps"`
	DisplayFPS        float64            `json:"display_fps"`
	FrameBytes        int                `json:"frame_bytes"`
	SnapshotMS        float64            `json:"snapshot_capture_encode_ms"`
	CaptureDeliveryMS float64            `json:"capture_delivery_ms"`
	PublishMS         float64            `json:"publish_ms"`
	WriteMS           float64            `json:"write_ms"`
	FrameAckMS        float64            `json:"frame_ack_ms"`
	DecodeMS          float64            `json:"decode_ms"`
	DrawMS            float64            `json:"draw_ms"`
	ServerDropped     uint64             `json:"server_dropped"`
	ClientDropped     uint64             `json:"client_dropped"`
	FeedbackAt        time.Time          `json:"feedback_at"`
}

// These measurements are reported by the leased viewer, not encoder timings.
// Pointer fields distinguish unsupported statistics from measured zero.
type WebRTCPerformance struct {
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	Codec         string   `json:"codec"`
	SampleSeconds float64  `json:"sample_seconds"`
	ReceiveFPS    *float64 `json:"receive_fps"`
	DisplayFPS    *float64 `json:"display_fps"`
	BitrateMbps   *float64 `json:"bitrate_mbps"`
	DecodeMS      *float64 `json:"decode_ms"`
	BufferMS      *float64 `json:"buffer_ms"`
	RTTMS         *float64 `json:"rtt_ms"`
	PacketsLost   *float64 `json:"packets_lost"`
	FramesDropped *float64 `json:"frames_dropped"`
}

type GPUFrameDemand struct {
	Token                                     string
	TargetFPS                                 float64
	Watching, Interactive                     bool
	Policy                                    string
	CaptureFPS, FrameAckMS, WriteMS, DecodeMS float64
}

// Weighted, capped sharing is a soft scheduling budget, not a GPU utilization
// limit. Low capture FPS alone may mean a static page, so only latency evidence
// enables feedback reduction. Every result is positive: zero means unlimited
// in the injected requestAnimationFrame scheduler.
func AllocateGPUFrameBudgets(workers []GPUFrameDemand, total float64) map[string]float64 {
	result := make(map[string]float64, len(workers))
	wants, weights := make(map[string]float64), make(map[string]float64)
	for _, w := range workers {
		target := w.TargetFPS
		if target <= 0 {
			target = 30
		}
		want, weight := math.Min(2, target), 0.25
		if w.Watching {
			want, weight = math.Min(6, target), 1
			if w.Policy == "animation" {
				want, weight = target, 2
			}
			if w.Interactive {
				want, weight = target, 3
			}
			if w.CaptureFPS > 0 && (w.FrameAckMS > 250 || w.WriteMS > 50 || w.DecodeMS > 1000/target) {
				want = math.Min(want, math.Max(math.Min(6, target), w.CaptureFPS*1.25))
			}
		}
		wants[w.Token], weights[w.Token] = want, weight
	}
	for len(wants) > 0 && total > 0 {
		weightSum := 0.0
		for _, weight := range weights {
			weightSum += weight
		}
		capped := false
		assigned := 0.0
		for token, want := range wants {
			if want <= total*weights[token]/weightSum {
				result[token] = want
				assigned += want
				delete(wants, token)
				delete(weights, token)
				capped = true
			}
		}
		if !capped {
			for token := range wants {
				result[token] = total * weights[token] / weightSum
			}
			break
		}
		total = math.Max(0, total-assigned)
	}
	return result
}
