package monitor

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func nvidiaSMIPath() (string, error) {
	if path := os.Getenv("GOSTREAM3D_NVIDIA_SMI"); path != "" {
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("nvidia-smi path is a directory")
		}
		return path, nil
	}
	if path, err := exec.LookPath("nvidia-smi"); err == nil {
		return path, nil
	}
	// WSL's Windows-provided tool is not necessarily on the Linux PATH.
	if info, err := os.Stat("/usr/lib/wsl/lib/nvidia-smi"); err == nil && !info.IsDir() {
		return "/usr/lib/wsl/lib/nvidia-smi", nil
	}
	return "", fmt.Errorf("nvidia-smi is missing")
}

// Null means unsupported/unavailable, never a fabricated zero. Metrics refer
// to the entire device, including programs outside GoStream3D.
type GPU struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Utilization *float64 `json:"utilization_percent"`
	Encoder     *float64 `json:"encoder_percent"`
	MemoryUsed  *uint64  `json:"memory_used_bytes"`
	MemoryTotal *uint64  `json:"memory_total_bytes"`
}

type GPUPoint struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Utilization *float64 `json:"utilization_percent"`
}

func gpuHistory(cards []GPU) []GPUPoint {
	points := make([]GPUPoint, len(cards))
	for i, card := range cards {
		points[i] = GPUPoint{ID: card.ID, Name: card.Name}
		if card.Utilization != nil {
			value := *card.Utilization
			points[i].Utilization = &value
		}
	}
	return points
}

func parseGPUs(raw string) ([]GPU, error) {
	records, err := csv.NewReader(strings.NewReader(raw)).ReadAll()
	if err != nil {
		return nil, err
	}
	result := []GPU{}
	number := func(s string, max float64) *float64 {
		v, e := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > max {
			return nil
		}
		return &v
	}
	memory := func(s string) *uint64 {
		v := number(s, 1e9)
		if v == nil {
			return nil
		}
		b := uint64(*v * 1024 * 1024)
		return &b
	}
	for _, r := range records {
		if len(r) != 6 {
			return nil, errors.New("unexpected GPU metrics columns")
		}
		result = append(result, GPU{strings.TrimSpace(r[0]), strings.TrimSpace(r[1]), number(r[2], 100), number(r[3], 100), memory(r[4]), memory(r[5])})
	}
	return result, nil
}

func SampleGPUs(parent context.Context) ([]GPU, string) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	path, err := nvidiaSMIPath()
	if err != nil {
		return []GPU{}, "GPU 采集工具不可用：需向后台容器提供 NVIDIA utility 驱动及 nvidia-smi；WSL2 需映射 /usr/lib/wsl。"
	}
	cmd := exec.CommandContext(ctx, path, "--query-gpu=uuid,name,utilization.gpu,utilization.encoder,memory.used,memory.total", "--format=csv,noheader,nounits")
	hideGPUCommand(cmd)
	data, err := cmd.Output()
	if err != nil {
		return []GPU{}, "NVIDIA GPU 采集失败：请检查后台容器的 NVIDIA runtime、utility 驱动权限及宿主机驱动。"
	}
	cards, err := parseGPUs(string(data))
	if err != nil {
		return []GPU{}, fmt.Sprintf("GPU 数据无法解析：%v", err)
	}
	if len(cards) == 0 {
		return cards, "未检测到可读取的 NVIDIA GPU。"
	}
	for _, card := range cards {
		if card.Utilization == nil || card.Encoder == nil || card.MemoryUsed == nil || card.MemoryTotal == nil {
			return cards, "部分 GPU 指标由驱动返回不可用，WSL2 的 NVML 支持因驱动版本而异；不可用值不会显示为 0。"
		}
	}
	return cards, ""
}
