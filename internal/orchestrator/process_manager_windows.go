//go:build windows

package orchestrator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/sys/windows"
	"gostream3d/internal/viewport"
)

// WindowsProcessManager renders each page with a headless browser running on
// this Windows host. JPEG screencast is an initial local transport; it does
// not use Neko, a Linux node, or Docker.
type WindowsProcessManager struct {
	ports             *PortAllocator
	cfg               NativeConfig
	browser           string
	mu                sync.Mutex
	active            map[string]*windowsWorker
	performanceSource func() PerformanceSettings
	budgetMu          sync.Mutex
	budgetAt          time.Time
	budgets           map[string]float64
}

type windowsViewer struct {
	conn        *websocket.Conn
	frames      chan []byte
	visible     bool
	telemetry   bool
	inputID     uint64
	inputFrame  uint64
	nextFrameID uint64
	sentFrames  [256]sentFrameSample
}

type windowsWorker struct {
	Worker
	lease             PortLease
	cmd               *exec.Cmd
	process           chan error
	job               windows.Handle
	cdp               *websocket.Conn
	cdpMu             sync.Mutex
	captureMu         sync.Mutex // Serialize viewport changes with capture start/stop.
	ackMu             sync.Mutex
	nextAck           time.Time
	frameInterval     time.Duration
	lastFrameAt       time.Time
	lastInputAt       time.Time
	lastPulseAt       time.Time
	frameCount        uint64
	mu                sync.Mutex
	closeOne          sync.Once
	stopMu            sync.Mutex
	stopped           bool
	closed            chan struct{}
	latest            []byte
	viewers           map[*windowsViewer]struct{}
	pending           map[int]chan cdpMessage
	nextID            int
	width             int
	height            int
	captureWake       chan struct{}
	captureActive     bool
	viewportSettling  bool // Never publish a drawing buffer cleared by a resize.
	targetURL         string
	readySelector     string
	performance       StreamPerformance
	performanceAt     time.Time
	performanceFrames uint64
	writeSampleAt     time.Time
	writeSampleFrames uint64
	lastWriteAt       time.Time
}

type cdpMessage struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func NewNativeManager(ports *PortAllocator, cfg NativeConfig) (WorkerController, error) {
	if ports == nil {
		return nil, errors.New("port allocator is required")
	}
	browser, err := findWindowsBrowser(cfg.ChromiumBinary)
	if err != nil {
		return nil, err
	}
	if cfg.WorkRoot == "" {
		cfg.WorkRoot = filepath.Join(os.TempDir(), "gostream3d-workers")
	}
	cfg.WorkRoot, err = filepath.Abs(cfg.WorkRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve worker directory: %w", err)
	}
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = 120 * time.Second
	}
	if cfg.CPURenderer == "" {
		cfg.CPURenderer = "swiftshader"
	}
	if cfg.CPURenderer != "swiftshader" && cfg.CPURenderer != "warp" {
		return nil, errors.New("unsupported Windows CPU renderer")
	}
	if cfg.CPUEffects == "" {
		cfg.CPUEffects = "original"
	}
	if cfg.CPUEffects != "original" && cfg.CPUEffects != "simplified" {
		return nil, errors.New("unsupported Windows CPU effects profile")
	}
	if err := os.MkdirAll(cfg.WorkRoot, 0700); err != nil {
		return nil, fmt.Errorf("create worker directory: %w", err)
	}
	return &WindowsProcessManager{
		ports: ports, cfg: cfg, browser: browser, active: make(map[string]*windowsWorker),
	}, nil
}

func findWindowsBrowser(configured string) (string, error) {
	if configured != "" && configured != "chromium" {
		path, err := exec.LookPath(configured)
		if err != nil {
			return "", fmt.Errorf("find configured Chromium %q: %w", configured, err)
		}
		return filepath.Abs(path)
	}
	if path, err := exec.LookPath("chromium"); err == nil {
		return filepath.Abs(path)
	}
	for _, path := range []string{
		filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Google", "Chrome", "Application", "chrome.exe"),
	} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", errors.New("Windows 本机渲染需要 Chromium、Microsoft Edge 或 Google Chrome；可设置 GOSTREAM3D_CHROMIUM_BIN")
}

func (m *WindowsProcessManager) Transport() string { return "jpeg" }

