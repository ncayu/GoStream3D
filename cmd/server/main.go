package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/moby/moby/client"
	"gostream3d/internal/api"
	"gostream3d/internal/appearance"
	"gostream3d/internal/config"
	"gostream3d/internal/cpulimit"
	"gostream3d/internal/linuxsetup"
	"gostream3d/internal/monitor"
	"gostream3d/internal/orchestrator"
	"gostream3d/internal/proxy"
)

var version = "dev"

func main() {
	configPath := flag.String("config", platformDefaultConfigPath(), "configuration file path")
	showVersion := flag.Bool("version", false, "print version and exit")
	initConfig := flag.Bool("init-config", false, "initialize Linux configuration without starting the server")
	setupRuntime := flag.String("setup-runtime", "native", "initial configuration runtime: native or docker")
	publicIP := flag.String("public-ip", "", "viewer-reachable IP for newly initialized Linux configuration")
	doctor := flag.Bool("doctor", false, "check configuration and runtime dependencies without starting workers")
	flag.Parse()
	if *showVersion {
		fmt.Println("GoStream3D " + version)
		return
	}
	logOutput, closeLog, logPath := platformLogOutput()
	defer closeLog()
	logger := slog.New(slog.NewTextHandler(logOutput, nil))
	fail := func(message string, err error) {
		logger.Error(message, "error", err)
		platformReportError(message, err, logPath)
		closeLog()
		os.Exit(1)
	}
	explicitConfig := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			explicitConfig = true
		}
	})
	if *initConfig {
		if runtime.GOOS != "linux" {
			fail("configuration initialization failed", errors.New("--init-config is supported on Linux"))
		}
		if err := linuxsetup.Create(*configPath, *setupRuntime, *publicIP); err != nil {
			fail("configuration initialization failed", err)
		}
		fmt.Println("Configuration ready:", *configPath, "(existing credentials preserved)")
		return
	}
	path, err := platformPrepareConfig(*configPath, explicitConfig)
	if err != nil {
		fail("startup configuration failed", err)
	}
	if err := config.LoadFile(path); err != nil {
		fail("configuration failed", err)
	}
	if *doctor {
		if err := checkDeployment(); err != nil {
			fail("deployment check failed", err)
		}
		fmt.Println("Configuration and runtime dependencies are ready. Verify video and input with a test project.")
		return
	}
	if err := run(logger); err != nil {
		fail("server stopped", err)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := platformResolveDataPaths(&cfg); err != nil {
		return err
	}
	var cpuControl *cpulimit.Service
	if runtime.GOOS == "windows" && cfg.Runtime == "native" {
		cpuControl, err = cpulimit.New(filepath.Join(filepath.Dir(cfg.SessionStore), "cpu-settings.json"), cfg.CPUPercent, applyCPULimit)
		if err != nil {
			return fmt.Errorf("CPU protection settings could not be restored or applied: %w", err)
		}
		cfg.CPUPercent = cpuControl.Settings().Percent
	} else if runtime.GOOS == "linux" && cfg.Runtime != "remote" && os.Getenv("GOSTREAM3D_CPU_CGROUP_PATH") != "" {
		controller, limitErr := cpulimit.NewCgroupController(os.Getenv("GOSTREAM3D_CPU_CGROUP_PATH"), runtime.NumCPU())
		if limitErr != nil {
			return limitErr
		}
		cpuControl, err = cpulimit.New(filepath.Join(filepath.Dir(cfg.SessionStore), "cpu-settings.json"), cfg.CPUPercent, controller.Apply)
		if err != nil {
			return fmt.Errorf("Linux CPU settings could not be restored: %w", err)
		}
		cfg.CPUPercent = cpuControl.Settings().Percent
	} else if err := applyCPULimit(cfg.CPUPercent); err != nil {
		return fmt.Errorf("CPU hard cap could not be applied: %w", err)
	}
	if cfg.Runtime == "remote" {
		return runRemote(cfg, logger)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	appearanceStore, err := appearance.Open(filepath.Join(filepath.Dir(cfg.SessionStore), "appearance.sqlite"))
	if err != nil {
		return fmt.Errorf("console appearance database could not be opened: %w", err)
	}
	defer appearanceStore.Close()
	ports, err := orchestrator.NewPortAllocator(cfg.HTTPPorts, cfg.CDPPorts, cfg.UDPPorts, cfg.UDPPerWorker)
	if err != nil {
		return err
	}
	channelControl, err := orchestrator.NewChannelSettings(filepath.Join(filepath.Dir(cfg.SessionStore), "channel-settings.json"), ports)
	if err != nil {
		return fmt.Errorf("render channel settings could not be restored: %w", err)
	}
	var workers orchestrator.WorkerController
	if cfg.Runtime == "docker" {
		dockerClient, err := orchestrator.NewDockerClientFromEnv()
		if err != nil {
			return err
		}
		defer dockerClient.Close()
		checkCtx, cancelCheck := context.WithTimeout(context.Background(), 5*time.Second)
		_, err = dockerClient.Ping(checkCtx, client.PingOptions{})
		cancelCheck()
		if err != nil {
			return err
		}
		dockerWorkers, err := orchestrator.NewDockerManager(dockerClient, ports, cfg.Docker)
		if err != nil {
			return err
		}
		staleCtx, cancelStale := context.WithTimeout(context.Background(), 30*time.Second)
		err = dockerWorkers.CleanupStale(staleCtx)
		cancelStale()
		if err != nil {
			return err
		}
		workers = dockerWorkers
	} else {
		workers, err = orchestrator.NewNativeManager(ports, cfg.Native)
		if err != nil {
			return err
		}
	}
	sessions, err := orchestrator.NewSessionManager(workers, cfg.IdleTimeout)
	if err != nil {
		return err
	}
	sessions.SetKeepPreloaded(cfg.KeepPreloaded)
	sessions.SetPreloadContext(ctx, cfg.Native.ReadyTimeout+10*time.Second)
	sessions.SetChannelCapacitySource(ports.Capacity)
	sessions.SetChannelAvailabilitySource(ports.Available)
	performanceControl, err := orchestrator.NewPerformanceSettings(filepath.Join(filepath.Dir(cfg.SessionStore), "performance-settings.json"))
	if err != nil {
		return fmt.Errorf("performance settings could not be restored: %w", err)
	}
	sessions.SetPerformanceSettingsSource(performanceControl.Settings)
	if local, ok := workers.(interface {
		SetPerformanceSettingsSource(func() orchestrator.PerformanceSettings)
	}); ok {
		local.SetPerformanceSettingsSource(performanceControl.Settings)
	}
	monitorService, monitorErr := monitor.NewWithSettings(filepath.Join(filepath.Dir(cfg.SessionStore), "monitor-settings.json"))
	if monitorErr != nil {
		logger.Warn("monitor settings could not be loaded; using 30-second default", "error", monitorErr)
	}
	monitorService.Sample(ctx)
	go monitorService.Run(ctx)
	if err := sessions.EnableStore(cfg.SessionStore); err != nil {
		return err
	}
	if state := sessions.StoreStatus(); len(state.Issues) > 0 {
		logger.Warn("project data needs recovery; valid links remain available", "issues", state.Issues, "archive", state.Archive, "write_blocked", state.WriteBlocked)
	}
	var capacityRunner orchestrator.CapacityRunner
	if local, ok := workers.(orchestrator.CapacityRunner); ok {
		capacityRunner = local
	}
	capacity := orchestrator.NewCapacityService(capacityRunner, ports.Available, performanceControl.Settings, filepath.Join(filepath.Dir(cfg.SessionStore), "capacity-report.json"))
	capacity.SetCPUPercentSource(func() int {
		if cpuControl != nil {
			return cpuControl.Settings().Percent
		}
		return cfg.CPUPercent
	})
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := capacity.Close(closeCtx); err != nil {
			logger.Error("capacity test cleanup failed", "error", err)
		}
	}()
	var wsProxy api.SignalProxy
	transport := "webrtc"
	if local, ok := workers.(interface {
		api.SignalProxy
		Transport() string
	}); ok {
		wsProxy = local
		transport = local.Transport()
	} else {
		wsProxy, err = proxy.NewWebSocketProxy(cfg.Docker.UserPassword, cfg.Docker.AdminPassword)
		if err != nil {
			return err
		}
	}
	router, err := api.NewRouter(api.RouterConfig{
		AdminAPIKey: cfg.AdminAPIKey, AdminUsername: cfg.AdminUsername, AdminPassword: cfg.AdminPassword,
		AdminCredentialsPath: filepath.Join(filepath.Dir(cfg.SessionStore), "admin-account.json"),
		PublicBaseURL:        cfg.PublicBaseURL,
		WebDist:              cfg.WebDist, Sessions: sessions, Proxy: wsProxy, Transport: transport, CPUPercent: cfg.CPUPercent, Monitor: monitorService,
		CPUControl:         cpuControl,
		ChannelControl:     channelControl,
		PerformanceControl: performanceControl,
		Capacity:           capacity,
		Appearance:         appearanceStore,
		PreloadContext:     ctx, PreloadTimeout: cfg.Native.ReadyTimeout + 10*time.Second, Logger: logger,
	})
	if err != nil {
		return err
	}
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: router, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()
	logger.Info("GoStream3D listening", "address", cfg.HTTPAddr)
	maintenanceDone := make(chan struct{})
	go func() {
		defer close(maintenanceDone)
		if err := sessions.PreloadSaved(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("some saved pages could not be preloaded; links remain available for retry", "error", err)
		}
		sessions.RunReaper(ctx, 10*time.Second, cfg.Native.ReadyTimeout+10*time.Second, logger)
	}()
	platformOpenBrowser(cfg.PublicBaseURL + "/admin")
	var serveErr error
	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = err
		}
	}
	stop()
	<-maintenanceDone
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	workersErr := sessions.StopAll(shutdownCtx)
	return errors.Join(serveErr, shutdownErr, workersErr)
}

func runRemote(cfg config.Config, logger *slog.Logger) error {
	handler, err := newRemoteGatewayHandler(cfg, logger)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()
	logger.Info("GoStream3D remote gateway listening", "address", cfg.HTTPAddr, "upstream", cfg.RemoteBaseURL)
	platformOpenBrowser(cfg.PublicBaseURL + "/admin")
	var serveErr error
	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return errors.Join(serveErr, server.Shutdown(shutdownCtx))
}

func newRemoteGatewayHandler(cfg config.Config, logger *slog.Logger) (http.Handler, error) {
	remoteHandler, err := proxy.NewRemoteHandler(cfg.RemoteBaseURL, logger)
	if err != nil {
		return nil, err
	}
	adminUI, err := api.NewAdminUIHandler(cfg.WebDist)
	if err != nil {
		return nil, err
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/admin" || r.URL.Path == "/admin/" || strings.HasPrefix(r.URL.Path, "/assets/") {
			adminUI.ServeHTTP(w, r)
			return
		}
		remoteHandler.ServeHTTP(w, r)
	})
	return handler, nil
}
