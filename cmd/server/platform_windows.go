//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"gostream3d/internal/config"
)

func platformResolveDataPaths(cfg *config.Config) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate GoStream3D executable: %w", err)
	}
	base := filepath.Dir(exe)
	if !filepath.IsAbs(cfg.SessionStore) {
		cfg.SessionStore = filepath.Join(base, cfg.SessionStore)
	}
	if !filepath.IsAbs(cfg.Native.WorkRoot) {
		cfg.Native.WorkRoot = filepath.Join(base, cfg.Native.WorkRoot)
	}
	return nil
}

func platformDefaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "config.env"
	}
	return filepath.Join(filepath.Dir(exe), "config.env")
}

func platformPrepareConfig(path string, explicit bool) (string, error) {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return "", fmt.Errorf("configuration path %q is a directory", path)
		}
		return path, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if explicit {
		return path, nil // LoadFile reports the missing explicitly named file.
	}
	if os.Getenv("GOSTREAM3D_RUNTIME") != "" {
		return "", nil // Preserve environment-only CLI deployments.
	}
	if err := writeDefaultLocalConfig(path); err != nil {
		return "", err
	}
	return path, nil
}

func platformLogOutput() (io.Writer, func(), string) {
	exe, err := os.Executable()
	if err == nil {
		path := filepath.Join(filepath.Dir(exe), "gostream3d.log")
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
			return io.MultiWriter(os.Stdout, f), func() { _ = f.Close() }, path
		}
	}
	if cacheDir, err := os.UserCacheDir(); err == nil {
		dir := filepath.Join(cacheDir, "GoStream3D")
		if os.MkdirAll(dir, 0700) == nil {
			path := filepath.Join(dir, "gostream3d.log")
			if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
				return io.MultiWriter(os.Stdout, f), func() { _ = f.Close() }, path
			}
		}
	}
	return os.Stdout, func() {}, ""
}

func platformReportError(message string, err error, logPath string) {
	details := fmt.Sprintf("%s:\n%s", message, err)
	if logPath != "" {
		details += "\n\n详细日志：" + logPath
	}
	text, _ := syscall.UTF16PtrFromString(details)
	title, _ := syscall.UTF16PtrFromString("GoStream3D 启动失败")
	proc := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	_, _, _ = proc.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}

func platformOpenBrowser(address string) {
	fmt.Fprintln(os.Stdout, "Open in browser:", address)
	// A browser launched here would join the CPU-limited job and be terminated
	// with the service. Open the printed address from the user's browser instead.
	if os.Getenv("GOSTREAM3D_NO_BROWSER") == "1" || cpuLimitJob != 0 {
		return
	}
	cmd := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", address)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Release()
	} else {
		fmt.Fprintln(os.Stderr, "Could not open browser:", err)
	}
}

// writeDefaultLocalConfig lets a standalone executable start without a setup UI.
// Existing configuration is never overwritten, and credentials stay in the file.
func writeDefaultLocalConfig(path string) error {
	content := "# GoStream3D: edit this file to change login credentials or startup settings.\n" +
		"# Restart the service after changing these values.\n" +
		"GOSTREAM3D_RUNTIME=native\n" +
		"GOSTREAM3D_HTTP_HOST=0.0.0.0\n" +
		"GOSTREAM3D_HTTP_PORT=8888\n" +
		"GOSTREAM3D_ADMIN_USERNAME=admin\n" +
		"GOSTREAM3D_ADMIN_PASSWORD=Admin@2026.com\n" +
		"GOSTREAM3D_CPU_MAX_PERCENT=50\n" +
		"GOSTREAM3D_CPU_RENDERER=swiftshader\n" +
		"GOSTREAM3D_CPU_EFFECTS=original\n" +
		"GOSTREAM3D_KEEP_PRELOADED=true\n"
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create configuration %q: %w", path, err)
	}
	_, writeErr := io.WriteString(file, content)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write configuration %q: %w", path, err)
	}
	return nil
}