func (m *WindowsProcessManager) StartWorker(ctx context.Context, spec WorkerSpec) (Worker, error) {
	if err := ctx.Err(); err != nil {
		return Worker{}, err
	}
	if !tokenPattern.MatchString(spec.Token) || !screenPattern.MatchString(spec.Screen) {
		return Worker{}, errors.New("invalid worker token or screen")
	}
	if err := ValidateTarget(spec.TargetURL); err != nil {
		return Worker{}, err
	}
	m.mu.Lock()
	if existing := m.active[spec.Token]; existing != nil {
		select {
		case <-existing.closed:
			delete(m.active, spec.Token)
			m.mu.Unlock()
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			cleanupErr := existing.shutdown(cleanupCtx)
			cancel()
			if cleanupErr != nil {
				return Worker{}, fmt.Errorf("cleanup disconnected local browser: %w", cleanupErr)
			}
		default:
			m.mu.Unlock()
			return existing.Worker, nil
		}
	} else {
		m.mu.Unlock()
	}
	lease, err := m.ports.Allocate()
	if err != nil {
		return Worker{}, err
	}
	started := false
	defer func() {
		if !started {
			lease.Release()
		}
	}()
	dimensions := strings.Split(strings.Split(spec.Screen, "@")[0], "x")
	framesPerSecond, _ := strconv.Atoi(strings.Split(spec.Screen, "@")[1])
	width, _ := strconv.Atoi(dimensions[0])
	height, _ := strconv.Atoi(dimensions[1])
	if width < 640 || width > 3840 || height < 360 || height > 2160 {
		return Worker{}, errors.New("invalid Windows worker dimensions")
	}
	dir := filepath.Join(m.cfg.WorkRoot, spec.Token)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Worker{}, err
	}
	profile := filepath.Join(dir, "browser-profile")
	args := []string{
		"--headless=new", "--no-first-run", "--no-default-browser-check", "--disable-extensions",
		"--disable-background-networking", "--disable-sync", "--hide-scrollbars", "--enable-webgl",
		"--disable-background-timer-throttling", "--disable-renderer-backgrounding",
		"--remote-debugging-address=127.0.0.1", "--remote-debugging-port=" + strconv.Itoa(lease.CDPPort),
		"--user-data-dir=" + profile,
		"--disk-cache-dir=" + filepath.Join(dir, "browser-cache"), "--disk-cache-size=268435456",
		"--window-size=" + strconv.Itoa(width) + "," + strconv.Itoa(height),
	}
	if strings.EqualFold(filepath.Base(m.browser), "msedge.exe") {
		// Edge's compatibility launcher can exit successfully after spawning a
		// replacement browser. Keep the managed process as the actual browser so
		// readiness, shutdown and inherited resource limits track its lifetime.
		args = append(args, "--edge-skip-compat-layer-relaunch")
	}
	if spec.RenderMode == "cpu" {
		args = append(args, "--use-gl=angle", "--ignore-gpu-blocklist")
		if m.cfg.CPURenderer == "warp" {
			args = append(args, "--use-angle=d3d11-warp", "--enable-features=AllowD3D11WarpFallback")
		} else {
			args = append(args, "--use-angle=swiftshader")
		}
	} else if spec.RenderMode != "nvidia" {
		return Worker{}, errors.New("unsupported render mode")
	}
	// Attach and install CPU context options before source scripts create WebGL.
	args = append(args, "about:blank")
	cmd := exec.Command(m.browser, args...)
	logFile, err := os.OpenFile(filepath.Join(dir, "chromium.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return Worker{}, err
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	job, err := startWindowsBrowser(cmd)
	if err != nil {
		logFile.Close()
		return Worker{}, fmt.Errorf("start local browser: %w", err)
	}
	process := make(chan error, 1)
	go func() { err := cmd.Wait(); _ = logFile.Close(); process <- err; close(process) }()
	startupCtx, cancel := context.WithTimeout(ctx, m.cfg.ReadyTimeout)
	defer cancel()
	endpoint, err := waitCDPEndpoint(startupCtx, lease.CDPPort, process)
	if err != nil {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		cleanupErr := stopWindowsBrowserJob(cleanupCtx, job)
		cancelCleanup()
		if cleanupErr != nil {
			_ = windows.CloseHandle(job) // KILL_ON_JOB_CLOSE remains a final safeguard.
		}
		<-process
		return Worker{}, errors.Join(fmt.Errorf("%w (browser=%q, CDP port=%d, log=%q)", err, m.browser, lease.CDPPort, logFile.Name()), cleanupErr)
	}
	cdp, _, err := websocket.DefaultDialer.DialContext(startupCtx, endpoint, nil)
	if err != nil {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		cleanupErr := stopWindowsBrowserJob(cleanupCtx, job)
		cancelCleanup()
		if cleanupErr != nil {
			_ = windows.CloseHandle(job)
		}
		<-process
		return Worker{}, errors.Join(fmt.Errorf("connect to local browser: %w (log=%q)", err, logFile.Name()), cleanupErr)
	}
	w := &windowsWorker{
		Worker: Worker{Token: spec.Token, CDPPort: lease.CDPPort, SignalURL: "local-cdp://" + spec.Token,
			StartedAt: time.Now().UTC(), Screen: spec.Screen, RenderMode: spec.RenderMode, Adaptive: spec.Adaptive},
		lease: lease, cmd: cmd, process: process, job: job, cdp: cdp, closed: make(chan struct{}),
		viewers: make(map[*windowsViewer]struct{}), pending: make(map[int]chan cdpMessage),
		width: width, height: height, targetURL: spec.TargetURL, readySelector: spec.ReadySelector,
		captureWake: make(chan struct{}, 1),
	}
	w.performance = StreamPerformance{Token: spec.Token, ProjectToken: spec.ProjectToken, RenderMode: spec.RenderMode, Policy: spec.FramePolicy, TargetFPS: float64(framesPerSecond)}
	if w.performance.Policy == "" {
		w.performance.Policy = "interactive"
	}
	if framesPerSecond > 0 {
		w.frameInterval = time.Second / time.Duration(framesPerSecond)
	}
	go w.readCDP()
	type startupCommand struct {
		method string
		params any
	}
	commands := []startupCommand{
		{"Page.enable", map[string]any{}},
		{"Runtime.enable", map[string]any{}},
		{"Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": height, "deviceScaleFactor": 1, "mobile": false}},
	}
	commands = append(commands, startupCommand{"Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": renderMeterScript}})
	if spec.RenderMode == "cpu" && m.cfg.CPUEffects == "simplified" {
		commands = append(commands, startupCommand{"Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": cpuEffectsScript}})
	}
	if spec.RenderMode == "nvidia" {
		commands = append(commands, startupCommand{"Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": gpuFrameBudgetScript}})
	}
	// Edge may restore a tab from a persistent profile. Navigate explicitly even
	// when the browser ignored the about:blank startup argument.
	commands = append(commands,
		startupCommand{"Page.navigate", map[string]any{"url": spec.TargetURL}},
		startupCommand{"Page.startScreencast", map[string]any{"format": "jpeg", "quality": 75, "maxWidth": width, "maxHeight": height, "maxFramesInFlight": 1}},
	)
	for _, command := range commands {
		_, err := w.call(startupCtx, command.method, command.params)
		// Navigating from about:blank can switch renderer processes. Edge briefly
		// has no active page while the new renderer attaches; retry only that case.
		for command.method == "Page.startScreencast" && err != nil && strings.Contains(err.Error(), "Not attached to an active page") && startupCtx.Err() == nil {
			select {
			case <-startupCtx.Done():
			case <-time.After(100 * time.Millisecond):
			}
			_, err = w.call(startupCtx, command.method, command.params)
		}
		if err != nil {
			cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
			_ = w.shutdown(cleanupCtx)
			cancelCleanup()
			return Worker{}, err
		}
	}
	expression := readinessExpression(spec.TargetURL, spec.ReadySelector)
	if spec.RenderMode == "cpu" && m.cfg.CPUEffects == "simplified" {
		expression += ` && !!window.__g3dCPUEffects && window.__g3dCPUEffects.error === null`
		if parsed, err := url.Parse(spec.TargetURL); err == nil && strings.EqualFold(parsed.Hostname(), "campus.thingjs.com") {
			expression += ` && window.__g3dCPUEffects.thingJS === true`
		}
	}
	readyTicker := time.NewTicker(250 * time.Millisecond)
	defer readyTicker.Stop()
	evaluations, readyResults := 0, 0
	lastEvaluation := ""
	var readyFrameBaseline uint64
	requestedReadyFrame := false
	for {
		result, callErr := w.call(startupCtx, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true})
		evaluations++
		if callErr == nil {
			var eval struct {
				Result struct {
					Value bool `json:"value"`
				} `json:"result"`
			}
			if err := json.Unmarshal(result, &eval); err != nil {
				lastEvaluation = err.Error()
			} else if eval.Result.Value {
				readyResults++
				w.mu.Lock()
				frameCount := w.frameCount
				w.mu.Unlock()
				if !requestedReadyFrame {
					readyFrameBaseline = frameCount
					requestedReadyFrame = true
					_ = w.sendNoReply("Page.stopScreencast", map[string]any{})
					_ = w.sendNoReply("Page.startScreencast", w.screencastParams())
				}
				if frameCount > readyFrameBaseline {
					break
				}
			} else {
				lastEvaluation = "ready condition false"
				requestedReadyFrame = false
			}
		} else {
			lastEvaluation = callErr.Error()
		}
		select {
		case <-startupCtx.Done():
			w.mu.Lock()
			frameReceived := len(w.latest) > 0
			w.mu.Unlock()
			cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
			_ = w.shutdown(cleanupCtx)
			cancelCleanup()
			return Worker{}, fmt.Errorf("local browser did not become ready (evaluations=%d ready=%d frame_received=%t last_evaluation=%q): %w", evaluations, readyResults, frameReceived, lastEvaluation, startupCtx.Err())
		case <-w.closed:
			cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
			_ = w.shutdown(cleanupCtx)
			cancelCleanup()
			return Worker{}, errors.New("local browser capture stopped")
		case <-readyTicker.C:
		}
	}
	m.mu.Lock()
	if existing := m.active[spec.Token]; existing != nil {
		m.mu.Unlock()
		w.shutdown(context.Background())
		return existing.Worker, nil
	}
	m.active[spec.Token] = w
	m.mu.Unlock()
	started = true
	if spec.RenderMode == "cpu" {
		// Stop encoding between preload and the first viewer. Continuous
		// capture is resumed by the loop as soon as a viewer connects.
		if _, err := w.call(startupCtx, "Page.stopScreencast", map[string]any{}); err != nil {
			_ = m.Stop(context.Background(), spec.Token)
			return Worker{}, err
		}
		go w.captureCPUFrames()
	} else {
		// Keep the ready frame for the first viewer without continually
		// encoding an unwatched preloaded scene.
		if _, err := w.call(startupCtx, "Page.stopScreencast", map[string]any{}); err != nil {
			_ = m.Stop(context.Background(), spec.Token)
			return Worker{}, err
		}
		go w.pulseScreencast(m)
	}
	go w.sampleRenderPerformance()
	return w.Worker, nil
}

