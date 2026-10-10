//go:build linux

package monitor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
)

type diskMount struct {
	Partition
	deviceID string
}

func sampleDisks(ctx context.Context) ([]Partition, error) {
	root, mountinfo := os.Getenv("GOSTREAM3D_HOST_ROOT"), os.Getenv("GOSTREAM3D_HOST_MOUNTINFO")
	if (root == "") != (mountinfo == "") {
		return nil, fmt.Errorf("host root and mountinfo must be configured together")
	}
	if mountinfo == "" {
		mountinfo = "/proc/self/mountinfo"
	}
	file, err := os.Open(mountinfo)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	mounts, err := readDiskMounts(file)
	if err != nil {
		return nil, err
	}
	result := []Partition{}
	seen := map[string]bool{}
	for _, mount := range mounts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if seen[mount.deviceID] {
			continue
		}
		usagePath := mount.Mountpoint
		if root != "" {
			usagePath = filepath.Join(root, strings.TrimPrefix(mount.Mountpoint, "/"))
		}
		usage, err := disk.UsageWithContext(ctx, usagePath)
		if err != nil || usage.Total == 0 {
			continue
		}
		seen[mount.deviceID] = true
		part := mount.Partition
		part.UsedBytes, part.TotalBytes, part.Percent = usage.Used, usage.Total, usage.UsedPercent
		result = append(result, part)
	}
	return result, nil
}

// Read the selected mount namespace, rather than excluding every bind mount.
// Btrfs subvolumes and container data mounts are valid storage filesystems too.
func readDiskMounts(reader io.Reader) ([]diskMount, error) {
	result := []diskMount{}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for scanner.Scan() {
		sections := strings.SplitN(scanner.Text(), " - ", 2)
		if len(sections) != 2 {
			return nil, fmt.Errorf("invalid disk mountinfo")
		}
		left, right := strings.Fields(sections[0]), strings.Fields(sections[1])
		if len(left) < 6 || len(right) < 3 {
			return nil, fmt.Errorf("invalid disk mountinfo fields")
		}
		mountpoint := unescape.Replace(left[4])
		if !filepath.IsAbs(mountpoint) || filepath.Clean(mountpoint) != mountpoint {
			continue
		}
		fstype := right[0]
		if !storageFilesystem(fstype, mountpoint) {
			continue
		}
		result = append(result, diskMount{Partition: Partition{Mountpoint: mountpoint, Device: unescape.Replace(right[1]), FSType: fstype}, deviceID: left[2]})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	// Prefer the root or shortest mount over aliases of the same filesystem.
	sort.SliceStable(result, func(i, j int) bool { return len(result[i].Mountpoint) < len(result[j].Mountpoint) })
	return result, nil
}

func storageFilesystem(fstype, mountpoint string) bool {
	switch fstype {
	case "proc", "sysfs", "tmpfs", "devtmpfs", "devpts", "cgroup", "cgroup2", "squashfs", "ramfs",
		"securityfs", "debugfs", "tracefs", "configfs", "pstore", "efivarfs", "mqueue", "hugetlbfs",
		"fusectl", "rpc_pipefs", "autofs", "binfmt_misc", "nsfs", "bpf":
		return false
	case "overlay", "aufs":
		// A standalone container can still report its readable root filesystem.
		return mountpoint == "/"
	}
	return true
}
