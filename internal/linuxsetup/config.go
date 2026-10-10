// Package linuxsetup prepares portable Linux installations without shell-sourcing secrets.
package linuxsetup

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// DefaultConfig keeps existing working-directory deployments compatible, then
// falls back to the configuration beside the executable.
func DefaultConfig(executable, directory string) string {
	local := filepath.Join(directory, "config.env")
	if _, err := os.Stat(local); err == nil {
		return local
	}
	return filepath.Join(filepath.Dir(executable), "config.env")
}

// Prepare preserves explicit files and environment-only deployments. Newly
// generated configurations use independent credentials and owner-only access.
func Prepare(path string, explicit bool) (string, error) {
	if path == "" {
		return "", nil
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
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
		return "", fmt.Errorf("configuration %q does not exist", path)
	}
	if os.Getenv("GOSTREAM3D_RUNTIME") != "" {
		return "", nil
	}
	if err := Create(path, "native", ""); err != nil {
		return "", err
	}
	return path, nil
}

// Create initializes an installation once; upgrades never replace credentials.
func Create(path, runtimeMode, publicIP string) error {
	if runtimeMode != "native" && runtimeMode != "docker" {
		return errors.New("setup runtime must be native or docker")
	}
	if publicIP == "" {
		publicIP = LocalIPv4()
	}
	if net.ParseIP(publicIP) == nil {
		return errors.New("public IP must be an IP address without a port")
	}
	var secrets [2]string
	for i := range secrets {
		var bytes [24]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return fmt.Errorf("generate configuration credentials: %w", err)
		}
		secrets[i] = hex.EncodeToString(bytes[:])
	}
	content := fmt.Sprintf("# GoStream3D Linux configuration. Existing values are never overwritten.\n"+
		"# Restart after editing. For Internet viewers set the reachable public IP.\n"+
		"GOSTREAM3D_RUNTIME=%s\nGOSTREAM3D_HTTP_HOST=0.0.0.0\nGOSTREAM3D_HTTP_PORT=8888\n"+
		"GOSTREAM3D_ADMIN_USERNAME=admin\nGOSTREAM3D_ADMIN_PASSWORD=Admin@2026.com\n"+
		"GOSTREAM3D_NEKO_USER_PASSWORD=%s\nGOSTREAM3D_NEKO_ADMIN_PASSWORD=%s\n"+
		"GOSTREAM3D_ICE_PUBLIC_IP=%s\nGOSTREAM3D_KEEP_PRELOADED=true\n"+
		"GOSTREAM3D_READY_TIMEOUT=300s\n"+
		"GOSTREAM3D_DOCKER_HTTP_BIND_IP=127.0.0.1\n"+
		"GOSTREAM3D_NEKO_IMAGE=gostream3d/neko/chromium:3.1.5\n",
		runtimeMode, secrets[0], secrets[1], publicIP)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		info, statErr := os.Stat(path)
		if statErr == nil && info.IsDir() {
			return fmt.Errorf("configuration path %q is a directory", path)
		}
		return statErr
	}
	if err != nil {
		return fmt.Errorf("create configuration %q: %w", path, err)
	}
	_, writeErr := file.WriteString(content)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write configuration: %w", err)
	}
	return nil
}

// LocalIPv4 is a LAN convenience, not public-IP or NAT detection.
func LocalIPv4() string {
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.To4() != nil && ip.IsGlobalUnicast() && ip.IsPrivate() {
				return ip.String()
			}
		}
	}
	return "127.0.0.1"
}