// Keep continuous capture running while watched. Request a single fallback
// snapshot only when the compositor stops delivering frames; never cancel
// slow software frames with repeated stop/start requests. Retain only the
// latest full-resolution frame and stop encoding when nobody is watching.
func (w *windowsWorker) captureCPUFrames() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		w.captureMu.Lock()
		w.mu.Lock()
		casting := w.captureActive
		watching := w.hasVisibleViewerLocked()
		lastFrame := w.lastFrameAt
		width, height := w.width, w.height
		w.mu.Unlock()
		var method string
		var params map[string]any
		if watching != casting {
			method, params = "Page.stopScreencast", map[string]any{}
			if watching {
				method, params = "Page.startScreencast", w.screencastParams()
			}
		} else if watching && time.Since(lastFrame) > 3*time.Second {
			method, params = "Page.captureScreenshot", map[string]any{
				"format": "jpeg", "quality": 75, "optimizeForSpeed": true,
				"fromSurface": true, "captureBeyondViewport": false,
				"clip": map[string]any{"x": 0, "y": 0, "width": width, "height": height, "scale": 1},
			}
		}
		if method != "" {
			captureAt := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			result, err := w.call(ctx, method, params)
			cancel()
			if err != nil {
				w.captureMu.Unlock()
				// CDP cannot cancel an in-flight capture. Let the session
				// reconnect instead of queuing more requests behind it.
				w.closeAll()
				_ = w.cdp.Close()
				return
			}
			if method == "Page.captureScreenshot" {
				w.mu.Lock()
				w.performance.SnapshotMS = smoothSample(w.performance.SnapshotMS, float64(time.Since(captureAt))/float64(time.Millisecond))
				w.mu.Unlock()
				var frame struct {
					Data string `json:"data"`
				}
				if json.Unmarshal(result, &frame) == nil {
					w.publishFrame(frame.Data)
				}
			} else {
				casting = watching
				w.mu.Lock()
				w.captureActive = casting
				w.mu.Unlock()
			}
		}
		w.captureMu.Unlock()
		select {
		case <-w.closed:
			return
		case <-w.captureWake:
		case <-ticker.C:
		}
	}
}

