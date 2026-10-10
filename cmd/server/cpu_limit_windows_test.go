//go:build windows

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestCPULimitJob(t *testing.T) {
	if os.Getenv("GOSTREAM3D_TEST_CPU_JOB") == "1" {
		// Even legacy unlimited startup must assign browsers to one job,
		// allowing a later platform update to protect existing children.
		if err := applyCPULimit(0); err != nil {
			t.Fatal(err)
		}
		job := cpuLimitJob
		for _, percent := range []int{40, 25, 100, 0, 40} {
			if err := applyCPULimit(percent); err != nil {
				t.Fatal(err)
			}
			if cpuLimitJob != job {
				t.Fatal("runtime update created a new job and lost existing browsers")
			}
			var cpu jobCPURateControl
			if err := windows.QueryInformationJobObject(cpuLimitJob, windows.JobObjectCpuRateControlInformation,
				uintptr(unsafe.Pointer(&cpu)), uint32(unsafe.Sizeof(cpu)), nil); err != nil {
				t.Fatal(err)
			}
			if percent == 0 {
				if cpu.ControlFlags != 0 {
					t.Fatalf("unlimited rollback retained a CPU cap: %+v", cpu)
				}
			} else if cpu.ControlFlags != jobCPUControlEnable|jobCPUControlHardCap || cpu.CPURate != uint32(percent*100) {
				t.Fatalf("unexpected job CPU policy: %+v", cpu)
			}
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCPULimitJob$")
	cmd.Env = append(os.Environ(), "GOSTREAM3D_TEST_CPU_JOB=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("CPU job helper failed: %v\n%s", err, strings.TrimSpace(string(output)))
	}
}
