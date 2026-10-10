package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	psnet "github.com/shirou/gopsutil/v4/net"
)

const DefaultInterval = 30 * time.Second
const MinInterval = 10 * time.Second
const MaxInterval = 5 * time.Minute
const historyLimit = 720 // Also cap memory when a short interval is configured.

var ErrInvalidInterval = errors.New("monitor interval must be between 10 and 300 seconds")

type Settings struct {
	IntervalSeconds int `json:"interval_seconds"`
}

type Resource struct {
	UsedPercent float64 `json:"used_percent"`
	UsedBytes   uint64  `json:"used_bytes"`
	TotalBytes  uint64  `json:"total_bytes"`
}

type Partition struct {
	Mountpoint string  `json:"mountpoint"`
	Device     string  `json:"device"`
	FSType     string  `json:"fstype"`
	UsedBytes  uint64  `json:"used_bytes"`
	TotalBytes uint64  `json:"total_bytes"`
	Percent    float64 `json:"percent"`
}

type Interface struct {
	Name   string  `json:"name"`
	Up     bool    `json:"up"`
	IPv4   string  `json:"ipv4,omitempty"`
	RxMbps float64 `json:"rx_mbps"`
	TxMbps float64 `json:"tx_mbps"`
}

type NetworkPoint struct {
	Time          time.Time  `json:"time"`
	RxMbps        float64    `json:"rx_mbps"`
	TxMbps        float64    `json:"tx_mbps"`
	CPUPercent    *float64   `json:"cpu_percent"`
	MemoryPercent *float64   `json:"memory_percent"`
	GPUs          []GPUPoint `json:"gpus"`
}

type System struct {
	OS            string `json:"os"`
	Platform      string `json:"platform"`
	Version       string `json:"version"`
	Kernel        string `json:"kernel"`
	Hostname      string `json:"hostname"`
	UptimeSeconds uint64 `json:"uptime_seconds"`
	LocalIP       string `json:"local_ip"`
}

type Snapshot struct {
	GPUs            []GPU          `json:"gpus"`
	GPUMessage      string         `json:"gpu_message"`
	SampledAt       time.Time      `json:"sampled_at"`
	IntervalSeconds int            `json:"interval_seconds"`
	CPUPercent      float64        `json:"cpu_percent"`
	CPUCores        int            `json:"cpu_cores"`
	Memory          Resource       `json:"memory"`
	Disk            Resource       `json:"disk"`
	Partitions      []Partition    `json:"partitions"`
	RxMbps          float64        `json:"rx_mbps"`
	TxMbps          float64        `json:"tx_mbps"`
	Interfaces      []Interface    `json:"interfaces"`
	History         []NetworkPoint `json:"history"`
	System          System         `json:"system"`
	Errors          []string       `json:"errors"`
}

type counter struct {
	received uint64
	sent     uint64
}

// Service samples the server, not a viewer's computer. API reads only its
// cached snapshot, so repeated dashboard requests do not multiply polling.
type Service struct {
	sampleMu        sync.Mutex
	settingsMu      sync.Mutex
	mu              sync.RWMutex
	snapshot        Snapshot
	previous        map[string]counter
	lastNet         time.Time
	interval        time.Duration
	settingsPath    string
	intervalChanged chan struct{}
}

func New() *Service {
	return &Service{previous: make(map[string]counter), interval: DefaultInterval, intervalChanged: make(chan struct{}, 1)}
}

func NewWithSettings(path string) (*Service, error) {
	svc := New()
	svc.settingsPath = path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return svc, nil
	}
	if err != nil {
		return svc, err
	}
	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return svc, fmt.Errorf("read monitor settings: %w", err)
	}
	interval := time.Duration(settings.IntervalSeconds) * time.Second
	if err := validateInterval(interval); err != nil {
		return svc, err
	}
	svc.interval = interval
	return svc, nil
}

func validateInterval(interval time.Duration) error {
	if interval < MinInterval || interval > MaxInterval || interval%time.Second != 0 {
		return ErrInvalidInterval
	}
	return nil
}

func (s *Service) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Settings{IntervalSeconds: int(s.interval / time.Second)}
}