func readinessExpression(targetURL, selector string) string {
	expression := "document.readyState === 'complete'"
	if selector != "" {
		selectorJSON, _ := json.Marshal(selector)
		expression += " && !!document.querySelector(" + string(selectorJSON) + ")"
	}
	parsed, err := url.Parse(targetURL)
	if err == nil && strings.EqualFold(parsed.Hostname(), "campus.thingjs.com") {
		expression += ` && !!window.app && app.inited === true && !!app.sceneRoot && ` +
			`app.sceneRoot.children.length > 0 && !!app.campusLoader && ` +
			`app.campusLoader.loadingCampus === false && ` +
			`Array.isArray(app.campusLoader.waitingLoadCampuses) && ` +
			`app.campusLoader.waitingLoadCampuses.length === 0 && app._currentFrame > 0`
	}
	return expression
}

func (w *windowsWorker) screencastParams() map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	return map[string]any{"format": "jpeg", "quality": 75, "maxWidth": w.width,
		"maxHeight": w.height, "maxFramesInFlight": 1}
}

// A hidden browser tab cannot display incoming frames. Keep its scene warm,
// but only capture while at least one viewer can see it. Call with w.mu held.
func (w *windowsWorker) hasVisibleViewerLocked() bool {
	for viewer := range w.viewers {
		if viewer.visible {
			return true
		}
	}
	return false
}

// Preserve the requested viewport and full frame rate while somebody is
// interacting. A preloaded or passively watched page can draw less often so
// three independent WebGL scenes do not burn the GPU before input arrives.
const gpuFrameBudgetScript = `(() => {
  if (window.__g3dSetFrameRate) return;
  const nativeRequest = window.requestAnimationFrame.bind(window);
  const nativeCancel = window.cancelAnimationFrame.bind(window);
  const pending = new Map();
  let nextId = 0, interval = 0, lastFrame = 0;
  function schedule(id, task) {
    const wait = Math.max(0, interval - (performance.now() - lastFrame));
    if (wait > 2) {
      task.timer = setTimeout(() => { task.timer = 0; task.raf = nativeRequest(ts => fire(id, task, ts)); }, wait);
    } else {
      task.raf = nativeRequest(ts => fire(id, task, ts));
    }
  }
  function fire(id, task, ts) {
    if (!pending.has(id)) return;
    pending.delete(id);
    lastFrame = ts;
    task.callback(ts);
  }
  window.requestAnimationFrame = callback => {
    const id = ++nextId, task = {callback, timer: 0, raf: 0};
    pending.set(id, task);
    schedule(id, task);
    return id;
  };
  window.cancelAnimationFrame = id => {
    const task = pending.get(id);
    if (!task) return;
    pending.delete(id);
    if (task.timer) clearTimeout(task.timer);
    if (task.raf) nativeCancel(task.raf);
  };
  window.__g3dSetFrameRate = fps => {
    window.__g3dFrameRate = fps;
    interval = fps > 0 ? 1000 / fps : 0;
    for (const [id, task] of pending) {
      if (!task.timer) continue;
      clearTimeout(task.timer);
      task.timer = 0;
      schedule(id, task);
    }
  };
})();`

