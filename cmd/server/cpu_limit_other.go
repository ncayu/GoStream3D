//go:build !windows

package main

import "fmt"

func applyCPULimit(percent int) error {
	if percent != 0 {
		return fmt.Errorf("native CPU hard cap is only available on Windows")
	}
	return nil
}
