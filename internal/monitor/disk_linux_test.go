//go:build linux

package monitor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
)

func TestSampleUsesHostDiskMounts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "boot"), 0755); err != nil {
		t.Fatal(err)
	}
	mountinfo := filepath.Join(t.TempDir(), "mountinfo")
	// The root can be a Btrfs subvolume; bind aliases must not double-count it.
	data := "36 25 8:1 /@ / rw - btrfs /dev/sda1 rw\n" +
		"37 36 8:1 /@/data /data rw - btrfs /dev/sda1 rw\n" +
		"38 36 8:2 / /boot rw - ext4 /dev/sda2 rw\n" +
		"39 36 0:3 / /proc rw - proc proc rw\n" +
		"40 36 7:0 / /snap/test ro - squashfs /dev/loop0 ro\n"
	if err := os.WriteFile(mountinfo, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSTREAM3D_HOST_ROOT", root)
	t.Setenv("GOSTREAM3D_HOST_MOUNTINFO", mountinfo)
	svc := New()
	svc.Sample(context.Background())
	got := svc.Snapshot()
	if len(got.Partitions) != 2 || got.Partitions[0].Mountpoint != "/" || got.Partitions[1].Mountpoint != "/boot" {
		t.Fatalf("expected host root and boot partitions only, got %+v", got.Partitions)
	}
	usage, err := disk.Usage(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Disk.TotalBytes != 2*usage.Total {
		t.Fatalf("incorrect aggregate total: %+v", got.Disk)
	}
}

func TestDiskMountEscapesAndFiltering(t *testing.T) {
	input := "1 0 8:1 / /mnt/my\\040disk rw - ext4 /dev/disk\\134name rw\n" +
		"2 0 0:2 / /var/lib/docker/overlay rw - overlay overlay rw\n" +
		"3 0 0:3 / / rw - overlay overlay rw\n" +
		"4 0 0:4 / /sys/firmware/efi rw - efivarfs efivarfs rw\n" +
		"5 0 0:5 / /mnt/../escape rw - ext4 /dev/sda5 rw\n" +
		"6 0 0:6 / /network rw - nfs server:/share rw\n"
	got, err := readDiskMounts(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Mountpoint != "/" || got[2].Mountpoint != "/mnt/my disk" || got[2].Device != `/dev/disk\name` {
		t.Fatalf("unexpected disk mounts: %+v", got)
	}
	if _, err := readDiskMounts(strings.NewReader("invalid\n")); err == nil {
		t.Fatal("malformed mountinfo must fail")
	}
}

func TestHostDiskConfigurationDoesNotFallBackToContainer(t *testing.T) {
	t.Setenv("GOSTREAM3D_HOST_ROOT", t.TempDir())
	t.Setenv("GOSTREAM3D_HOST_MOUNTINFO", "")
	if _, err := sampleDisks(context.Background()); err == nil {
		t.Fatal("partial host configuration must fail")
	}
	t.Setenv("GOSTREAM3D_HOST_MOUNTINFO", filepath.Join(t.TempDir(), "missing"))
	if _, err := sampleDisks(context.Background()); err == nil {
		t.Fatal("missing host mounts must fail instead of reporting container capacity")
	}
}

func TestDiskUnreadableAliasDoesNotHideReadableMount(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "available"), 0755); err != nil {
		t.Fatal(err)
	}
	mountinfo := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(mountinfo, []byte("1 0 8:1 / /gone rw - ext4 /dev/sda1 rw\n2 0 8:1 /data /available rw - ext4 /dev/sda1 rw\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSTREAM3D_HOST_ROOT", root)
	t.Setenv("GOSTREAM3D_HOST_MOUNTINFO", mountinfo)
	got, err := sampleDisks(context.Background())
	if err != nil || len(got) != 1 || got[0].Mountpoint != "/available" {
		t.Fatalf("readable alias was lost: %+v, %v", got, err)
	}
}
