//go:build linux

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gostream3d/internal/config"
	"gostream3d/internal/linuxsetup"
)

var linuxConfigDirectory string

func platformDefaultConfigPath() string {
	executable, err := os.Executable()
	if err != nil {
		return "config.env"
	}
	directory, _ := os.Getwd()
	return linuxsetup.DefaultConfig(executable, directory)
}

func platformPrepareConfig(path string, explicit bool) (string, error) {
	prepared, err := linuxsetup.Prepare(path, explicit)
	if prepared != "" {
		linuxConfigDirectory = filepath.Dir(prepared)
	}
	return prepared, err
}

func platformResolveDataPaths(cfg *config.Config) error {
	if linuxConfigDirectory == "" {
		return nil // Environment-only deployments keep their working directory.
	}
	for _, path := range []*string{&cfg.SessionStore, &cfg.Native.WorkRoot, &cfg.WebDist} {
		if *path != "" && !filepath.IsAbs(*path) {
			*path = filepath.Join(linuxConfigDirectory, *path)
		}
	}
	cfg.Native.WebDist = cfg.WebDist
	return nil
}

func platformLogOutput() (io.Writer, func(), string) {
	return os.Stdout, func() {}, "" // systemd captures stdout and stderr.
}

func platformReportError(_ string, _ error, _ string) {}

func platformOpenBrowser(address string) {
	fmt.Fprintln(os.Stdout, "管理后台 / Admin:", address)
}
