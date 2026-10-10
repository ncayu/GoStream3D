//go:build linux

package orchestrator

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed xorg.conf
var xorgTemplate string

type nativeProcess struct {
	name string
	cmd  *exec.Cmd
	done chan error
}

type nativeWorker struct {
	Worker
	lease        PortLease
	dir          string
	processes    []*nativeProcess
	depthControl io.Closer
}

type ProcessManager struct {
	ports    *PortAllocator
	cfg      NativeConfig
	mu       sync.Mutex
	active   map[string]*nativeWorker
	starting map[string]chan struct{}
}

func NewNativeManager(ports *PortAllocator, cfg NativeConfig) (WorkerController, error) {
	if ports == nil {
		return nil, errors.New("port allocator is required")
	}
	var err error
	for _, item := range []struct {
		label string
		path  *string
	}{
		{"neko", &cfg.NekoBinary}, {"chromium", &cfg.ChromiumBinary},
		{"Xorg", &cfg.XorgBinary}, {"pulseaudio", &cfg.PulseBinary},
	} {
		if *item.path == "" {
			*item.path = item.label
		}
		*item.path, err = exec.LookPath(*item.path)
		if err != nil {
			return nil, fmt.Errorf("find %s: %w", item.label, err)
		}
		*item.path, err = filepath.Abs(*item.path)
		if err != nil {
			return nil, err
		}
	}
	if cfg.VirtualGLBinary == "" {
		cfg.VirtualGLBinary = "vglrun"
	}
	if cfg.WorkRoot == "" {
		cfg.WorkRoot = filepath.Join(os.TempDir(), "gostream3d-workers")
	}
	if cfg.WebDist != "" {
		cfg.WebDist, err = filepath.Abs(cfg.WebDist)
		if err != nil {
			return nil, err
		}
		if info, statErr := os.Stat(cfg.WebDist); statErr != nil || !info.IsDir() {
			cfg.WebDist = ""
		}
	}
	cfg.WorkRoot, err = filepath.Abs(cfg.WorkRoot)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.WorkRoot, 0700); err != nil {
		return nil, fmt.Errorf("create worker directory: %w", err)
	}
	if cfg.UserPassword == "" || cfg.AdminPassword == "" || cfg.UserPassword == cfg.AdminPassword {
		return nil, errors.New("distinct Neko passwords are required")
	}
	if strings.ContainsAny(cfg.UserPassword+cfg.AdminPassword, "\r\n\x00") {
		return nil, errors.New("invalid Neko password")
	}
	if cfg.ICEPublicIP != "" && net.ParseIP(cfg.ICEPublicIP) == nil {
		return nil, errors.New("ICEPublicIP must be an IP address")
	}
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = 120 * time.Second
	}
	return &ProcessManager{ports: ports, cfg: cfg, active: make(map[string]*nativeWorker), starting: make(map[string]chan struct{})}, nil
}

