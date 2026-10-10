package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gostream3d/internal/orchestrator"
)

type Config struct {
	Runtime       string
	RemoteBaseURL string
	HTTPAddr      string
	PublicBaseURL string
	WebDist       string
	SessionStore  string
	AdminAPIKey   string
	AdminUsername string
	AdminPassword string
	HTTPPorts     orchestrator.PortRange
	CDPPorts      orchestrator.PortRange
	UDPPorts      orchestrator.PortRange
	UDPPerWorker  int
	IdleTimeout   time.Duration
	KeepPreloaded bool
	CPUPercent    int
	Docker        orchestrator.DockerConfig
	Native        orchestrator.NativeConfig
}

func Load() (Config, error) {
	httpAddr, defaultPublicBaseURL, httpErr := httpEndpoints()
	if httpErr != nil {
		return Config{}, httpErr
	}
	c := Config{
		Runtime:       getenv("GOSTREAM3D_RUNTIME", "native"),
		RemoteBaseURL: os.Getenv("GOSTREAM3D_REMOTE_BASE_URL"),
		HTTPAddr:      httpAddr,
		PublicBaseURL: strings.TrimRight(getenv("GOSTREAM3D_PUBLIC_BASE_URL", defaultPublicBaseURL), "/"),
		WebDist:       getenv("GOSTREAM3D_WEB_DIST", "web/dist"),
		SessionStore:  getenv("GOSTREAM3D_SESSION_STORE", "data/sessions.json"),
		AdminAPIKey:   os.Getenv("GOSTREAM3D_ADMIN_API_KEY"),
		AdminUsername: os.Getenv("GOSTREAM3D_ADMIN_USERNAME"),
		AdminPassword: os.Getenv("GOSTREAM3D_ADMIN_PASSWORD"),
		Docker: orchestrator.DockerConfig{
			Image:           os.Getenv("GOSTREAM3D_NEKO_IMAGE"),
			NvidiaImage:     os.Getenv("GOSTREAM3D_NEKO_NVIDIA_IMAGE"),
			GPUBackend:      getenv("GOSTREAM3D_GPU_BACKEND", "auto"),
			GPURenderDevice: os.Getenv("GOSTREAM3D_GPU_RENDER_DEVICE"),
			HostRoot:        getenv("GOSTREAM3D_HOST_ROOT", "/"),
			CgroupParent:    os.Getenv("GOSTREAM3D_CGROUP_PARENT"),
			HTTPBindIP:      os.Getenv("GOSTREAM3D_DOCKER_HTTP_BIND_IP"),
			ICEPublicIP:     os.Getenv("GOSTREAM3D_ICE_PUBLIC_IP"),
			UserPassword:    os.Getenv("GOSTREAM3D_NEKO_USER_PASSWORD"),
			AdminPassword:   os.Getenv("GOSTREAM3D_NEKO_ADMIN_PASSWORD"),
			Screen:          getenv("GOSTREAM3D_SCREEN", "1920x1080@30"),
		},
		Native: orchestrator.NativeConfig{
			GPUBackend:      getenv("GOSTREAM3D_GPU_BACKEND", "auto"),
			GPURenderDevice: os.Getenv("GOSTREAM3D_GPU_RENDER_DEVICE"),
			NekoBinary:      getenv("GOSTREAM3D_NEKO_BIN", "neko"),
			ChromiumBinary:  getenv("GOSTREAM3D_CHROMIUM_BIN", "chromium"),
			CPURenderer:     getenv("GOSTREAM3D_CPU_RENDERER", "swiftshader"),
			CPUEffects:      getenv("GOSTREAM3D_CPU_EFFECTS", "original"),
			XorgBinary:      getenv("GOSTREAM3D_XORG_BIN", "Xorg"),
			PulseBinary:     getenv("GOSTREAM3D_PULSE_BIN", "pulseaudio"),
			VirtualGLBinary: getenv("GOSTREAM3D_VGLRUN_BIN", "vglrun"),
			WorkRoot:        getenv("GOSTREAM3D_WORK_ROOT", "data/workers"),
			ICEPublicIP:     os.Getenv("GOSTREAM3D_ICE_PUBLIC_IP"),
			UserPassword:    os.Getenv("GOSTREAM3D_NEKO_USER_PASSWORD"),
			AdminPassword:   os.Getenv("GOSTREAM3D_NEKO_ADMIN_PASSWORD"),
		},
	}
	if c.Runtime != "native" && c.Runtime != "docker" && c.Runtime != "remote" {
		return Config{}, errors.New("GOSTREAM3D_RUNTIME must be native, docker or remote")
	}
	c.Native.WebDist = c.WebDist
	if c.Native.CPURenderer != "swiftshader" && c.Native.CPURenderer != "warp" {
		return Config{}, errors.New("GOSTREAM3D_CPU_RENDERER must be swiftshader or warp")
	}
	if c.Native.CPUEffects != "original" && c.Native.CPUEffects != "simplified" {
		return Config{}, errors.New("GOSTREAM3D_CPU_EFFECTS must be original or simplified")
	}
	if c.Native.CPUEffects == "simplified" && (runtime.GOOS != "windows" || c.Runtime != "native") {
		return Config{}, errors.New("GOSTREAM3D_CPU_EFFECTS=simplified currently requires Windows native mode")
	}
	if (c.AdminUsername == "") != (c.AdminPassword == "") {
		return Config{}, errors.New("GOSTREAM3D_ADMIN_USERNAME and GOSTREAM3D_ADMIN_PASSWORD must be configured together")
	}
	if c.AdminPassword != "" && len(c.AdminPassword) < 12 {
		return Config{}, errors.New("GOSTREAM3D_ADMIN_PASSWORD must contain at least 12 characters")
	}
	if c.Runtime != "remote" && c.AdminAPIKey == "" && c.AdminUsername == "" {
		return Config{}, errors.New("configure GOSTREAM3D_ADMIN_USERNAME and GOSTREAM3D_ADMIN_PASSWORD (or legacy GOSTREAM3D_ADMIN_API_KEY)")
	}
	if c.Runtime == "remote" {
		if err := validateOrigin(c.RemoteBaseURL); err != nil {
			return Config{}, fmt.Errorf("GOSTREAM3D_REMOTE_BASE_URL: %w", err)
		}
	}
	if err := validateOrigin(c.PublicBaseURL); err != nil {
		return Config{}, fmt.Errorf("GOSTREAM3D_PUBLIC_BASE_URL: %w", err)
	}
	var err error
	if c.HTTPPorts, err = parseRange(getenv("GOSTREAM3D_HTTP_PORTS", "30000-30002")); err != nil {
		return Config{}, fmt.Errorf("GOSTREAM3D_HTTP_PORTS: %w", err)
	}
	if c.CDPPorts, err = parseRange(getenv("GOSTREAM3D_CDP_PORTS", "31000-31002")); err != nil {
		return Config{}, fmt.Errorf("GOSTREAM3D_CDP_PORTS: %w", err)
	}
	if c.UDPPorts, err = parseRange(getenv("GOSTREAM3D_UDP_PORTS", "52000-52023")); err != nil {
		return Config{}, fmt.Errorf("GOSTREAM3D_UDP_PORTS: %w", err)
	}
	if c.UDPPerWorker, err = strconv.Atoi(getenv("GOSTREAM3D_UDP_PER_WORKER", "8")); err != nil || c.UDPPerWorker < 1 {
		return Config{}, errors.New("GOSTREAM3D_UDP_PER_WORKER must be positive")
	}
	if c.IdleTimeout, err = time.ParseDuration(getenv("GOSTREAM3D_IDLE_TIMEOUT", "180s")); err != nil || c.IdleTimeout <= 0 {
		return Config{}, errors.New("GOSTREAM3D_IDLE_TIMEOUT must be positive")
	}
	if c.KeepPreloaded, err = strconv.ParseBool(getenv("GOSTREAM3D_KEEP_PRELOADED", "true")); err != nil {
		return Config{}, errors.New("GOSTREAM3D_KEEP_PRELOADED must be true or false")
	}
	defaultCPUPercent := "0"
	if runtime.GOOS == "windows" && c.Runtime == "native" {
		defaultCPUPercent = "50"
	}
	if c.CPUPercent, err = strconv.Atoi(getenv("GOSTREAM3D_CPU_MAX_PERCENT", defaultCPUPercent)); err != nil || c.CPUPercent < 0 || c.CPUPercent > 100 {
		return Config{}, errors.New("GOSTREAM3D_CPU_MAX_PERCENT must be between 0 and 100")
	}
	linuxCPU := runtime.GOOS == "linux" && c.Runtime != "remote" && os.Getenv("GOSTREAM3D_CPU_CGROUP_PATH") != ""
	if c.Runtime == "docker" && os.Getenv("GOSTREAM3D_CPU_CGROUP_PATH") != "" && c.Docker.CgroupParent == "" {
		return Config{}, errors.New("Docker CPU control requires GOSTREAM3D_CGROUP_PARENT shared by the gateway and workers")
	}
	if c.CPUPercent > 0 && !linuxCPU && (runtime.GOOS != "windows" || c.Runtime != "native") {
		return Config{}, errors.New("GOSTREAM3D_CPU_MAX_PERCENT requires Windows native mode or a delegated Linux cgroup CPU controller")
	}
	if c.Docker.MemoryBytes, err = strconv.ParseInt(getenv("GOSTREAM3D_WORKER_MEMORY_BYTES", "4294967296"), 10, 64); err != nil {
		return Config{}, fmt.Errorf("GOSTREAM3D_WORKER_MEMORY_BYTES: %w", err)
	}
	if c.Docker.NanoCPUs, err = strconv.ParseInt(getenv("GOSTREAM3D_WORKER_NANO_CPUS", "0"), 10, 64); err != nil {
		return Config{}, fmt.Errorf("GOSTREAM3D_WORKER_NANO_CPUS: %w", err)
	}
	if c.Docker.ReadyTimeout, err = time.ParseDuration(getenv("GOSTREAM3D_READY_TIMEOUT", "300s")); err != nil || c.Docker.ReadyTimeout <= 0 {
		return Config{}, errors.New("GOSTREAM3D_READY_TIMEOUT must be positive")
	}
	c.Native.ReadyTimeout = c.Docker.ReadyTimeout
	return c, nil
}

