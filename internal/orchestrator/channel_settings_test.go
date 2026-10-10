package orchestrator

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func channelTestPorts(t *testing.T) *PortAllocator {
	t.Helper()
	ports, err := NewPortAllocator(PortRange{30000, 30002}, PortRange{31000, 31002}, PortRange{52000, 52023}, 8)
	if err != nil {
		t.Fatal(err)
	}
	return ports
}

func TestChannelSettingsDefaultResizePersistenceAndRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "channel-settings.json")
	ports := channelTestPorts(t)
	service, err := NewChannelSettings(path, ports)
	if err != nil {
		t.Fatal(err)
	}
	if got := service.Settings(); got != (ChannelSettings{MaxChannels: 3}) {
		t.Fatalf("default settings: %+v", got)
	}
	if err := service.SetMaxChannels(10); err != nil {
		t.Fatal(err)
	}
	var leases []PortLease
	for i := range 10 {
		lease, err := ports.Allocate()
		if err != nil {
			t.Fatal(err)
		}
		if lease.HTTPPort != 30000+i || lease.CDPPort != 31000+i || lease.UDPStart != 52000+i*8 || lease.UDPEnd != 52007+i*8 {
			t.Fatalf("resized pool changed configured starting ports or block width: %+v", lease)
		}
		leases = append(leases, lease)
	}
	if _, err := ports.Allocate(); !errors.Is(err, ErrPortsExhausted) {
		t.Fatalf("eleventh allocation should be refused: %v", err)
	}
	if got := service.Settings(); got != (ChannelSettings{MaxChannels: 10, ActiveChannels: 10}) {
		t.Fatalf("allocated worker count: %+v", got)
	}
	for _, lease := range leases {
		lease.Release()
	}
	restored, err := NewChannelSettings(path, channelTestPorts(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := restored.Settings(); got != (ChannelSettings{MaxChannels: 10}) {
		t.Fatalf("settings did not restore: %+v", got)
	}
	// Replacing an existing settings file is required on Windows as well.
	if err := restored.SetMaxChannels(1); err != nil {
		t.Fatal(err)
	}
	restoredAgain, err := NewChannelSettings(path, channelTestPorts(t))
	if err != nil || restoredAgain.Settings().MaxChannels != 1 {
		t.Fatalf("updated settings did not restore: %v", err)
	}
}

func TestChannelSettingsShrinkRejectsOccupiedHighPortsWithoutChangingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channel-settings.json")
	ports := channelTestPorts(t)
	service, err := NewChannelSettings(path, ports)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetMaxChannels(3); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var leases []PortLease
	for range 3 {
		lease, err := ports.Allocate()
		if err != nil {
			t.Fatal(err)
		}
		leases = append(leases, lease)
	}
	leases[0].Release()
	leases[1].Release()
	// One active worker alone is not enough evidence that a two-slot pool is
	// safe: that worker still owns the third slot's original port numbers.
	if err := service.SetMaxChannels(2); !errors.Is(err, ErrChannelsInUse) {
		t.Fatalf("shrink moved/disconnected an occupied high slot: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || service.Settings() != (ChannelSettings{MaxChannels: 3, ActiveChannels: 1}) {
		t.Fatalf("rejected shrink changed settings: %+v, %v", service.Settings(), err)
	}
	leases[2].Release()
	if err := service.SetMaxChannels(1); err != nil {
		t.Fatal(err)
	}
}

func TestChannelSettingsInvalidCapacityAndCorruptedRestore(t *testing.T) {
	for _, value := range []int{-1, 0, 101} {
		t.Run(strconv.Itoa(value), func(t *testing.T) {
			service, err := NewChannelSettings(filepath.Join(t.TempDir(), "channels.json"), channelTestPorts(t))
			if err != nil {
				t.Fatal(err)
			}
			if err := service.SetMaxChannels(value); !errors.Is(err, ErrInvalidChannelCapacity) || service.Settings().MaxChannels != 3 {
				t.Fatalf("invalid capacity %d accepted or changed pool: %v", value, err)
			}
		})
	}
	for _, data := range []string{"", "{", "{}", `{"max_channels":0}`, `{"max_channels":101}`, `{"max_channels":1.5}`, `{"max_channels":"10"}`} {
		t.Run("corrupt-"+data, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "channels.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			ports := channelTestPorts(t)
			if _, err := NewChannelSettings(path, ports); err == nil || ports.Capacity() != 3 {
				t.Fatalf("corrupt saved settings accepted or pool changed: %v", err)
			}
		})
	}
	for _, scenario := range []struct {
		name string
		http PortRange
		cdp  PortRange
		udp  PortRange
		n    int
	}{
		{"TCP-overlap", PortRange{30000, 30000}, PortRange{30001, 30001}, PortRange{52000, 52007}, 2},
		{"TCP-overflow", PortRange{65535, 65535}, PortRange{31000, 31000}, PortRange{52000, 52007}, 2},
		{"UDP-overflow", PortRange{30000, 30000}, PortRange{31000, 31000}, PortRange{65528, 65535}, 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ports, err := NewPortAllocator(scenario.http, scenario.cdp, scenario.udp, 8)
			if err != nil {
				t.Fatal(err)
			}
			service, err := NewChannelSettings(filepath.Join(t.TempDir(), "channels.json"), ports)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.SetMaxChannels(scenario.n); !errors.Is(err, ErrInvalidChannelCapacity) || ports.Capacity() != 1 {
				t.Fatalf("unsafe port expansion accepted: %v", err)
			}
		})
	}
}

func TestChannelSettingsPersistenceFailureKeepsPoolAndPreviousFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "channels.json")
	service, err := NewChannelSettings(path, channelTestPorts(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetMaxChannels(3); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("file prevents directory creation"), 0600); err != nil {
		t.Fatal(err)
	}
	renameBlocker := filepath.Join(root, "directory-destination")
	if err := os.Mkdir(renameBlocker, 0700); err != nil {
		t.Fatal(err)
	}
	for _, invalidPath := range []string{filepath.Join(blocker, "channels.json"), renameBlocker} {
		service.path = invalidPath
		if err := service.SetMaxChannels(10); err == nil {
			t.Fatal("expected real filesystem persistence failure")
		}
		if got := service.Settings(); got.MaxChannels != 3 {
			t.Fatalf("failed persistence changed capacity: %+v", got)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("failed persistence changed saved setting: %v", err)
		}
		if staged, _ := filepath.Glob(filepath.Join(root, ".gostream3d-channels-*")); len(staged) != 0 {
			t.Fatalf("failed persistence leaked staged files: %v", staged)
		}
	}
}

func TestChannelSettingsConcurrentResizeAllocationAndStatus(t *testing.T) {
	service, err := NewChannelSettings(filepath.Join(t.TempDir(), "channels.json"), channelTestPorts(t))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 40 {
				lease, err := service.ports.Allocate()
				if err != nil && !errors.Is(err, ErrPortsExhausted) {
					t.Error(err)
				}
				if err == nil {
					lease.Release()
				}
				_ = service.Settings()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 20 {
			if err := service.SetMaxChannels(3 + i%8); err != nil && !errors.Is(err, ErrChannelsInUse) {
				t.Error(err)
			}
		}
	}()
	wg.Wait()
	if got := service.Settings(); got.ActiveChannels != 0 {
		t.Fatalf("concurrent operations lost a lease: %+v", got)
	}
	if err := service.SetMaxChannels(100); err != nil {
		t.Fatal(err)
	}
}
