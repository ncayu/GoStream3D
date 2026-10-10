package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const (
	defaultImage       = "gostream3d/neko/chromium:3.1.5"
	defaultNvidiaImage = "gostream3d/neko/nvidia-chromium:3.1.5"
	nekoHTTPPort       = 8080
	chromiumCDPPort    = 9222
	defaultSHMBytes    = 2 << 30
	defaultReadyTime   = 120 * time.Second
)

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var screenPattern = regexp.MustCompile(`^[1-9][0-9]{2,3}x[1-9][0-9]{2,3}@(30|60|90|120)$`)

type DockerConfig struct {
	Image           string
	NvidiaImage     string
	GPUBackend      string
	GPURenderDevice string
	HostRoot        string
	CgroupParent    string // Shared CPU budget with the gateway (cgroup v2).
	HTTPBindIP      string // Address reachable by the Go gateway; use 127.0.0.1 when it runs on the Docker host.
	ICEPublicIP     string // Publicly reachable address advertised to browser ICE clients.
	UserPassword    string // Internal credentials; never send to the browser.
	AdminPassword   string
	Screen          string // e.g. 1920x1080@30
	MemoryBytes     int64
	NanoCPUs        int64
	ReadyTimeout    time.Duration
}

type Worker struct {
	Adaptive    bool
	GPUBackend  string
	GPURenderer string
	Token       string
	ContainerID string
	HTTPPort    int
	CDPPort     int
	UDPStart    int
	UDPEnd      int
	SignalURL   string // HTTP base URL of the internal Neko service.
	StartedAt   time.Time
	Screen      string
	RenderMode  string
}

type WorkerSpec struct {
	FramePolicy   string
	ProjectToken  string
	Adaptive      bool
	Token         string
	TargetURL     string
	Screen        string
	RenderMode    string
	ReadySelector string
}

type managedWorker struct {
	Worker
	lease        PortLease
	depthControl io.Closer
}

// DockerManager owns workers created by this process. A shared Docker host
// requires one manager instance and nonoverlapping port pools per gateway.
type DockerManager struct {
	client   *client.Client
	ports    *PortAllocator
	cfg      DockerConfig
	mu       sync.Mutex
	active   map[string]managedWorker
	orphan   map[string]managedWorker // Docker rollback failed; retain lease until explicit Stop succeeds.
	starting map[string]chan struct{}
}

func NewDockerManager(cli *client.Client, ports *PortAllocator, cfg DockerConfig) (*DockerManager, error) {
	if cli == nil || ports == nil {
		return nil, errors.New("Docker client and port allocator are required")
	}
	if cfg.Image == "" {
		cfg.Image = defaultImage
	}
	if cfg.NvidiaImage == "" {
		cfg.NvidiaImage = defaultNvidiaImage
	}
	if cfg.HTTPBindIP == "" {
		cfg.HTTPBindIP = "127.0.0.1"
	}
	bindIP, bindErr := netip.ParseAddr(cfg.HTTPBindIP)
	if bindErr != nil || !bindIP.Is4() || bindIP.IsUnspecified() || bindIP.IsMulticast() {
		return nil, errors.New("HTTPBindIP must be a specific IPv4 address reachable by the gateway")
	}
	if cfg.ICEPublicIP != "" && net.ParseIP(cfg.ICEPublicIP) == nil {
		return nil, errors.New("ICEPublicIP must be an IP address")
	}
	if cfg.UserPassword == "" || cfg.AdminPassword == "" || cfg.UserPassword == cfg.AdminPassword {
		return nil, errors.New("distinct nonempty Neko passwords are required")
	}
	if strings.ContainsAny(cfg.UserPassword+cfg.AdminPassword, "\r\n\x00") {
		return nil, errors.New("Neko passwords contain invalid characters")
	}
	if cfg.Screen == "" {
		cfg.Screen = "1920x1080@30"
	}
	if !screenPattern.MatchString(cfg.Screen) || strings.HasSuffix(cfg.Screen, "@90") || strings.HasSuffix(cfg.Screen, "@120") {
		return nil, errors.New("Docker Neko Screen must be WIDTHxHEIGHT@30 or WIDTHxHEIGHT@60")
	}
	if cfg.MemoryBytes == 0 {
		cfg.MemoryBytes = 4 << 30
	}
	if cfg.MemoryBytes < 0 || cfg.NanoCPUs < 0 {
		return nil, errors.New("resource limits cannot be negative")
	}
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = defaultReadyTime
	}
	if cfg.ReadyTimeout < 0 {
		return nil, errors.New("ReadyTimeout cannot be negative")
	}
	return &DockerManager{client: cli, ports: ports, cfg: cfg, active: make(map[string]managedWorker), orphan: make(map[string]managedWorker), starting: make(map[string]chan struct{})}, nil
}

