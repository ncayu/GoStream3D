package cpulimit

import (
	"fmt"
	"os"
	"strings"
)

// The cgroup is shared by the gateway and all workers. Limiting individual
// containers would multiply the user's total budget as sessions are added.
type CgroupController struct {
	path  string
	cores int
}

func NewCgroupController(path string, cores int) (*CgroupController, error) {
	if cores < 1 {
		return nil, fmt.Errorf("invalid CPU capacity")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read delegated cgroup CPU controller: %w", err)
	}
	if len(strings.Fields(string(raw))) != 2 {
		return nil, fmt.Errorf("invalid cgroup v2 cpu.max")
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("CPU controller is not writable: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return &CgroupController{path: path, cores: cores}, nil
}

func (c *CgroupController) Apply(percent int) error {
	if percent < 0 || percent > 100 {
		return ErrInvalidPercent
	}
	quota := "max"
	if percent > 0 {
		quota = fmt.Sprint(int64(c.cores) * 100000 * int64(percent) / 100)
	}
	expected := quota + " 100000"
	if err := os.WriteFile(c.path, []byte(expected+"\n"), 0600); err != nil {
		return fmt.Errorf("apply shared CPU quota: %w", err)
	}
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(raw)) != expected {
		return fmt.Errorf("CPU quota readback mismatch")
	}
	return nil
}