// WebGL can update its canvas without Chrome reporting compositor damage.
// Restart a stalled screencast only after a real capture gap. Restarting every
// 250 ms during pointer input interrupts Chrome's capture pipeline and can
// starve older independent GPU sessions while a newer session is active.
func (w *windowsWorker) pulseScreencast(manager *WindowsProcessManager) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	budget := -1.0
	for {
		select {
		case <-w.closed:
			return
		case <-w.captureWake:
		case <-ticker.C:
		}
		w.captureMu.Lock()
		w.mu.Lock()
		casting := w.captureActive
		watching := w.hasVisibleViewerLocked()
		lastFrame, lastPulse := w.lastFrameAt, w.lastPulseAt
		stalled := casting && watching && time.Since(lastFrame) >= 3*time.Second && time.Since(lastPulse) >= 3*time.Second
		if stalled {
			w.lastPulseAt = time.Now()
		}
		w.mu.Unlock()
		wantBudget := manager.gpuBudgetFor(w.Token)
		if math.Abs(wantBudget-budget) > 0.1 {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := w.call(ctx, "Runtime.evaluate", map[string]any{"expression": "window.__g3dSetFrameRate && window.__g3dSetFrameRate(" + strconv.FormatFloat(wantBudget, 'f', 3, 64) + ")"})
			cancel()
			if err == nil {
				budget = wantBudget
				w.mu.Lock()
				w.performance.BudgetFPS = budget
				w.mu.Unlock()
			}
		}
		if watching == casting && !stalled {
			w.captureMu.Unlock()
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if casting {
			if _, err := w.call(ctx, "Page.stopScreencast", map[string]any{}); err != nil {
				w.captureMu.Unlock()
				cancel()
				w.closeAll()
				_ = w.cdp.Close()
				return
			}
			casting = false
		}
		if watching {
			if _, err := w.call(ctx, "Page.startScreencast", w.screencastParams()); err != nil {
				w.captureMu.Unlock()
				cancel()
				w.closeAll()
				_ = w.cdp.Close()
				return
			}
			casting = true
			w.mu.Lock()
			w.lastPulseAt = time.Now()
			w.mu.Unlock()
		}
		cancel()
		w.mu.Lock()
		w.captureActive = casting
		w.mu.Unlock()
		w.captureMu.Unlock()
	}
}

func waitCDPEndpoint(ctx context.Context, port int, process <-chan error) (string, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port) + "/json/list"
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	launcherExited := false
	for {
		if err := ctx.Err(); err != nil {
			if launcherExited {
				return "", fmt.Errorf("local browser launcher exited successfully but DevTools did not become available: %w", err)
			}
			return "", fmt.Errorf("wait for local browser DevTools: %w", err)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return "", err
		}
		response, err := client.Do(request)
		if err == nil {
			var targets []struct {
				Type string `json:"type"`
				WS   string `json:"webSocketDebuggerUrl"`
			}
			if response.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&targets) == nil {
				for _, target := range targets {
					if target.Type == "page" && target.WS != "" {
						response.Body.Close()
						return target.WS, nil
					}
				}
			}
			response.Body.Close()
		}
		select {
		case err := <-process:
			if err != nil {
				return "", fmt.Errorf("local browser exited before DevTools became available: %w", err)
			}
			// A successful launcher exit is not a browser failure. Edge can
			// relaunch during profile initialization or compatibility handling.
			// Descendants remain owned by the worker's job; wait for their CDP
			// endpoint within the original startup deadline. Disable this channel
			// after exit so a closed channel cannot cause a busy polling loop.
			launcherExited = true
			process = nil
		case <-ctx.Done():
			// Report cancellation consistently at the top of the loop.
		case <-ticker.C:
		}
	}
}

// Send requests synchronously to preserve protocol order, then allow callers
// to await the replies independently (CPU input uses a bounded pipeline).
func (w *windowsWorker) beginCall(method string, params any) (int, <-chan cdpMessage, error) {
	w.mu.Lock()
	w.nextID++
	id := w.nextID
	result := make(chan cdpMessage, 1)
	w.pending[id] = result
	w.mu.Unlock()
	if err := w.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		w.mu.Lock()
		delete(w.pending, id)
		w.mu.Unlock()
		return 0, nil, err
	}
	return id, result, nil
}

func (w *windowsWorker) awaitCall(ctx context.Context, id int, result <-chan cdpMessage) (json.RawMessage, error) {
	defer func() { w.mu.Lock(); delete(w.pending, id); w.mu.Unlock() }()
	select {
	case response := <-result:
		if response.Error != nil {
			return nil, errors.New(response.Error.Message)
		}
		return response.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-w.closed:
		return nil, errors.New("local browser disconnected")
	}
}

func (w *windowsWorker) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id, result, err := w.beginCall(method, params)
	if err != nil {
		return nil, err
	}
	response, err := w.awaitCall(ctx, id, result)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	return response, nil
}

func (w *windowsWorker) send(value any) error {
	w.cdpMu.Lock()
	defer w.cdpMu.Unlock()
	_ = w.cdp.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return w.cdp.WriteJSON(value)
}

func (w *windowsWorker) sendNoReply(method string, params any) error {
	w.mu.Lock()
	w.nextID++
	id := w.nextID
	w.mu.Unlock()
	return w.send(map[string]any{"id": id, "method": method, "params": params})
}