func NewDockerClientFromEnv() (*client.Client, error) {
	return client.New(client.FromEnv)
}

func ValidateTarget(raw string) error {
	if len(raw) > 4096 {
		return errors.New("target URL is too long")
	}
	if strings.ContainsAny(raw, "\r\n\x00") {
		return errors.New("target URL contains control characters")
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return errors.New("target URL must be an absolute HTTP(S) URL without credentials")
	}
	return nil
}

// Start creates a dedicated browser worker and waits for Neko and Chromium.
// A token can have only one worker in this manager.
func (m *DockerManager) Start(ctx context.Context, token, targetURL string) (Worker, error) {
	return m.StartWithScreen(ctx, token, targetURL, m.cfg.Screen)
}

func (m *DockerManager) StartWithScreen(ctx context.Context, token, targetURL, screen string) (Worker, error) {
	return m.StartWorker(ctx, WorkerSpec{Token: token, TargetURL: targetURL, Screen: screen, RenderMode: "cpu"})
}

func (m *DockerManager) StartWorker(ctx context.Context, spec WorkerSpec) (Worker, error) {
	token, targetURL, screen := spec.Token, spec.TargetURL, spec.Screen
	if spec.RenderMode == "" {
		spec.RenderMode = "cpu"
	}
	if spec.RenderMode != "cpu" && spec.RenderMode != "nvidia" {
		return Worker{}, errors.New("render mode must be cpu or nvidia")
	}
	backend, renderDevice := "", ""
	gpuConfig := &container.HostConfig{}
	var gpuEnv []string
	if spec.RenderMode == "nvidia" {
		info, err := m.client.Info(ctx, client.InfoOptions{})
		if err != nil {
			return Worker{}, fmt.Errorf("GPU runtime detection: %w", err)
		}
		_, hasNvidia := info.Info.Runtimes["nvidia"]
		backend, renderDevice, err = resolveGPUBackend(m.cfg.GPUBackend, m.cfg.HostRoot, m.cfg.GPURenderDevice, hasNvidia)
		if err != nil {
			return Worker{}, err
		}
		if backend == "nvidia" {
			gpuEnv, err = configureNvidiaGPU(m.cfg.HostRoot, gpuConfig)
			if err != nil {
				return Worker{}, err
			}
		}
	}
	if err := validateReadySelector(spec.ReadySelector); err != nil {
		return Worker{}, err
	}
	if !tokenPattern.MatchString(token) {
		return Worker{}, errors.New("invalid session token")
	}
	if err := ValidateTarget(targetURL); err != nil {
		return Worker{}, err
	}
	if !screenPattern.MatchString(screen) {
		return Worker{}, errors.New("invalid screen specification")
	}
	if strings.HasSuffix(screen, "@90") || strings.HasSuffix(screen, "@120") {
		return Worker{}, errors.New("Docker Neko supports at most 60 FPS")
	}
	for {
		m.mu.Lock()
		if existing, ok := m.active[token]; ok {
			m.mu.Unlock()
			inspected, err := m.client.ContainerInspect(ctx, existing.ContainerID, client.ContainerInspectOptions{})
			if err != nil && !errdefs.IsNotFound(err) {
				return Worker{}, fmt.Errorf("inspect Neko worker: %w", err)
			}
			if err == nil && inspected.Container.State != nil && inspected.Container.State.Running {
				return existing.Worker, nil
			}
			if err := m.Stop(ctx, token); err != nil {
				return Worker{}, fmt.Errorf("replace stopped Neko worker: %w", err)
			}
			continue
		}
		if _, ok := m.orphan[token]; ok {
			m.mu.Unlock()
			return Worker{}, errors.New("previous container cleanup failed; call Stop before retrying")
		}
		if ch, ok := m.starting[token]; ok {
			m.mu.Unlock()
			select {
			case <-ch:
				continue
			case <-ctx.Done():
				return Worker{}, ctx.Err()
			}
		}
		m.starting[token] = make(chan struct{})
		m.mu.Unlock()
		break
	}
	defer func() {
		m.mu.Lock()
		close(m.starting[token])
		delete(m.starting, token)
		m.mu.Unlock()
	}()
	lease, err := m.ports.Allocate()
	if err != nil {
		return Worker{}, err
	}

	webPort := network.MustParsePort(strconv.Itoa(nekoHTTPPort) + "/tcp")
	cdpPort := network.MustParsePort(strconv.Itoa(chromiumCDPPort) + "/tcp")
	httpBindIP := netip.MustParseAddr(m.cfg.HTTPBindIP)
	udpBindings := network.PortMap{
		webPort: {{HostIP: httpBindIP, HostPort: strconv.Itoa(lease.HTTPPort)}},
		cdpPort: {{HostIP: httpBindIP, HostPort: strconv.Itoa(lease.CDPPort)}},
	}
	exposed := network.PortSet{webPort: struct{}{}, cdpPort: struct{}{}}
	for port := lease.UDPStart; port <= lease.UDPEnd; port++ {
		p := network.MustParsePort(strconv.Itoa(port) + "/udp")
		exposed[p] = struct{}{}
		udpBindings[p] = []network.PortBinding{{HostIP: netip.IPv4Unspecified(), HostPort: strconv.Itoa(port)}}
	}
	env := []string{
		"GOSTREAM3D_TARGET_URL=" + targetURL,
		"GOSTREAM3D_RENDER_MODE=" + spec.RenderMode,
		"NEKO_LEGACY=true",
		"NEKO_PASSWORD=" + m.cfg.UserPassword,
		"NEKO_PASSWORD_ADMIN=" + m.cfg.AdminPassword,
		"NEKO_IMPLICIT_CONTROL=true",
		"NEKO_CONTROL_PROTECTION=false",
		"NEKO_SCREEN=" + screen,
		"NEKO_EPR=" + strconv.Itoa(lease.UDPStart) + "-" + strconv.Itoa(lease.UDPEnd),
		"NEKO_ICELITE=true",
		"NEKO_BIND=:8080",
		"NEKO_FILE_TRANSFER_ENABLED=false",
	}
	depthProfile := gpuDepthProfileRequired(spec.RenderMode, targetURL)
	if depthProfile {
		env = append(env, "GOSTREAM3D_BROWSER_BOOTSTRAP=true")
	}
	if m.cfg.ICEPublicIP != "" {
		env = append(env, "NEKO_NAT1TO1="+m.cfg.ICEPublicIP)
	}
	if spec.RenderMode == "cpu" {
		env = append(env, videoPipelineEnvironment(cpuVideoPipeline(":99.0", screen))...)
	}
	image := m.cfg.Image
	hostConfig := &container.HostConfig{
		PortBindings: udpBindings, ShmSize: defaultSHMBytes,
		Resources:     container.Resources{Memory: m.cfg.MemoryBytes, NanoCPUs: m.cfg.NanoCPUs, CgroupParent: m.cfg.CgroupParent},
		RestartPolicy: container.RestartPolicy{Name: "no"},
	}
	if spec.RenderMode == "nvidia" {
		image = m.cfg.NvidiaImage
		env = append(env, "GOSTREAM3D_GPU_BACKEND="+backend, "NEKO_CAPTURE_VIDEO_CODEC=h264")
		env = append(env, videoPipelineEnvironment(gpuVideoPipeline(":99.0", screen, backend))...)
		if backend == "nvidia" {
			hostConfig.DeviceRequests = gpuConfig.DeviceRequests
			hostConfig.Devices = gpuConfig.Devices
			hostConfig.Mounts = gpuConfig.Mounts
			env = append(env, gpuEnv...)
		} else {
			hostConfig.Devices = []container.DeviceMapping{{PathOnHost: renderDevice, PathInContainer: renderDevice, CgroupPermissions: "rwm"}}
			env = append(env, "LIBVA_DRIVER_NAME=iHD")
		}
	}
	created, err := m.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: "gostream3d-" + token,
		Config: &container.Config{
			Image:        image,
			Env:          env,
			ExposedPorts: exposed,
			Labels:       map[string]string{"gostream3d.managed": "true", "gostream3d.token": token},
		},
		HostConfig: hostConfig,
	})
	if err != nil {
		lease.Release()
		return Worker{}, fmt.Errorf("create Neko container: %w", err)
	}

	var depthControl io.Closer
	cleanup := func(startErr error) (Worker, error) {
		if depthControl != nil {
			_ = depthControl.Close()
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, removeErr := m.client.ContainerRemove(cleanupCtx, created.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
		if removeErr != nil && !errdefs.IsNotFound(removeErr) {
			m.mu.Lock()
			m.orphan[token] = managedWorker{Worker: Worker{Token: token, ContainerID: created.ID}, lease: lease}
			m.mu.Unlock()
			return Worker{}, errors.Join(startErr, fmt.Errorf("rollback container %s: %w; ports retained", created.ID, removeErr))
		}
		lease.Release()
		return Worker{}, startErr
	}
	if _, err := m.client.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return cleanup(fmt.Errorf("start Neko container: %w", err))
	}

	baseURL := "http://" + net.JoinHostPort(m.cfg.HTTPBindIP, strconv.Itoa(lease.HTTPPort))
	readyCtx, cancelReady := context.WithTimeout(ctx, m.cfg.ReadyTimeout)
	defer cancelReady()
	if err := waitForNeko(readyCtx, baseURL, m.cfg.ReadyTimeout); err != nil {
		return cleanup(fmt.Errorf("Neko readiness: %w", err))
	}
	if depthProfile {
		var err error
		depthControl, err = initializeGPUPage(readyCtx, m.cfg.HTTPBindIP, lease.CDPPort, targetURL)
		if err != nil {
			return cleanup(fmt.Errorf("GPU depth profile: %w", err))
		}
	}
	if err := waitForPage(readyCtx, m.cfg.HTTPBindIP, lease.CDPPort, spec.ReadySelector, m.cfg.ReadyTimeout); err != nil {
		return cleanup(fmt.Errorf("Chromium page readiness: %w", err))
	}
	renderer := ""
	if spec.RenderMode == "nvidia" {
		var err error
		renderer, err = verifyHardwareGPU(readyCtx, m.cfg.HTTPBindIP, lease.CDPPort)
		if err != nil {
			return cleanup(err)
		}
	}
	worker := Worker{
		Token: token, ContainerID: created.ID, HTTPPort: lease.HTTPPort, CDPPort: lease.CDPPort,
		UDPStart: lease.UDPStart, UDPEnd: lease.UDPEnd,
		SignalURL: baseURL, StartedAt: time.Now().UTC(), Screen: screen, RenderMode: spec.RenderMode, Adaptive: spec.Adaptive,
		GPUBackend: backend, GPURenderer: renderer,
	}
	m.mu.Lock()
	m.active[token] = managedWorker{Worker: worker, lease: lease, depthControl: depthControl}
	m.mu.Unlock()
	return worker, nil
}

