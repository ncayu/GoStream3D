package cpulimit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrInvalidPercent = errors.New("CPU limit must be an integer between 1 and 100")

type Settings struct {
	Percent   int  `json:"cpu_max_percent"`
	Supported bool `json:"supported"`
}

type Service struct {
	mu      sync.Mutex
	percent int
	path    string
	apply   func(int) error
}

// New restores the saved platform setting before browsers can start. A bad
// saved limit is an error: silently falling back could exceed the user's cap.
func New(path string, initial int, apply func(int) error) (*Service, error) {
	if initial < 0 || initial > 100 || apply == nil {
		return nil, errors.New("invalid CPU limit configuration")
	}
	s := &Service{path: path, percent: initial, apply: apply}
	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			var saved struct {
				Percent *int `json:"cpu_max_percent"`
			}
			if err := json.Unmarshal(data, &saved); err != nil {
				return nil, fmt.Errorf("read CPU limit settings: %w", err)
			}
			if saved.Percent == nil || *saved.Percent < 1 || *saved.Percent > 100 {
				return nil, ErrInvalidPercent
			}
			s.percent = *saved.Percent
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if err := apply(s.percent); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Settings{Percent: s.percent, Supported: true}
}

func (s *Service) SetPercent(percent int) error {
	if percent < 1 || percent > 100 {
		return ErrInvalidPercent
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Write and sync first, so ordinary storage errors leave the current CPU
	// protection untouched. Only commit the file after the OS accepts it.
	var temp string
	if s.path != "" {
		var err error
		temp, err = stageSettings(s.path, percent)
		if err != nil {
			return err
		}
		defer os.Remove(temp)
	}
	if err := s.apply(percent); err != nil {
		return err
	}
	if temp != "" {
		if err := os.Rename(temp, s.path); err != nil {
			if rollback := s.apply(s.percent); rollback != nil {
				s.percent = percent // Report the actual applied value if rollback fails.
				return errors.Join(err, fmt.Errorf("restore previous CPU limit: %w", rollback))
			}
			return err
		}
	}
	s.percent = percent
	return nil
}

func stageSettings(path string, percent int) (name string, resultErr error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".gostream3d-cpu-*")
	if err != nil {
		return "", err
	}
	name = file.Name()
	defer func() {
		_ = file.Close()
		if resultErr != nil {
			_ = os.Remove(name)
		}
	}()
	if err = file.Chmod(0600); err != nil {
		return name, err
	}
	data, err := json.MarshalIndent(struct {
		Percent int `json:"cpu_max_percent"`
	}{percent}, "", "  ")
	if err != nil {
		return name, err
	}
	if _, err = file.Write(append(data, '\n')); err != nil {
		return name, err
	}
	if err = file.Sync(); err != nil {
		return name, err
	}
	if err = file.Close(); err != nil {
		return name, err
	}
	return name, nil
}
