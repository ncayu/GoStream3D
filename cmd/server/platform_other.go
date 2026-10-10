//go:build !windows && !linux

package main

import (
	"io"
	"os"

	"gostream3d/internal/config"
)

func platformResolveDataPaths(_ *config.Config) error { return nil }

func platformDefaultConfigPath() string { return "config.env" }

func platformPrepareConfig(path string, _ bool) (string, error) { return path, nil }

func platformLogOutput() (io.Writer, func(), string) {
	return os.Stdout, func() {}, ""
}

func platformReportError(_ string, _ error, _ string) {}

func platformOpenBrowser(_ string) {}