func (s *Service) SetInterval(interval time.Duration) error {
	if err := validateInterval(interval); err != nil {
		return err
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.settingsPath != "" {
		if err := saveSettings(s.settingsPath, Settings{IntervalSeconds: int(interval / time.Second)}); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.interval = interval
	s.mu.Unlock()
	select {
	case s.intervalChanged <- struct{}{}:
	default:
	}
	return nil
}

func saveSettings(path string, settings Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".gostream3d-monitor-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func (s *Service) Run(ctx context.Context) {
	timer := time.NewTimer(time.Duration(s.Settings().IntervalSeconds) * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.Sample(ctx)
		case <-s.intervalChanged:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(time.Duration(s.Settings().IntervalSeconds) * time.Second)
	}
}

func (s *Service) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copy := s.snapshot
	copy.IntervalSeconds = int(s.interval / time.Second)
	copy.Partitions = append([]Partition{}, copy.Partitions...)
	copy.Interfaces = append([]Interface{}, copy.Interfaces...)
	copy.History = append([]NetworkPoint{}, copy.History...)
	for i := range copy.History {
		copy.History[i].GPUs = append([]GPUPoint{}, copy.History[i].GPUs...)
		for j := range copy.History[i].GPUs {
			if value := copy.History[i].GPUs[j].Utilization; value != nil {
				v := *value
				copy.History[i].GPUs[j].Utilization = &v
			}
		}
		if value := copy.History[i].CPUPercent; value != nil {
			v := *value
			copy.History[i].CPUPercent = &v
		}
		if value := copy.History[i].MemoryPercent; value != nil {
			v := *value
			copy.History[i].MemoryPercent = &v
		}
	}
	copy.Errors = append([]string{}, copy.Errors...)
	copy.GPUs = append([]GPU{}, copy.GPUs...)
	for i := range copy.GPUs {
		g := &copy.GPUs[i]
		if g.Utilization != nil {
			v := *g.Utilization
			g.Utilization = &v
		}
		if g.Encoder != nil {
			v := *g.Encoder
			g.Encoder = &v
		}
		if g.MemoryUsed != nil {
			v := *g.MemoryUsed
			g.MemoryUsed = &v
		}
		if g.MemoryTotal != nil {
			v := *g.MemoryTotal
			g.MemoryTotal = &v
		}
	}
	return copy
}

func (s *Service) Sample(parent context.Context) {
	s.sampleMu.Lock()
	defer s.sampleMu.Unlock()
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	now := time.Now().UTC()
	result := Snapshot{SampledAt: now, Partitions: []Partition{}, Interfaces: []Interface{}, History: []NetworkPoint{}, Errors: []string{}}
	point := NetworkPoint{Time: now}
	// Run GPU sampling concurrently within the same bounded sample deadline.
	gpuReady := make(chan struct{})
	var cards []GPU
	var gpuMessage string
	go func() { cards, gpuMessage = SampleGPUs(ctx); close(gpuReady) }()
	if values, err := cpu.PercentWithContext(ctx, 100*time.Millisecond, false); err == nil && len(values) > 0 {
		result.CPUPercent = values[0]
		value := values[0]
		point.CPUPercent = &value
	} else {
		result.Errors = append(result.Errors, "CPU 使用率暂不可用")
	}
	if cores, err := cpu.CountsWithContext(ctx, true); err == nil {
		result.CPUCores = cores
	}
	if memory, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		result.Memory = Resource{UsedPercent: memory.UsedPercent, UsedBytes: memory.Used, TotalBytes: memory.Total}
		value := memory.UsedPercent
		point.MemoryPercent = &value
	} else {
		result.Errors = append(result.Errors, "内存信息暂不可用")
	}
	if partitions, err := sampleDisks(ctx); err == nil {
		result.Partitions = partitions
		for _, part := range partitions {
			result.Disk.TotalBytes += part.TotalBytes
			result.Disk.UsedBytes += part.UsedBytes
		}
	} else {
		result.Errors = append(result.Errors, "磁盘分区暂不可用")
	}
	if result.Disk.TotalBytes > 0 {
		result.Disk.UsedPercent = 100 * float64(result.Disk.UsedBytes) / float64(result.Disk.TotalBytes)
	} else {
		result.Errors = append(result.Errors, "未找到可读取的磁盘分区")
	}
	sort.Slice(result.Partitions, func(i, j int) bool { return result.Partitions[i].Mountpoint < result.Partitions[j].Mountpoint })
	if info, err := host.InfoWithContext(ctx); err == nil {
		result.System = System{OS: info.OS, Platform: info.Platform, Version: info.PlatformVersion,
			Kernel: info.KernelVersion, Hostname: info.Hostname, UptimeSeconds: info.Uptime}
	} else {
		result.System.OS = runtime.GOOS
		result.Errors = append(result.Errors, "系统信息暂不可用")
	}
	s.sampleNetwork(ctx, now, &result)
	<-gpuReady
	result.GPUs, result.GPUMessage = cards, gpuMessage
	point.GPUs = gpuHistory(cards)
	s.mu.Lock()
	point.RxMbps, point.TxMbps = result.RxMbps, result.TxMbps
	result.History = append(append([]NetworkPoint(nil), s.snapshot.History...), point)
	cutoff := now.Add(-time.Hour)
	for len(result.History) > 0 && result.History[0].Time.Before(cutoff) {
		result.History = result.History[1:]
	}
	if len(result.History) > historyLimit {
		result.History = result.History[len(result.History)-historyLimit:]
	}
	s.snapshot = result
	s.mu.Unlock()
}

