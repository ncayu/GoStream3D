//go:build !windows

package monitor

import "os/exec"

func hideGPUCommand(*exec.Cmd) {}
