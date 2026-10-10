//go:build !linux

package monitor

import (
	"context"

	"github.com/shirou/gopsutil/v4/disk"
)

func sampleDisks(ctx context.Context) ([]Partition, error) {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, err
	}
	result := []Partition{}
	seen := make(map[string]bool)
	for _, part := range parts {
		if part.Mountpoint == "" || seen[part.Mountpoint] {
			continue
		}
		seen[part.Mountpoint] = true
		usage, err := disk.UsageWithContext(ctx, part.Mountpoint)
		if err != nil || usage.Total == 0 {
			continue
		}
		result = append(result, Partition{Mountpoint: part.Mountpoint, Device: part.Device, FSType: part.Fstype,
			UsedBytes: usage.Used, TotalBytes: usage.Total, Percent: usage.UsedPercent})
	}
	return result, nil
}