func (w *windowsWorker) readCDP() {
	w.cdp.SetReadLimit(24 << 20)
	defer w.closeAll()
	for {
		var message cdpMessage
		if err := w.cdp.ReadJSON(&message); err != nil {
			return
		}
		if message.ID != 0 {
			w.mu.Lock()
			result := w.pending[message.ID]
			w.mu.Unlock()
			if result != nil {
				select {
				case result <- message:
				default:
				}
			}
			continue
		}
		if message.Method != "Page.screencastFrame" {
			continue
		}
		var frame struct {
			Data      string `json:"data"`
			SessionID int    `json:"sessionId"`
			Metadata  struct {
				Timestamp float64 `json:"timestamp"`
			} `json:"metadata"`
		}
		if json.Unmarshal(message.Params, &frame) != nil {
			continue
		}
		w.publishFrame(frame.Data)
		if frame.Metadata.Timestamp > 0 {
			elapsed := float64(time.Now().UnixNano())/1e6 - frame.Metadata.Timestamp*1000
			if validFeedback(elapsed, 30000) {
				w.mu.Lock()
				w.performance.CaptureDeliveryMS = smoothSample(w.performance.CaptureDeliveryMS, elapsed)
				w.mu.Unlock()
			}
		}
		w.ackFrame(frame.SessionID)
	}
}

func (w *windowsWorker) publishFrame(encoded string) {
	begin := time.Now()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.viewportSettling {
		return
	}
	if w.Adaptive {
		config, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width != w.width || config.Height != w.height {
			return
		}
	}
	w.latest = data
	w.lastFrameAt = time.Now()
	w.frameCount++
	w.performance.FrameBytes = len(data)
	w.performance.PublishMS = smoothSample(w.performance.PublishMS, float64(time.Since(begin))/float64(time.Millisecond))
	if w.performanceAt.IsZero() {
		w.performanceAt = time.Now()
	}
	w.performanceFrames++
	if elapsed := time.Since(w.performanceAt).Seconds(); elapsed >= 1 {
		w.performance.CaptureFPS = float64(w.performanceFrames) / elapsed
		w.performanceAt = time.Now()
		w.performanceFrames = 0
	}
	for viewer := range w.viewers {
		if !viewer.visible {
			continue
		}
		select {
		case viewer.frames <- data:
		default:
			select {
			case <-viewer.frames:
				w.performance.ServerDropped++
			default:
			}
			select {
			case viewer.frames <- data:
			default:
			}
		}
	}
}

func (w *windowsWorker) recordInput() {
	w.mu.Lock()
	wake := time.Since(w.lastInputAt) >= 3*time.Second
	w.lastInputAt = time.Now()
	w.mu.Unlock()
	if wake {
		select {
		case w.captureWake <- struct{}{}:
		default:
		}
	}
}

// Acknowledgement pacing bounds JPEG capture work to the configured FPS.
// The browser still decides how often the 3D application draws internally.
func (w *windowsWorker) ackFrame(sessionID int) {
	if w.frameInterval <= 0 {
		_ = w.sendNoReply("Page.screencastFrameAck", map[string]any{"sessionId": sessionID})
		return
	}
	w.ackMu.Lock()
	now := time.Now()
	due := now
	if w.nextAck.After(due) {
		due = w.nextAck
	}
	w.nextAck = due.Add(w.frameInterval)
	w.ackMu.Unlock()
	ack := func() {
		select {
		case <-w.closed:
			return
		default:
		}
		_ = w.sendNoReply("Page.screencastFrameAck", map[string]any{"sessionId": sessionID})
	}
	if due.Equal(now) {
		ack()
		return
	}
	time.AfterFunc(time.Until(due), ack)
}

// SetActive deliberately leaves the page visible. Chromium's lifecycle freeze
// can resume timers while leaving document.visibilityState hidden; many 3D
// applications then stop drawing and the viewer receives only a stale frame.
// The process-wide Windows Job Object remains the CPU hard limit.
func (m *WindowsProcessManager) SetActive(ctx context.Context, token string, active bool) error {
	m.mu.Lock()
	w := m.active[token]
	m.mu.Unlock()
	if w == nil {
		return errors.New("local render worker unavailable")
	}
	return nil
}

func (w *windowsWorker) closeAll() {
	w.closeOne.Do(func() {
		close(w.closed)
		w.mu.Lock()
		for viewer := range w.viewers {
			_ = viewer.conn.Close()
			close(viewer.frames)
			delete(w.viewers, viewer)
		}
		w.mu.Unlock()
	})
}

