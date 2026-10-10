//go:build windows

package main

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	jobCPUControlEnable  = 0x1
	jobCPUControlHardCap = 0x4
)

type jobCPURateControl struct {
	ControlFlags uint32
	CPURate      uint32
}

// Keep the handle open for the process lifetime. Child browser processes
// inherit membership, and Windows terminates them if the gateway exits.
var cpuLimitJob windows.Handle
var cpuLimitMu sync.Mutex

func applyCPULimit(percent int) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("invalid CPU limit percentage")
	}
	cpuLimitMu.Lock()
	defer cpuLimitMu.Unlock()
	if cpuLimitJob != 0 {
		return setJobCPUPercent(cpuLimitJob, percent)
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create CPU limit job: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("set browser cleanup policy: %w", err)
	}
	// A new job has no CPU quota until one is enabled. This also lets a
	// legacy unlimited startup track all future browsers for live updates.
	if percent > 0 {
		if err = setJobCPUPercent(job, percent); err != nil {
			windows.CloseHandle(job)
			return fmt.Errorf("set CPU hard cap: %w", err)
		}
	}
	process, err := windows.GetCurrentProcess()
	if err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("get current process for CPU cap: %w", err)
	}
	if err = windows.AssignProcessToJobObject(job, process); err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("assign gateway to CPU limit job: %w", err)
	}
	cpuLimitJob = job
	return nil
}

func setJobCPUPercent(job windows.Handle, percent int) error {
	cpu := jobCPURateControl{}
	if percent > 0 {
		cpu.ControlFlags = jobCPUControlEnable | jobCPUControlHardCap
		cpu.CPURate = uint32(percent * 100)
	}
	_, err := windows.SetInformationJobObject(job, windows.JobObjectCpuRateControlInformation,
		uintptr(unsafe.Pointer(&cpu)), uint32(unsafe.Sizeof(cpu)))
	return err
}
