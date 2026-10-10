package orchestrator

import (
	"context"
	"errors"
	"gostream3d/internal/monitor"
	"math"
	"sort"
	"sync"
	"time"
)

type CapacityRequest struct {
	ProjectToken string  `json:"project_token"`
	MaxStreams   int     `json:"max_streams"`
	Seconds      int     `json:"seconds"`
	MinFPS       float64 `json:"min_fps"`
}
type CapacityStream struct {
	Token          string  `json:"token"`
	AverageFPS     float64 `json:"average_fps"`
	LowFPS         float64 `json:"p10_fps"`
	RenderFPS      float64 `json:"render_fps"`
	CaptureFPS     float64 `json:"capture_fps"`
	WriteMS        float64 `json:"write_ms"`
	BytesPerSecond float64 `json:"bytes_per_second"`
	Samples        int     `json:"samples"`
}
type CapacityStage struct {
	GPUsAtEnd    []monitor.GPU    `json:"gpus_at_end"`
	OtherWorkers int              `json:"other_workers"`
	Streams      int              `json:"streams"`
	Passed       bool             `json:"passed"`
	Measurements []CapacityStream `json:"measurements"`
	Error        string           `json:"error,omitempty"`
}
type CapacityReport struct {
	CPUCap           int                 `json:"cpu_max_percent"`
	AvailableAtStart int                 `json:"available_channels_at_start"`
	Status           string              `json:"status"`
	Supported        bool                `json:"supported"`
	StartedAt        time.Time           `json:"started_at"`
	FinishedAt       time.Time           `json:"finished_at"`
	Project          SessionInfo         `json:"project"`
	Request          CapacityRequest     `json:"request"`
	Settings         PerformanceSettings `json:"settings"`
	CurrentStreams   int                 `json:"current_streams"`
	Recommended      int                 `json:"recommended_streams"`
	Stages           []CapacityStage     `json:"stages"`
	Error            string              `json:"error,omitempty"`
}
type CapacityRunner interface {
	RunCapacityStage(context.Context, SessionInfo, int, time.Duration) (CapacityStage, error)
}
type CapacityService struct {
	cpuSource func() int
	mu        sync.Mutex
	runner    CapacityRunner
	available func() int
	settings  func() PerformanceSettings
	path      string
	report    CapacityReport
	cancel    context.CancelFunc
	done      chan struct{}
}

func NewCapacityService(runner CapacityRunner, available func() int, settings func() PerformanceSettings, path string) *CapacityService {
	s := &CapacityService{runner: runner, available: available, settings: settings, path: path, report: CapacityReport{Status: "idle", Supported: runner != nil, Stages: []CapacityStage{}}}
	// A saved report is evidence for its recorded conditions, never an automatic
	// channel quota. An interrupted process cannot resume a previous load test.
	if data, err := readCapacityReport(path); err == nil {
		s.report = data
		s.report.Supported = runner != nil
		if data.Status == "running" {
			s.report.Status = "interrupted"
			s.report.Error = "上次测试被程序退出中断，请重新测试。"
		}
	}
	return s
}
func (s *CapacityService) Snapshot() CapacityReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.report
	p.Stages = append([]CapacityStage{}, p.Stages...)
	for i := range p.Stages {
		p.Stages[i].Measurements = append([]CapacityStream{}, p.Stages[i].Measurements...)
	}
	return p
}
func (s *CapacityService) SetCPUPercentSource(source func() int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cpuSource = source
}
func (s *CapacityService) cpuPercent() int {
	if s.cpuSource != nil {
		return s.cpuSource()
	}
	return 0
}
func (s *CapacityService) Start(parent context.Context, info SessionInfo, input CapacityRequest) error {
	if s.runner == nil {
		return errors.New("当前节点不支持本机场景容量测试")
	}
	if input.ProjectToken != info.Token || input.MaxStreams < 1 || input.MaxStreams > 10 || input.Seconds < 30 || input.Seconds > 120 || math.IsNaN(input.MinFPS) || math.IsInf(input.MinFPS, 0) || input.MinFPS < 1 || input.MinFPS > float64(info.FPS) {
		return errors.New("请选择项目、1–10 路、30–120 秒和不超过目标帧率的达标 FPS")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return errors.New("容量测试正在运行或清理，请先取消并等待结束")
	}
	if s.available == nil || s.available() < input.MaxStreams {
		return errors.New("空闲通道不足；请先释放足够通道，测试不会回收项目实例")
	}
	ctx, cancel := context.WithTimeout(parent, 25*time.Minute)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.report = CapacityReport{Status: "running", Supported: true, StartedAt: time.Now().UTC(), Project: info, Request: input, Settings: s.settings(), Stages: []CapacityStage{}}
	s.report.CPUCap = s.cpuPercent()
	s.report.AvailableAtStart = s.available()
	go s.run(ctx, info, input)
	return nil
}
func (s *CapacityService) run(ctx context.Context, info SessionInfo, input CapacityRequest) {
	status, reason := "complete", ""
	for count := 1; count <= input.MaxStreams; count++ {
		if ctx.Err() != nil {
			status, reason = "cancelled", ctx.Err().Error()
			break
		}
		s.mu.Lock()
		s.report.CurrentStreams = count
		s.mu.Unlock()
		stage, err := s.runner.RunCapacityStage(ctx, info, count, time.Duration(input.Seconds)*time.Second)
		s.mu.Lock()
		changed := s.settings() != s.report.Settings || s.cpuPercent() != s.report.CPUCap
		s.mu.Unlock()
		if changed {
			err = errors.Join(err, errors.New("性能策略或 CPU 配额在测试过程中变化，请重新测试"))
		}
		if err != nil {
			stage.Error = err.Error()
			status, reason = "failed", err.Error()
		}
		stage.Passed = err == nil && len(stage.Measurements) == count
		for _, m := range stage.Measurements {
			if m.Samples < input.Seconds || math.IsNaN(m.LowFPS) || math.IsInf(m.LowFPS, 0) || m.LowFPS < input.MinFPS {
				stage.Passed = false
			}
		}
		s.mu.Lock()
		s.report.Stages = append(s.report.Stages, stage)
		if stage.Passed {
			s.report.Recommended = count
		}
		s.mu.Unlock()
		if !stage.Passed {
			break
		}
	}
	if ctx.Err() != nil {
		status = "cancelled"
		reason = ctx.Err().Error()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.report.Status = status
	s.report.Error = reason
	s.report.FinishedAt = time.Now().UTC()
	if err := saveCapacityReport(s.path, s.report); err != nil {
		s.report.Error += "；保存测试报告失败：" + err.Error()
	}
	s.cancel()
	s.cancel = nil
	close(s.done)
}
func (s *CapacityService) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}
func (s *CapacityService) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.cancel == nil {
		s.mu.Unlock()
		return nil
	}
	s.cancel()
	done := s.done
	s.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func lowFPS(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copy := append([]float64{}, values...)
	sort.Float64s(copy)
	return copy[int(math.Floor(float64(len(copy))*.1))]
}