func (w *windowsWorker) shutdown(ctx context.Context) error {
	w.stopMu.Lock()
	defer w.stopMu.Unlock()
	if w.stopped {
		return nil
	}
	w.closeAll()
	_ = w.cdp.Close()
	if err := stopWindowsBrowserJob(ctx, w.job); err != nil {
		return err
	}
	w.job = 0
	select {
	case <-w.process:
		w.lease.Release()
		w.stopped = true
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *WindowsProcessManager) Stop(ctx context.Context, token string) error {
	m.mu.Lock()
	w := m.active[token]
	m.mu.Unlock()
	if w == nil {
		return nil
	}
	if err := w.shutdown(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	if m.active[token] == w {
		delete(m.active, token)
	}
	m.mu.Unlock()
	return nil
}

func (m *WindowsProcessManager) Serve(w http.ResponseWriter, r *http.Request, signalURL string) error {
	u, err := url.Parse(signalURL)
	if err != nil || u.Scheme != "local-cdp" || u.Host == "" {
		http.Error(w, "invalid local worker", http.StatusBadGateway)
		return errors.New("invalid local worker URL")
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(parsed.Host, r.Host) || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			http.Error(w, "invalid websocket origin", http.StatusForbidden)
			return errors.New("invalid websocket origin")
		}
	}
	m.mu.Lock()
	worker := m.active[u.Host]
	m.mu.Unlock()
	if worker == nil {
		http.Error(w, "local worker unavailable", http.StatusServiceUnavailable)
		return errors.New("local worker unavailable")
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	viewer := &windowsViewer{conn: conn, frames: make(chan []byte, 1), visible: true}
	worker.mu.Lock()
	select {
	case <-worker.closed:
		worker.mu.Unlock()
		return errors.New("local worker stopped")
	default:
	}
	worker.viewers[viewer] = struct{}{}
	worker.lastInputAt = time.Now()
	if len(worker.latest) > 0 {
		viewer.frames <- worker.latest
	}
	worker.mu.Unlock()
	select {
	case worker.captureWake <- struct{}{}:
	default:
	}
	defer func() {
		worker.mu.Lock()
		if _, ok := worker.viewers[viewer]; ok {
			delete(worker.viewers, viewer)
			close(viewer.frames)
		}
		worker.mu.Unlock()
		select {
		case worker.captureWake <- struct{}{}:
		default:
		}
	}()
	writeDone := make(chan error, 1)
	go func() {
		for frame := range viewer.frames {
			writeAt := time.Now()
			worker.mu.Lock()
			telemetry := viewer.telemetry
			viewer.nextFrameID++
			id := viewer.nextFrameID
			inputID := uint64(0)
			// Associate only a newly published latest frame after the command was
			// sent to CDP, never a ready frame queued before input.
			if viewer.inputID > 0 && worker.frameCount > viewer.inputFrame && len(frame) > 0 && len(worker.latest) > 0 && &frame[0] == &worker.latest[0] {
				inputID = viewer.inputID
			}
			if telemetry {
				viewer.sentFrames[id%uint64(len(viewer.sentFrames))] = sentFrameSample{id: id, at: writeAt, inputID: inputID}
			}
			worker.mu.Unlock()
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if telemetry {
				if err := conn.WriteJSON(map[string]any{"type": "frame", "id": id, "inputID": inputID}); err != nil {
					_ = conn.Close()
					writeDone <- err
					return
				}
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				_ = conn.Close()
				writeDone <- err
				return
			}
			worker.mu.Lock()
			worker.performance.WriteMS = smoothSample(worker.performance.WriteMS, float64(time.Since(writeAt))/float64(time.Millisecond))
			worker.lastWriteAt = time.Now()
			if worker.writeSampleAt.IsZero() {
				worker.writeSampleAt = writeAt
			}
			worker.writeSampleFrames++
			if elapsed := time.Since(worker.writeSampleAt).Seconds(); elapsed >= 1 {
				worker.performance.SendFPS = float64(worker.writeSampleFrames) / elapsed
				worker.writeSampleFrames = 0
				worker.writeSampleAt = time.Now()
			}
			worker.mu.Unlock()
		}
		writeDone <- nil
	}()
	conn.SetReadLimit(8192)
	inputCtx, cancelInputs := context.WithCancel(r.Context())
	defer cancelInputs()
	// Resizing may wait for slow compositor replies. Read and dispatch input
	// independently, with only the latest pending viewport retained.
	resizeRequests := make(chan viewport.Size, 1)
	if worker.Adaptive {
		go func() {
			if err := worker.dispatchViewportResizes(inputCtx, resizeRequests); err != nil {
				_ = conn.Close()
			}
		}()
	}
	var inputQueue *windowsInputQueue
	if worker.RenderMode == "cpu" {
		inputQueue = newWindowsInputQueue()
		go func() {
			if err := worker.dispatchCPUInputs(inputCtx, inputQueue); err != nil {
				_ = conn.Close()
			}
		}()
	}
	var currentInputID uint64
	dispatch := func(kind, method string, params map[string]any) error {
		id := currentInputID
		if inputQueue != nil {
			return inputQueue.push(inputCtx, windowsInputCommand{kind: kind, method: method, params: params, sent: func() { worker.markInputSent(viewer, id) }})
		}
		err := worker.sendNoReply(method, params)
		if err == nil {
			worker.markInputSent(viewer, id)
		}
		return err
	}
	for {
		var input struct {
			Type        string  `json:"type"`
			Width       int     `json:"width"`
			Height      int     `json:"height"`
			FrameWidth  int     `json:"frameWidth"`
			FrameHeight int     `json:"frameHeight"`
			X           float64 `json:"x"`
			Y           float64 `json:"y"`
			Button      string  `json:"button"`
			Buttons     int     `json:"buttons"`
			ClickCount  int     `json:"clickCount"`
			DeltaX      float64 `json:"deltaX"`
			DeltaY      float64 `json:"deltaY"`
			Key         string  `json:"key"`
			Code        string  `json:"code"`
			KeyCode     int     `json:"keyCode"`
			Modifiers   int     `json:"modifiers"`
			Text        string  `json:"text"`
			Repeat      bool    `json:"repeat"`
			Location    int     `json:"location"`
			FrameID     uint64  `json:"frameID"`
			DecodeMS    float64 `json:"decodeMS"`
			DrawMS      float64 `json:"drawMS"`
			DisplayFPS  float64 `json:"displayFPS"`
			Dropped     uint64  `json:"dropped"`
			InputID     uint64  `json:"inputID"`
			InputMS     float64 `json:"inputMS"`
		}
		if err := conn.ReadJSON(&input); err != nil {
			return err
		}
		if input.Type == "telemetry" {
			worker.mu.Lock()
			viewer.telemetry = true
			worker.mu.Unlock()
			continue
		}
		if input.Type == "frameAck" {
			worker.recordFrameFeedback(viewer, input.FrameID, input.DecodeMS, input.DrawMS, input.DisplayFPS, input.Dropped, input.InputMS)
			continue
		}
		currentInputID = input.InputID
		if input.Type == "resize" {
			size := viewport.Size{Width: input.Width, Height: input.Height}
			if worker.Adaptive && size.Valid() {
				select {
				case resizeRequests <- size:
				default:
					select {
					case <-resizeRequests:
					default:
					}
					resizeRequests <- size // This read loop is the only producer.
				}
			}
			continue
		}
		if input.Type == "inactive" || input.Type == "activate" {
			worker.mu.Lock()
			viewer.visible = input.Type == "activate"
			if viewer.visible && len(worker.latest) > 0 {
				select {
				case viewer.frames <- worker.latest:
				default:
				}
			} else if !viewer.visible {
				select {
				case <-viewer.frames:
				default:
				}
			}
			worker.mu.Unlock()
			select {
			case worker.captureWake <- struct{}{}:
			default:
			}
			if input.Type == "inactive" {
				continue
			}
			worker.recordInput()
			continue
		}
		if input.Type == "keyDown" || input.Type == "keyUp" {
			if len(input.Key) > 64 || len(input.Code) > 64 || len(input.Text) > 32 ||
				input.KeyCode < 0 || input.KeyCode > 255 || input.Modifiers < 0 || input.Modifiers > 15 ||
				input.Location < 0 || input.Location > 3 {
				continue
			}
			params := map[string]any{"type": input.Type, "key": input.Key, "code": input.Code,
				"windowsVirtualKeyCode": input.KeyCode, "nativeVirtualKeyCode": input.KeyCode,
				"modifiers": input.Modifiers, "autoRepeat": input.Repeat, "location": input.Location}
			if input.Type == "keyDown" && input.Text != "" && input.Modifiers&7 == 0 {
				params["text"] = input.Text
				params["unmodifiedText"] = input.Text
			}
			if err := dispatch(input.Type, "Input.dispatchKeyEvent", params); err != nil {
				return err
			}
			worker.recordInput()
			continue
		}
		if input.Type == "text" {
			if input.Text == "" || len(input.Text) > 4096 {
				continue
			}
			if err := dispatch(input.Type, "Input.insertText", map[string]any{"text": input.Text}); err != nil {
				return err
			}
			worker.recordInput()
			continue
		}
		worker.mu.Lock()
		width, height := worker.width, worker.height
		worker.mu.Unlock()
		// Input from a frame captured before resizing must not hit the new layout.
		if worker.Adaptive && input.Type != "up" && (input.FrameWidth != width || input.FrameHeight != height) {
			continue
		}
		if worker.Adaptive && input.Type == "up" {
			input.X = math.Min(input.X, float64(width))
			input.Y = math.Min(input.Y, float64(height))
		}
		if !finiteCoordinate(input.X, float64(width)) || !finiteCoordinate(input.Y, float64(height)) {
			continue
		}
		if input.ClickCount < 0 || input.ClickCount > 3 {
			continue
		}
		clickCount := input.ClickCount
		if clickCount == 0 {
			clickCount = 1
		}
		params := map[string]any{"x": input.X, "y": input.Y, "buttons": input.Buttons}
		switch input.Type {
		case "move":
			params["type"] = "mouseMoved"
		case "down":
			params["type"], params["button"], params["clickCount"] = "mousePressed", input.Button, clickCount
		case "up":
			params["type"], params["button"], params["clickCount"] = "mouseReleased", input.Button, clickCount
		case "wheel":
			if !finiteWheelDelta(input.DeltaX) || !finiteWheelDelta(input.DeltaY) {
				continue
			}
			params["type"], params["deltaX"], params["deltaY"] = "mouseWheel", input.DeltaX, input.DeltaY
		default:
			continue
		}
		if input.Button != "" && input.Button != "none" && input.Button != "left" && input.Button != "middle" && input.Button != "right" {
			continue
		}
		if err := dispatch(input.Type, "Input.dispatchMouseEvent", params); err != nil {
			return err
		}
		worker.recordInput()
		select {
		case err := <-writeDone:
			return err
		default:
		}
	}
}

func finiteCoordinate(value, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= maximum
}

func finiteWheelDelta(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && math.Abs(value) <= 32767
}