func httpEndpoints() (string, string, error) {
	host, port, err := net.SplitHostPort(getenv("GOSTREAM3D_HTTP_ADDR", ":8888"))
	if err != nil {
		return "", "", fmt.Errorf("GOSTREAM3D_HTTP_ADDR: %w", err)
	}
	if configuredHost := os.Getenv("GOSTREAM3D_HTTP_HOST"); configuredHost != "" {
		host = configuredHost
	}
	if configuredPort := os.Getenv("GOSTREAM3D_HTTP_PORT"); configuredPort != "" {
		port = configuredPort
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", "", errors.New("GOSTREAM3D_HTTP_PORT must be between 1 and 65535")
	}
	listenAddr := net.JoinHostPort(host, port)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	publicBaseURL := (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, port)}).String()
	return listenAddr, publicBaseURL, nil
}

func validateOrigin(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("must be an HTTP(S) origin without a path, query or credentials")
	}
	return nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func parseRange(value string) (orchestrator.PortRange, error) {
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return orchestrator.PortRange{}, errors.New("expected start-end")
	}
	start, err1 := strconv.Atoi(parts[0])
	end, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || start < 1 || end > 65535 || start > end {
		return orchestrator.PortRange{}, errors.New("invalid port range")
	}
	return orchestrator.PortRange{Start: start, End: end}, nil
}