func (s *Service) sampleNetwork(ctx context.Context, now time.Time, result *Snapshot) {
	interfaces, err := psnet.InterfacesWithContext(ctx)
	if err != nil {
		result.Errors = append(result.Errors, "网络接口暂不可用")
		return
	}
	io, err := psnet.IOCountersWithContext(ctx, true)
	if err != nil {
		result.Errors = append(result.Errors, "网络流量暂不可用")
		return
	}
	byName := make(map[string]psnet.IOCountersStat, len(io))
	for _, item := range io {
		byName[item.Name] = item
	}
	seconds := now.Sub(s.lastNet).Seconds()
	next := make(map[string]counter, len(interfaces))
	selectedPrivateIP := false
	for _, item := range interfaces {
		if hasFlag(item.Flags, "loopback") {
			continue
		}
		current, hasIO := byName[item.Name]
		iface := Interface{Name: item.Name, Up: hasFlag(item.Flags, "up")}
		for _, addr := range item.Addrs {
			ip, _, err := net.ParseCIDR(addr.Addr)
			if err == nil && ip.To4() != nil && !ip.IsLoopback() {
				iface.IPv4 = ip.String()
				if iface.Up && (result.System.LocalIP == "" || (!selectedPrivateIP && ip.IsPrivate())) {
					result.System.LocalIP = iface.IPv4
					selectedPrivateIP = ip.IsPrivate()
				}
				break
			}
		}
		if hasIO {
			if previous, ok := s.previous[item.Name]; ok && seconds > 0 {
				iface.RxMbps = megabitsPerSecond(previous.received, current.BytesRecv, seconds)
				iface.TxMbps = megabitsPerSecond(previous.sent, current.BytesSent, seconds)
			}
			next[item.Name] = counter{received: current.BytesRecv, sent: current.BytesSent}
		}
		if iface.Up {
			result.RxMbps += iface.RxMbps
			result.TxMbps += iface.TxMbps
		}
		result.Interfaces = append(result.Interfaces, iface)
	}
	sort.Slice(result.Interfaces, func(i, j int) bool { return result.Interfaces[i].Name < result.Interfaces[j].Name })
	s.previous, s.lastNet = next, now
}

func hasFlag(flags []string, target string) bool {
	for _, flag := range flags {
		if strings.EqualFold(flag, target) {
			return true
		}
	}
	return false
}

func megabitsPerSecond(before, after uint64, seconds float64) float64 {
	if after < before || seconds <= 0 {
		return 0
	}
	return float64(after-before) * 8 / seconds / 1_000_000
}

func (s Snapshot) Validate() error {
	if s.SampledAt.IsZero() {
		return errors.New("monitor sample is not ready")
	}
	if s.CPUPercent < 0 || s.CPUPercent > 100 {
		return fmt.Errorf("invalid CPU utilization %.2f", s.CPUPercent)
	}
	return nil
}