func (m *ProcessManager) StartWorker(ctx context.Context, spec WorkerSpec) (Worker, error) {
	if !tokenPattern.MatchString(spec.Token) {
		return Worker{}, errors.New("invalid session token")
	}
	if err := ValidateTarget(spec.TargetURL); err != nil {
		return Worker{}, err
	}
	if !screenPattern.MatchString(spec.Screen) {
		return Worker{}, errors.New("invalid screen specification")
	}
	if strings.HasSuffix(spec.Screen, "@90") || strings.HasSuffix(spec.Screen, "@120") {
		return Worker{}, errors.New("Linux Neko supports at most 60 FPS")
	}
	if err := validateReadySelector(spec.ReadySelector); err != nil {
		return Worker{}, err
	}
	if spec.RenderMode != "cpu" && spec.RenderMode != "nvidia" {
		return Worker{}, errors.New("invalid render mode")
	}
	for {
		m.mu.Lock()
		if existing, ok := m.active[spec.Token]; ok {
			if existing.alive() {
				worker := existing.Worker
				m.mu.Unlock()
				return worker, nil
			}
			m.mu.Unlock()
			if err := m.Stop(ctx, spec.Token); err != nil {
				return Worker{}, err
			}
			continue
		}
		if ch, ok := m.starting[spec.Token]; ok {
			m.mu.Unlock()
			select {
			case <-ch:
				continue
			case <-ctx.Done():
				return Worker{}, ctx.Err()
			}
		}
		m.starting[spec.Token] = make(chan struct{})
		m.mu.Unlock()
		break
	}
	defer func() { m.mu.Lock(); close(m.starting[spec.Token]); delete(m.starting, spec.Token); m.mu.Unlock() }()
	lease, err := m.ports.Allocate()
	if err != nil {
		return Worker{}, err
	}
	dir, err := os.MkdirTemp(m.cfg.WorkRoot, "worker-"+spec.Token+"-")
	if err != nil {
		lease.Release()
		return Worker{}, err
	}
	display := 100 + lease.HTTPPort - m.ports.http.Start
	inputSocket := filepath.Join(dir, "input.sock")
	pulseSocket := filepath.Join(dir, "pulse.sock")
	xorgConfig := strings.Replace(xorgTemplate, "/tmp/xf86-input-neko.sock", inputSocket, 1)
	if err := os.WriteFile(filepath.Join(dir, "xorg.conf"), []byte(xorgConfig), 0600); err != nil {
		lease.Release()
		os.RemoveAll(dir)
		return Worker{}, err
	}
	pulseConfig := "load-module module-null-sink sink_name=audio_output\n" +
		"load-module module-null-sink sink_name=audio_input\n" +
		"load-module module-virtual-source source_name=microphone master=audio_input.monitor\n" +
		"load-module module-native-protocol-unix socket=" + pulseSocket + "\n" +
		"load-module module-always-sink\n"
	if err := os.WriteFile(filepath.Join(dir, "default.pa"), []byte(pulseConfig), 0600); err != nil {
		lease.Release()
		os.RemoveAll(dir)
		return Worker{}, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "xdg"), 0700); err != nil {
		lease.Release()
		os.RemoveAll(dir)
		return Worker{}, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "chromium-profile"), 0700); err != nil {
		lease.Release()
		os.RemoveAll(dir)
		return Worker{}, err
	}
	w := &nativeWorker{lease: lease, dir: dir}
	cleanup := func(cause error) (Worker, error) {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stopErr := w.stop(stopCtx)
		lease.Release()
		removeErr := os.RemoveAll(dir)
		return Worker{}, errors.Join(cause, stopErr, removeErr)
	}
	env := append(os.Environ(),
		"DISPLAY=:"+strconv.Itoa(display),
		"XDG_RUNTIME_DIR="+filepath.Join(dir, "xdg"),
		"PULSE_SERVER=unix:"+pulseSocket,
		"NEKO_DESKTOP_INPUT_SOCKET="+inputSocket,
		"NEKO_LEGACY=true",
		"NEKO_PASSWORD="+m.cfg.UserPassword,
		"NEKO_PASSWORD_ADMIN="+m.cfg.AdminPassword,
		"NEKO_IMPLICIT_CONTROL=true",
		"NEKO_CONTROL_PROTECTION=false",
		"NEKO_SCREEN="+spec.Screen,
		"NEKO_EPR="+strconv.Itoa(lease.UDPStart)+"-"+strconv.Itoa(lease.UDPEnd),
		"NEKO_ICELITE=true",
		"NEKO_SERVER_BIND=127.0.0.1:"+strconv.Itoa(lease.HTTPPort),
		"NEKO_FILE_TRANSFER_ENABLED=false",
	)
	if m.cfg.ICEPublicIP != "" {
		env = append(env, "NEKO_NAT1TO1="+m.cfg.ICEPublicIP)
	}
	if spec.RenderMode == "cpu" {
		env = append(env, videoPipelineEnvironment(cpuVideoPipeline(":"+strconv.Itoa(display), spec.Screen))...)
	}
	graphicsOpenGL := false
	if spec.RenderMode == "nvidia" {
		_, driverErr := exec.LookPath("nvidia-smi")
		if driverErr != nil {
			_, driverErr = os.Stat("/usr/lib/wsl/lib/nvidia-smi")
		}
		backend, _, err := resolveGPUBackend(m.cfg.GPUBackend, "/", m.cfg.GPURenderDevice, driverErr == nil)
		if err != nil {
			return cleanup(err)
		}
		w.GPUBackend = backend
		env = append(env, "NEKO_CAPTURE_VIDEO_CODEC=h264")
		env = append(env, videoPipelineEnvironment(gpuVideoPipeline(":"+strconv.Itoa(display), spec.Screen, backend))...)
		if backend == "intel" {
			env = append(env, "LIBVA_DRIVER_NAME=iHD")
		} else {
			graphicsEnv, wsl, err := nvidiaGraphicsEnvironment("/")
			if err != nil {
				return cleanup(err)
			}
			graphicsOpenGL = wsl
			env = append(env, graphicsEnv...)
		}
	}
	start := func(name, path string, args ...string) error {
		process, err := startNativeProcess(dir, env, name, path, args...)
		if err != nil {
			return err
		}
		w.processes = append(w.processes, process)
		return nil
	}
	if err := start("xorg", m.cfg.XorgBinary, ":"+strconv.Itoa(display), "-config", filepath.Join(dir, "xorg.conf"), "-noreset", "-nolisten", "tcp"); err != nil {
		return cleanup(err)
	}
	if err := waitForSocket(ctx, filepath.Join("/tmp/.X11-unix", "X"+strconv.Itoa(display)), 15*time.Second, w.processes[len(w.processes)-1]); err != nil {
		return cleanup(fmt.Errorf("Xorg display: %w", err))
	}
	if err := start("pulseaudio", m.cfg.PulseBinary, "--daemonize=no", "--exit-idle-time=-1", "-n", "-F", filepath.Join(dir, "default.pa")); err != nil {
		return cleanup(err)
	}
	if err := waitForSocket(ctx, pulseSocket, 10*time.Second, w.processes[len(w.processes)-1]); err != nil {
		return cleanup(fmt.Errorf("PulseAudio: %w", err))
	}
	nekoArgs := []string{"serve"}
	if m.cfg.WebDist != "" {
		nekoArgs = append(nekoArgs, "--server.static", m.cfg.WebDist)
	}
	if err := start("neko", m.cfg.NekoBinary, nekoArgs...); err != nil {
		return cleanup(err)
	}
	chromiumArgs := []string{"--window-position=0,0", "--display=:" + strconv.Itoa(display),
		"--user-data-dir=" + filepath.Join(dir, "chromium-profile"), "--no-first-run", "--start-maximized", "--kiosk", "--disable-dev-shm-usage",
		"--remote-debugging-address=127.0.0.1", "--remote-debugging-port=" + strconv.Itoa(lease.CDPPort),
		"--ignore-gpu-blocklist", "--enable-webgl"}
	path := m.cfg.ChromiumBinary
	if spec.RenderMode == "cpu" {
		chromiumArgs = append(chromiumArgs, "--use-gl=angle", "--use-angle=swiftshader", "--enable-unsafe-swiftshader")
	} else if graphicsOpenGL {
		chromiumArgs = append(chromiumArgs, "--use-gl=angle", "--use-angle=gl", "--disable-software-rasterizer")
	} else {
		chromiumArgs = append(chromiumArgs, "--use-gl=angle", "--enable-features=Vulkan", "--use-angle=vulkan", "--disable-vulkan-surface", "--disable-software-rasterizer")
	}
	depthProfile := gpuDepthProfileRequired(spec.RenderMode, spec.TargetURL)
	initialURL := spec.TargetURL
	if depthProfile {
		initialURL = "about:blank"
	}
	chromiumArgs = append(chromiumArgs, "--app="+initialURL)
	if err := start("chromium", path, chromiumArgs...); err != nil {
		return cleanup(err)
	}
	readyCtx, cancelReady := context.WithTimeout(ctx, m.cfg.ReadyTimeout)
	defer cancelReady()
	probeCtx, cancelProbe := context.WithCancel(readyCtx)
	defer cancelProbe()
	failures := make(chan error, len(w.processes))
	for _, process := range w.processes {
		go func(p *nativeProcess) {
			select {
			case processErr := <-p.done:
				failures <- fmt.Errorf("%s exited: %v (see %s.log)", p.name, processErr, p.name)
				cancelProbe()
			case <-probeCtx.Done():
			}
		}(process)
	}
	baseURL := "http://127.0.0.1:" + strconv.Itoa(lease.HTTPPort)
	if err := waitForNeko(probeCtx, baseURL, m.cfg.ReadyTimeout); err != nil {
		return cleanup(fmt.Errorf("Neko readiness: %w", errors.Join(err, firstFailure(failures))))
	}
	if depthProfile {
		var err error
		w.depthControl, err = initializeGPUPage(probeCtx, "127.0.0.1", lease.CDPPort, spec.TargetURL)
		if err != nil {
			return cleanup(fmt.Errorf("GPU depth profile: %w", err))
		}
	}
	if err := waitForPage(probeCtx, "127.0.0.1", lease.CDPPort, spec.ReadySelector, m.cfg.ReadyTimeout); err != nil {
		return cleanup(fmt.Errorf("Chromium readiness: %w", errors.Join(err, firstFailure(failures))))
	}
	if spec.RenderMode == "nvidia" {
		renderer, err := verifyHardwareGPU(probeCtx, "127.0.0.1", lease.CDPPort)
		if err != nil {
			return cleanup(err)
		}
		w.GPURenderer = renderer
	}
	w.Worker = Worker{Token: spec.Token, HTTPPort: lease.HTTPPort, CDPPort: lease.CDPPort,
		UDPStart: lease.UDPStart, UDPEnd: lease.UDPEnd, SignalURL: baseURL, StartedAt: time.Now().UTC(), Screen: spec.Screen, RenderMode: spec.RenderMode, Adaptive: spec.Adaptive, GPUBackend: w.GPUBackend, GPURenderer: w.GPURenderer}
	m.mu.Lock()
	m.active[spec.Token] = w
	m.mu.Unlock()
	return w.Worker, nil
}