func waitForNeko(parent context.Context, baseURL string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	httpClient := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/", nil)
		if err != nil {
			return err
		}
		resp, err := httpClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Stop removes the worker before releasing its ports. It is safe to call more than once.
func (m *DockerManager) Stop(ctx context.Context, token string) error {
	for {
		m.mu.Lock()
		ch, starting := m.starting[token]
		if !starting {
			break
		}
		m.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer m.mu.Unlock()
	worker, ok := m.active[token]
	if !ok {
		worker, ok = m.orphan[token]
		if !ok {
			return nil
		}
	}
	stopTimeout := 5
	_, stopErr := m.client.ContainerStop(ctx, worker.ContainerID, client.ContainerStopOptions{Timeout: &stopTimeout})
	_, err := m.client.ContainerRemove(ctx, worker.ContainerID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	if err != nil && !errdefs.IsNotFound(err) {
		if stopErr != nil && !errdefs.IsNotFound(stopErr) {
			return errors.Join(fmt.Errorf("stop Neko container %s: %w", worker.ContainerID, stopErr), fmt.Errorf("remove Neko container %s: %w", worker.ContainerID, err))
		}
		return fmt.Errorf("remove Neko container %s: %w", worker.ContainerID, err)
	}
	delete(m.active, token)
	delete(m.orphan, token)
	if worker.depthControl != nil {
		_ = worker.depthControl.Close()
	}
	worker.lease.Release()
	return nil
}

func (m *DockerManager) Get(token string) (Worker, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	worker, ok := m.active[token]
	return worker.Worker, ok
}

// CleanupStale removes workers left by a previous gateway process. Call only
// during startup of a single gateway that owns this Docker host's workers.
func (m *DockerManager) CleanupStale(ctx context.Context) error {
	filters := make(client.Filters).Add("label", "gostream3d.managed=true")
	listed, err := m.client.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return fmt.Errorf("list stale workers: %w", err)
	}
	var allErr error
	for _, item := range listed.Items {
		if item.Labels["gostream3d.managed"] != "true" {
			continue
		}
		_, err := m.client.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
		if err != nil && !errdefs.IsNotFound(err) {
			allErr = errors.Join(allErr, fmt.Errorf("remove stale worker %s: %w", item.ID, err))
		}
	}
	return allErr
}
