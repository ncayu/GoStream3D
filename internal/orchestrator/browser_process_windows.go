//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Each worker owns its browser and any replacement processes. A PID tree is
// insufficient when Edge's launcher exits before its replacement is ready.
// This job nests under the gateway's existing CPU quota job.
func startWindowsBrowser(cmd *exec.Cmd) (job windows.Handle, err error) {
	job, err = windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create browser job: %w", err)
	}
	defer func() {
		if err != nil {
			_ = windows.CloseHandle(job)
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}
	}()
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return job, fmt.Errorf("set browser job cleanup: %w", err)
	}
	// Assign before executing any browser code, so even an immediate relaunch
	// cannot escape tracking. os/exec closes the primary thread handle; locate
	// that suspended thread to resume it after assigning the process.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	cmd.SysProcAttr.HideWindow = true
	if err = cmd.Start(); err != nil {
		return job, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return job, fmt.Errorf("open browser process for job: %w", err)
	}
	defer windows.CloseHandle(process)
	if err = windows.AssignProcessToJobObject(job, process); err != nil {
		return job, fmt.Errorf("assign browser job: %w", err)
	}
	if err = resumeWindowsBrowser(uint32(cmd.Process.Pid)); err != nil {
		return job, err
	}
	return job, nil
}

func resumeWindowsBrowser(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("snapshot browser thread: %w", err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return fmt.Errorf("open browser thread: %w", err)
		}
		_, err = windows.ResumeThread(thread)
		_ = windows.CloseHandle(thread)
		if err != nil {
			return fmt.Errorf("resume browser thread: %w", err)
		}
		return nil
	}
	return fmt.Errorf("find suspended browser thread: %w", err)
}

func stopWindowsBrowserJob(ctx context.Context, job windows.Handle) error {
	if job == 0 {
		return nil
	}
	processes, err := windowsBrowserJobProcesses(job)
	if err != nil {
		return err
	}
	defer func() {
		for _, process := range processes {
			_ = windows.CloseHandle(process)
		}
	}()
	if err := windows.TerminateJobObject(job, 1); err != nil {
		return fmt.Errorf("terminate browser job: %w", err)
	}
	// Termination is asynchronous. Wait for every descendant to exit before
	// releasing ports and allowing the same persistent profile to be reused.
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var accounting struct {
			TotalUserTime, TotalKernelTime, ThisPeriodUserTime, ThisPeriodKernelTime int64
			TotalPageFaults, TotalProcesses, ActiveProcesses, TerminatedProcesses    uint32
		}
		if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil); err != nil {
			return fmt.Errorf("query browser cleanup: %w", err)
		}
		// Job accounting may reach zero before process handles are signaled.
		// Also await the processes observed before termination so profile/port
		// reuse cannot race the replacement browser's final teardown.
		allExited := accounting.ActiveProcesses == 0
		for _, process := range processes {
			status, err := windows.WaitForSingleObject(process, 0)
			if err != nil {
				return fmt.Errorf("wait for browser process cleanup: %w", err)
			}
			allExited = allExited && status == windows.WAIT_OBJECT_0
		}
		if allExited {
			return windows.CloseHandle(job)
		}
		select {
		case <-ctx.Done():
			return errors.Join(errors.New("wait for browser job cleanup"), ctx.Err())
		case <-ticker.C:
		}
	}
}

func windowsBrowserJobProcesses(job windows.Handle) ([]windows.Handle, error) {
	// JOBOBJECT_BASIC_PROCESS_ID_LIST has an 8-byte header and a variable
	// length array of ULONG_PTR process IDs.
	for capacity := 32; ; capacity *= 2 {
		buffer := make([]uintptr, capacity+int(8/unsafe.Sizeof(uintptr(0))))
		err := windows.QueryInformationJobObject(job, windows.JobObjectBasicProcessIdList,
			uintptr(unsafe.Pointer(&buffer[0])), uint32(len(buffer))*uint32(unsafe.Sizeof(uintptr(0))), nil)
		if errors.Is(err, windows.ERROR_MORE_DATA) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("list browser job processes: %w", err)
		}
		count := *(*uint32)(unsafe.Add(unsafe.Pointer(&buffer[0]), 4))
		pids := buffer[int(8/unsafe.Sizeof(uintptr(0))):]
		processes := make([]windows.Handle, 0, count)
		for _, pid := range pids[:count] {
			process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
			if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				continue // The process already exited since the snapshot.
			}
			if err != nil {
				for _, process := range processes {
					_ = windows.CloseHandle(process)
				}
				return nil, fmt.Errorf("open browser process for cleanup: %w", err)
			}
			processes = append(processes, process)
		}
		return processes, nil
	}
}