func startNativeProcess(dir string, env []string, name, path string, args ...string) (*nativeProcess, error) {
	logFile, err := os.OpenFile(filepath.Join(dir, name+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	process := &nativeProcess{name: name, cmd: cmd, done: make(chan error, 1)}
	go func() { process.done <- cmd.Wait(); close(process.done); logFile.Close() }()
	return process, nil
}

func waitForSocket(parent context.Context, path string, timeout time.Duration, process *nativeProcess) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-process.done:
			return fmt.Errorf("%s exited before socket was ready: %v", process.name, err)
		case <-ticker.C:
		}
	}
}

func firstFailure(failures <-chan error) error {
	select {
	case err := <-failures:
		return err
	default:
		return nil
	}
}

func (w *nativeWorker) alive() bool {
	for _, process := range w.processes {
		select {
		case <-process.done:
			return false
		default:
		}
	}
	return len(w.processes) > 0
}

func (w *nativeWorker) stop(ctx context.Context) error {
	if w.depthControl != nil {
		_ = w.depthControl.Close()
	}
	for i := len(w.processes) - 1; i >= 0; i-- {
		_ = syscall.Kill(-w.processes[i].cmd.Process.Pid, syscall.SIGTERM)
	}
	var allErr error
	for i := len(w.processes) - 1; i >= 0; i-- {
		process := w.processes[i]
		select {
		case <-process.done:
		case <-ctx.Done():
			_ = syscall.Kill(-process.cmd.Process.Pid, syscall.SIGKILL)
			<-process.done
			allErr = errors.Join(allErr, ctx.Err())
		}
	}
	return allErr
}

func (m *ProcessManager) Stop(ctx context.Context, token string) error {
	for {
		m.mu.Lock()
		if ch, starting := m.starting[token]; starting {
			m.mu.Unlock()
			select {
			case <-ch:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		w, ok := m.active[token]
		if !ok {
			m.mu.Unlock()
			return nil
		}
		delete(m.active, token)
		m.mu.Unlock()
		stopErr := w.stop(ctx)
		if stopErr != nil {
			m.mu.Lock()
			m.active[token] = w
			m.mu.Unlock()
			return stopErr
		}
		w.lease.Release()
		if err := os.RemoveAll(w.dir); err != nil {
			return fmt.Errorf("remove worker files: %w", err)
		}
		return nil
	}
}
