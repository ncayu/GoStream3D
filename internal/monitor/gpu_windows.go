//go:build windows

package monitor

import (
	"os/exec"
	"syscall"
)

func hideGPUCommand(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
