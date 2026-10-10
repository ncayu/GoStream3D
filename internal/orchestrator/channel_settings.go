package orchestrator

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrInvalidChannelCapacity = errors.New("channel capacity must be between 1 and 100 with valid port ranges")
var ErrChannelsInUse = errors.New("running render channels use ports outside the requested capacity")

// ChannelSettings describes the configured worker limit, including preloaded
// and starting workers. It is not a measurement of GPU hardware performance.
type ChannelSettings struct {
	MaxChannels    int `json:"max_channels"`
	ActiveChannels int `json:"active_channels"`
}

type ChannelSettingsService struct {
	path  string
	ports *PortAllocator
}

type savedChannelSettings struct {
	MaxChannels int `json:"max_channels"`
}

// NewChannelSettings restores the saved pool size before renderers are started.
// With no saved setting the configured port pool is retained as-is.
func NewChannelSettings(path string, ports *PortAllocator) (*ChannelSettingsService, error) {
	if path == "" || ports == nil {
		return nil, errors.New("channel settings path and port allocator are required")
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read channel settings: %w", err)
	}
	service := &ChannelSettingsService{path: path, ports: ports}
	if errors.Is(err, os.ErrNotExist) {
		if capacity := ports.Capacity(); capacity < 1 || capacity > 100 {
			return nil, fmt.Errorf("%w: configured capacity %d", ErrInvalidChannelCapacity, capacity)
		}
		return service, nil
	}
	var saved savedChannelSettings
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("decode channel settings: %w", err)
	}
	ports.mu.Lock()
	defer ports.mu.Unlock()
	httpPorts, cdpPorts, udpPorts, err := ports.rangesForCapacityLocked(saved.MaxChannels)
	if err != nil {
		return nil, fmt.Errorf("restore channel settings: %w", err)
	}
	ports.http, ports.cdp, ports.udp = httpPorts, cdpPorts, udpPorts
	return service, nil
}

func (s *ChannelSettingsService) Settings() ChannelSettings {
	s.ports.mu.Lock()
	defer s.ports.mu.Unlock()
	return ChannelSettings{MaxChannels: s.ports.capacityLocked(), ActiveChannels: len(s.ports.leasing)}
}

// SetMaxChannels stages and atomically replaces the settings file while holding
// the allocator lock. Existing workers retain their ports; unsafe shrinkage is
// rejected. Allocation sees the new ranges only after persistence succeeds.
func (s *ChannelSettingsService) SetMaxChannels(value int) error {
	s.ports.mu.Lock()
	defer s.ports.mu.Unlock()
	httpPorts, cdpPorts, udpPorts, err := s.ports.rangesForCapacityLocked(value)
	if err != nil {
		return err
	}
	if err := s.save(value); err != nil {
		return err
	}
	s.ports.http, s.ports.cdp, s.ports.udp = httpPorts, cdpPorts, udpPorts
	return nil
}

func (a *PortAllocator) rangesForCapacityLocked(value int) (PortRange, PortRange, PortRange, error) {
	if value < 1 || value > 100 {
		return PortRange{}, PortRange{}, PortRange{}, ErrInvalidChannelCapacity
	}
	httpPorts := PortRange{Start: a.http.Start, End: a.http.Start + value - 1}
	cdpPorts := PortRange{Start: a.cdp.Start, End: a.cdp.Start + value - 1}
	udpPorts := PortRange{Start: a.udp.Start, End: a.udp.Start + value*a.udpWidth - 1}
	for _, pool := range []PortRange{httpPorts, cdpPorts, udpPorts} {
		if err := pool.validate(); err != nil {
			return PortRange{}, PortRange{}, PortRange{}, fmt.Errorf("%w: %v", ErrInvalidChannelCapacity, err)
		}
	}
	if httpPorts.Start <= cdpPorts.End && cdpPorts.Start <= httpPorts.End {
		return PortRange{}, PortRange{}, PortRange{}, fmt.Errorf("%w: HTTP and CDP TCP ranges overlap", ErrInvalidChannelCapacity)
	}
	for _, lease := range a.leasing {
		if lease.HTTPPort > httpPorts.End || lease.CDPPort > cdpPorts.End || lease.UDPEnd > udpPorts.End {
			return PortRange{}, PortRange{}, PortRange{}, ErrChannelsInUse
		}
	}
	return httpPorts, cdpPorts, udpPorts, nil
}

func (s *ChannelSettingsService) save(value int) error {
	data, err := json.MarshalIndent(savedChannelSettings{MaxChannels: value}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode channel settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("create channel settings directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".gostream3d-channels-*")
	if err != nil {
		return fmt.Errorf("stage channel settings: %w", err)
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return fmt.Errorf("protect channel settings: %w", err)
	}
	if _, err := temp.Write(append(data, '\n')); err != nil {
		temp.Close()
		return fmt.Errorf("write channel settings: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("flush channel settings: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close channel settings: %w", err)
	}
	if err := os.Rename(temp.Name(), s.path); err != nil {
		return fmt.Errorf("replace channel settings: %w", err)
	}
	return nil
}
