//go:build windows

package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWindowsCPUFullResolutionCapture(t *testing.T) {
	if _, err := findWindowsBrowser("chromium"); err != nil {
		t.Skip(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<body style="margin:0"><canvas width="1920" height="1080"></canvas><script>
const gl=document.querySelector('canvas').getContext('webgl');
let n=0;function draw(){gl.clearColor((n++%240)/240,.2,.3,1);gl.clear(gl.COLOR_BUFFER_BIT);requestAnimationFrame(draw)}
draw();document.body.dataset.ready='yes';
</script></body>`)
	}))
	defer target.Close()
	for _, profile := range []struct{ backend, effects, mode string }{
		{"swiftshader", "original", "cpu"},
		{"warp", "original", "cpu"},
		{"warp", "simplified", "cpu"},
		{"warp", "simplified", "nvidia"},
	} {
		backend := profile.backend
		t.Run(backend+"-"+profile.effects+"-"+profile.mode, func(t *testing.T) {
			httpPort, cdpPort := freeTestPort(t, 32400), freeTestPort(t, 32500)
			p, err := NewPortAllocator(PortRange{httpPort, httpPort}, PortRange{cdpPort, cdpPort}, PortRange{52400, 52407}, 8)
			if err != nil {
				t.Fatal(err)
			}
			controller, err := NewNativeManager(p, NativeConfig{WorkRoot: t.TempDir(), CPURenderer: backend, CPUEffects: profile.effects, ReadyTimeout: 45 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			m := controller.(*WindowsProcessManager)
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
			defer cancel()
			worker, err := m.StartWorker(ctx, WorkerSpec{Token: "full-resolution-test", TargetURL: target.URL, Screen: "1920x1080@30", RenderMode: profile.mode, ReadySelector: "body[data-ready='yes']"})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Stop(context.Background(), worker.Token)
			w := m.active[worker.Token]
			attributes, err := w.call(ctx, "Runtime.evaluate", map[string]any{"expression": `(()=>{const gl=document.querySelector('canvas').getContext('webgl');return {antialias:gl.getContextAttributes().antialias,profile:window.__g3dCPUEffects?.profile||'',width:gl.drawingBufferWidth,height:gl.drawingBufferHeight}})()`, "returnByValue": true})
			if err != nil {
				t.Fatal(err)
			}
			var contextValue struct {
				Result struct {
					Value struct {
						Antialias bool   `json:"antialias"`
						Profile   string `json:"profile"`
						Width     int    `json:"width"`
						Height    int    `json:"height"`
					} `json:"value"`
				} `json:"result"`
			}
			if err := json.Unmarshal(attributes, &contextValue); err != nil {
				t.Fatal(err)
			}
			v := contextValue.Result.Value
			if v.Width != 1920 || v.Height != 1080 {
				t.Fatalf("context resolution changed: %+v", v)
			}
			if profile.mode == "cpu" && profile.effects == "simplified" {
				if v.Antialias || v.Profile != "simplified" {
					t.Fatalf("CPU profile did not apply before context creation: %+v", v)
				}
			} else if v.Profile != "" || !v.Antialias {
				t.Fatalf("original/GPU context modified: %+v", v)
			}
			renderer, err := w.call(ctx, "Runtime.evaluate", map[string]any{"expression": `(()=>{const g=document.querySelector('canvas').getContext('webgl'),e=g.getExtension('WEBGL_debug_renderer_info');return e?g.getParameter(e.UNMASKED_RENDERER_WEBGL):''})()`, "returnByValue": true})
			if err != nil {
				t.Fatal(err)
			}
			var value struct {
				Result struct {
					Value string `json:"value"`
				} `json:"result"`
			}
			if err := json.Unmarshal(renderer, &value); err != nil {
				t.Fatal(err)
			}
			description := strings.ToLower(value.Result.Value)
			if profile.mode == "cpu" && backend == "warp" && !strings.Contains(description, "basic render") && !strings.Contains(description, "warp") {
				t.Fatalf("WARP used a non-software renderer: %s", value.Result.Value)
			}
			if profile.mode == "cpu" && backend == "swiftshader" && !strings.Contains(description, "swiftshader") {
				t.Fatalf("unexpected renderer: %s", value.Result.Value)
			}
			t.Logf("verified CPU backend: %s", value.Result.Value)
			viewer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { _ = m.Serve(rw, r, worker.SignalURL) }))
			defer viewer.Close()
			ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(viewer.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			start := time.Now()
			frames := 0
			hashes := map[[32]byte]bool{}
			for time.Since(start) < 2*time.Second {
				_ = ws.SetReadDeadline(time.Now().Add(8 * time.Second))
				_, frame, err := ws.ReadMessage()
				if err != nil {
					t.Fatal(err)
				}
				bounds, err := jpeg.DecodeConfig(bytes.NewReader(frame))
				if err != nil {
					t.Fatal(err)
				}
				if bounds.Width != 1920 || bounds.Height != 1080 {
					t.Fatalf("resolution changed: %+v", bounds)
				}
				frames++
				hashes[sha256.Sum256(frame)] = true
			}
			if len(hashes) < 2 {
				t.Fatal("animated CPU scene returned only a static image")
			}
			t.Logf("1920x1080 stream: %.1f FPS, %d distinct frames", float64(frames)/time.Since(start).Seconds(), len(hashes))
			_ = ws.Close()
			deadline := time.Now().Add(8 * time.Second)
			for {
				w.mu.Lock()
				stopped := len(w.viewers) == 0 && !w.captureActive
				w.mu.Unlock()
				if stopped {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("CPU capture continued without viewers")
				}
				time.Sleep(50 * time.Millisecond)
			}
		})
	}
}

func freeTestPort(t *testing.T, start int) int {
	t.Helper()
	for port := start; port < start+100; port++ {
		listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			listener.Close()
			return port
		}
	}
	t.Fatalf("no free test port in %d-%d", start, start+99)
	return 0
}

func TestWindowsLocalCaptureAndInput(t *testing.T) {
	if _, err := findWindowsBrowser("chromium"); err != nil {
		t.Skip(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body style="margin:0"><canvas width="640" height="360"></canvas><input id="text" style="position:absolute;left:450px;top:50px;width:140px"><script>
const canvas=document.querySelector('canvas'); const gl=canvas.getContext('webgl',{preserveDrawingBuffer:true});
if(gl){gl.clearColor(.8,0,0,1);gl.clear(gl.COLOR_BUFFER_BIT);document.body.dataset.ready='yes';
canvas.addEventListener('click',()=>{gl.clearColor(0,.8,0,1);gl.clear(gl.COLOR_BUFFER_BIT)});
canvas.addEventListener('dblclick',()=>{gl.clearColor(.8,0,.8,1);gl.clear(gl.COLOR_BUFFER_BIT)});
canvas.addEventListener('mousemove',event=>{if(event.buttons&1){gl.clearColor(.8,.8,0,1);gl.clear(gl.COLOR_BUFFER_BIT)}});
canvas.addEventListener('wheel',event=>{if(event.deltaY<0)gl.clearColor(.8,0,.8,1);else gl.clearColor(0,0,.8,1);gl.clear(gl.COLOR_BUFFER_BIT)});}
document.querySelector('#text').addEventListener('input',event=>{const value=event.target.value;gl.clearColor(value==='a'?.8:0,0,value==='ab'?.8:0,1);gl.clear(gl.COLOR_BUFFER_BIT)});
</script></body></html>`))
	}))
	defer target.Close()
	httpPort, cdpPort := freeTestPort(t, 32000), freeTestPort(t, 32100)
	ports, err := NewPortAllocator(PortRange{httpPort, httpPort}, PortRange{cdpPort, cdpPort}, PortRange{52000, 52007}, 8)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := NewNativeManager(ports, NativeConfig{WorkRoot: t.TempDir(), ReadyTimeout: 45 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	manager := controller.(*WindowsProcessManager)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	worker, err := manager.StartWorker(ctx, WorkerSpec{Token: "capture-test", TargetURL: target.URL, Screen: "640x360@30", RenderMode: "cpu", ReadySelector: "body[data-ready='yes']"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := manager.Stop(stopCtx, worker.Token); err != nil {
			t.Error(err)
		}
	}()
	viewer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = manager.Serve(w, r, worker.SignalURL) }))
	defer viewer.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(viewer.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(20 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	frame, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if frame.Bounds().Dx() < 600 || frame.Bounds().Dy() < 300 {
		t.Fatalf("unexpected frame bounds: %v", frame.Bounds())
	}
	firstR, firstG, _, _ := frame.At(100, 100).RGBA()
	if firstR <= firstG {
		t.Fatal("preloaded WebGL canvas did not render the red scene")
	}
	if err := ws.WriteJSON(map[string]any{"type": "down", "x": 100, "y": 100, "button": "left", "buttons": 1}); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteJSON(map[string]any{"type": "up", "x": 100, "y": 100, "button": "left", "buttons": 0}); err != nil {
		t.Fatal(err)
	}
	waitForColor := func(name string, accept func(uint32, uint32, uint32) bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			_ = ws.SetReadDeadline(deadline)
			_, next, readErr := ws.ReadMessage()
			if readErr != nil {
				t.Fatalf("wait for %s: %v", name, readErr)
			}
			image, decodeErr := jpeg.Decode(bytes.NewReader(next))
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			r, g, b, _ := image.At(100, 100).RGBA()
			if accept(r, g, b) {
				return
			}
		}
		t.Fatalf("%s was not captured", name)
	}
	waitForColor("click", func(r, g, b uint32) bool { return g > r && g > b })
	for _, input := range []map[string]any{
		{"type": "down", "x": 100, "y": 100, "button": "left", "buttons": 1, "clickCount": 2},
		{"type": "up", "x": 100, "y": 100, "button": "left", "buttons": 0, "clickCount": 2},
	} {
		if err := ws.WriteJSON(input); err != nil {
			t.Fatal(err)
		}
	}
	waitForColor("double click", func(r, g, b uint32) bool { return r > g && b > g && r > 20000 && b > 20000 })
	if err := ws.WriteJSON(map[string]any{"type": "down", "x": 100, "y": 100, "button": "left", "buttons": 1}); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteJSON(map[string]any{"type": "move", "x": 120, "y": 110, "button": "none", "buttons": 1}); err != nil {
		t.Fatal(err)
	}
	waitForColor("drag", func(r, g, b uint32) bool { return r > b && g > b && r > 20000 && g > 20000 })
	if err := ws.WriteJSON(map[string]any{"type": "up", "x": 120, "y": 110, "button": "left", "buttons": 0}); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteJSON(map[string]any{"type": "wheel", "x": 120, "y": 110, "button": "none", "buttons": 0, "deltaX": 0, "deltaY": 100}); err != nil {
		t.Fatal(err)
	}
	waitForColor("wheel", func(r, g, b uint32) bool { return b > r && b > g })
	if err := ws.WriteJSON(map[string]any{"type": "wheel", "x": 120, "y": 110, "button": "none", "buttons": 0, "deltaX": 0, "deltaY": -100}); err != nil {
		t.Fatal(err)
	}
	waitForColor("reverse wheel", func(r, g, b uint32) bool { return r > g && b > g && r > 20000 && b > 20000 })
	for _, input := range []map[string]any{
		{"type": "down", "x": 470, "y": 60, "button": "left", "buttons": 1},
		{"type": "up", "x": 470, "y": 60, "button": "left", "buttons": 0},
		{"type": "keyDown", "key": "a", "code": "KeyA", "keyCode": 65, "text": "a"},
		{"type": "keyUp", "key": "a", "code": "KeyA", "keyCode": 65},
	} {
		if err := ws.WriteJSON(input); err != nil {
			t.Fatal(err)
		}
	}
	waitForColor("keyboard", func(r, g, b uint32) bool { return r > g && r > b })
	if err := ws.WriteJSON(map[string]any{"type": "text", "text": "b"}); err != nil {
		t.Fatal(err)
	}
	waitForColor("text insertion", func(r, g, b uint32) bool { return b > r && b > g })
}

func TestWindowsMultiViewersHaveIndependentFramesAndInput(t *testing.T) {
	if _, err := findWindowsBrowser("chromium"); err != nil {
		t.Skip(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<body style="margin:0"><canvas width="1280" height="720"></canvas><script>
const canvas=document.querySelector('canvas'),gl=canvas.getContext('webgl',{preserveDrawingBuffer:true});
gl.clearColor(.8,0,0,1);gl.clear(gl.COLOR_BUFFER_BIT);document.body.dataset.ready='yes';
canvas.addEventListener('click',()=>{gl.clearColor(0,.8,0,1);gl.clear(gl.COLOR_BUFFER_BIT)});
</script></body>`))
	}))
	defer target.Close()
	httpPort, cdpPort := freeTestPort(t, 32200), freeTestPort(t, 32300)
	ports, err := NewPortAllocator(PortRange{httpPort, httpPort + 2}, PortRange{cdpPort, cdpPort + 2}, PortRange{52100, 52123}, 8)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := NewNativeManager(ports, NativeConfig{WorkRoot: t.TempDir(), ReadyTimeout: 45 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	manager := controller.(*WindowsProcessManager)
	sessions, err := NewSessionManager(manager, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetKeepPreloaded(true)
	sessions.SetChannelCapacity(ports.Capacity())
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := sessions.StopAll(stopCtx); err != nil {
			t.Error(err)
		}
	}()
	info, err := sessions.CreatePublished(SessionOptions{TargetURL: target.URL, Resolution: "1280x720", FPS: 30, ConcurrencyMode: "multi", MaxViewers: 3, PreloadCount: 3, ReadySelector: "body[data-ready='yes']"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	if _, err := sessions.Warm(ctx, info.Token); err != nil {
		t.Fatal(err)
	}
	if status, _ := sessions.Status(info.Token); status.ReadyInstances != 3 {
		t.Fatalf("pool not ready: %+v", status)
	}
	hotStarted := time.Now()
	first, releaseFirst, err := sessions.Acquire(ctx, info.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseFirst()
	second, releaseSecond, err := sessions.Acquire(ctx, info.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseSecond()
	third, releaseThird, err := sessions.Acquire(ctx, info.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseThird()
	if time.Since(hotStarted) > 2*time.Second {
		t.Fatal("ready routes waited for browser startup")
	}
	if third.Token == first.Token || third.Token == second.Token || third.CDPPort == first.CDPPort || third.CDPPort == second.CDPPort {
		t.Fatal("third viewer shares an instance")
	}
	if first.Token == second.Token || first.SignalURL == second.SignalURL || first.CDPPort == second.CDPPort {
		t.Fatalf("viewers share a browser: first=%+v second=%+v", first, second)
	}
	viewer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signal := first.SignalURL
		if r.URL.Path == "/second" {
			signal = second.SignalURL
		}
		if r.URL.Path == "/third" {
			signal = third.SignalURL
		}
		_ = manager.Serve(w, r, signal)
	}))
	defer viewer.Close()
	open := func(path string) *websocket.Conn {
		t.Helper()
		ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(viewer.URL, "http")+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		return ws
	}
	one, two, three := open("/first"), open("/second"), open("/third")
	defer three.Close()
	defer one.Close()
	defer two.Close()
	readColor := func(ws *websocket.Conn) (uint32, uint32) {
		t.Helper()
		_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
		_, frame, err := ws.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		image, err := jpeg.Decode(bytes.NewReader(frame))
		if err != nil {
			t.Fatal(err)
		}
		r, g, _, _ := image.At(100, 100).RGBA()
		return r, g
	}
	for _, ws := range []*websocket.Conn{one, two, three} {
		r, g := readColor(ws)
		if r <= g {
			t.Fatalf("expected independent red initial frame: r=%d g=%d", r, g)
		}
	}
	for _, event := range []map[string]any{
		{"type": "down", "x": 100, "y": 100, "button": "left", "buttons": 1},
		{"type": "up", "x": 100, "y": 100, "button": "left", "buttons": 0},
	} {
		if err := one.WriteJSON(event); err != nil {
			t.Fatal(err)
		}
	}
	green := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r, g := readColor(one)
		if g > r {
			green = true
			break
		}
	}
	if !green {
		t.Fatal("first viewer did not receive its click")
	}
	for _, ws := range []*websocket.Conn{two, three} {
		r, g := readColor(ws)
		if r <= g {
			t.Fatalf("another viewer changed with first viewer's input: r=%d g=%d", r, g)
		}
	}
}

func TestWindowsPreloadWaitsForSceneFrame(t *testing.T) {
	if _, err := findWindowsBrowser("chromium"); err != nil {
		t.Skip(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body style="margin:0"><canvas width="640" height="360"></canvas><script>
const canvas=document.querySelector('canvas');const ctx=canvas.getContext('2d');ctx.fillStyle='red';ctx.fillRect(0,0,640,360);
setTimeout(()=>{ctx.fillStyle='green';ctx.fillRect(0,0,640,360);document.body.id='scene-ready'},900);
</script></body></html>`))
	}))
	defer target.Close()
	httpPort, cdpPort := freeTestPort(t, 32800), freeTestPort(t, 32900)
	ports, err := NewPortAllocator(PortRange{httpPort, httpPort}, PortRange{cdpPort, cdpPort}, PortRange{52040, 52047}, 8)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := NewNativeManager(ports, NativeConfig{WorkRoot: t.TempDir(), ReadyTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	manager := controller.(*WindowsProcessManager)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	worker, err := manager.StartWorker(ctx, WorkerSpec{Token: "scene-ready-test", TargetURL: target.URL,
		Screen: "640x360@30", RenderMode: "cpu", ReadySelector: "#scene-ready"})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(context.Background(), worker.Token)
	viewer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = manager.Serve(w, r, worker.SignalURL) }))
	defer viewer.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(viewer.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	frame, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := frame.At(100, 100).RGBA()
	if g <= r || g <= b {
		t.Fatal("preload published a placeholder frame before the scene was ready")
	}
}

func TestThingJSReadinessRequiresSceneLoader(t *testing.T) {
	expression := readinessExpression("https://campus.thingjs.com/s/p/example", "")
	for _, required := range []string{"app.inited", "app.sceneRoot", "app.campusLoader.loadingCampus", "waitingLoadCampuses", "app._currentFrame"} {
		if !strings.Contains(expression, required) {
			t.Fatalf("missing %s in readiness expression", required)
		}
	}
	if strings.Contains(readinessExpression("https://example.com/scene", "#ready"), "campusLoader") {
		t.Fatal("ThingJS condition leaked into other sites")
	}
}

func TestWindowsIdlePageRemainsVisibleForNextViewer(t *testing.T) {
	if _, err := findWindowsBrowser("chromium"); err != nil {
		t.Skip(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><script>window.ticks=0;setInterval(()=>{window.ticks++;document.body.style.backgroundColor=window.ticks%2?'#123':'#345'},20)</script></body></html>`))
	}))
	defer target.Close()
	httpPort, cdpPort := freeTestPort(t, 32400), freeTestPort(t, 32500)
	ports, err := NewPortAllocator(PortRange{httpPort, httpPort}, PortRange{cdpPort, cdpPort}, PortRange{52020, 52027}, 8)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := NewNativeManager(ports, NativeConfig{WorkRoot: t.TempDir(), ReadyTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	manager := controller.(*WindowsProcessManager)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	worker, err := manager.StartWorker(ctx, WorkerSpec{Token: "idle-freeze", TargetURL: target.URL, Screen: "640x360@30", RenderMode: "cpu"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := manager.Stop(stopCtx, worker.Token); err != nil {
			t.Error(err)
		}
	}()
	manager.mu.Lock()
	w := manager.active[worker.Token]
	manager.mu.Unlock()
	readTicks := func() int {
		t.Helper()
		result, err := w.call(ctx, "Runtime.evaluate", map[string]any{"expression": "window.ticks", "returnByValue": true})
		if err != nil {
			t.Fatal(err)
		}
		var value struct {
			Result struct {
				Value int `json:"value"`
			} `json:"result"`
		}
		if err := json.Unmarshal(result, &value); err != nil {
			t.Fatal(err)
		}
		return value.Result.Value
	}
	readVisibility := func() string {
		t.Helper()
		result, err := w.call(ctx, "Runtime.evaluate", map[string]any{"expression": "document.visibilityState", "returnByValue": true})
		if err != nil {
			t.Fatal(err)
		}
		var value struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if err := json.Unmarshal(result, &value); err != nil {
			t.Fatal(err)
		}
		return value.Result.Value
	}
	initialVisibility := readVisibility()
	time.Sleep(200 * time.Millisecond)
	beforeIdle := readTicks()
	if err := manager.SetActive(ctx, worker.Token, false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	if err := manager.SetActive(ctx, worker.Token, true); err != nil {
		t.Fatal(err)
	}
	if visibility := readVisibility(); visibility != initialVisibility {
		t.Fatalf("page visibility was not restored: before=%s after=%s", initialVisibility, visibility)
	}
	afterIdle := readTicks()
	if afterIdle-beforeIdle < 3 {
		t.Fatalf("page stopped rendering while idle: before=%d after=%d", beforeIdle, afterIdle)
	}
	viewer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = manager.Serve(w, r, worker.SignalURL) }))
	defer viewer.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(viewer.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	seen := make(map[[32]byte]bool)
	deadline := time.Now().Add(5 * time.Second)
	for len(seen) < 2 {
		_ = ws.SetReadDeadline(deadline)
		kind, frame, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("live frames stopped after idle: %v (unique=%d)", err, len(seen))
		}
		if kind == websocket.BinaryMessage {
			seen[sha256.Sum256(frame)] = true
		}
	}
}

func TestWindowsWorkerOpensConfiguredURLAfterPreviousNavigation(t *testing.T) {
	if _, err := findWindowsBrowser("chromium"); err != nil {
		t.Skip(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>" + r.URL.Path + "</body></html>"))
	}))
	defer target.Close()
	httpPort, cdpPort := freeTestPort(t, 32600), freeTestPort(t, 32700)
	ports, err := NewPortAllocator(PortRange{httpPort, httpPort}, PortRange{cdpPort, cdpPort}, PortRange{52030, 52037}, 8)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := NewNativeManager(ports, NativeConfig{WorkRoot: t.TempDir(), ReadyTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	manager := controller.(*WindowsProcessManager)
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	spec := WorkerSpec{Token: "navigation-test", TargetURL: target.URL + "/configured", Screen: "640x360@30", RenderMode: "cpu"}
	if _, err := manager.StartWorker(ctx, spec); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	first := manager.active[spec.Token]
	manager.mu.Unlock()
	if _, err := first.call(ctx, "Page.navigate", map[string]any{"url": target.URL + "/other"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(ctx, spec.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartWorker(ctx, spec); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(context.Background(), spec.Token)
	manager.mu.Lock()
	second := manager.active[spec.Token]
	manager.mu.Unlock()
	result, err := second.call(ctx, "Runtime.evaluate", map[string]any{"expression": "location.pathname", "returnByValue": true})
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &value); err != nil {
		t.Fatal(err)
	}
	if value.Result.Value != "/configured" {
		t.Fatalf("browser restored the wrong page: %s", value.Result.Value)
	}
}

func TestWindowsSavedProjectPreloadsAfterRestart(t *testing.T) {
	if _, err := findWindowsBrowser("chromium"); err != nil {
		t.Skip(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body style="background:#c00">preloaded</body></html>`))
	}))
	defer target.Close()
	store := filepath.Join(t.TempDir(), "sessions.json")
	httpPort, cdpPort := freeTestPort(t, 32200), freeTestPort(t, 32300)
	newManager := func() *WindowsProcessManager {
		ports, err := NewPortAllocator(PortRange{httpPort, httpPort}, PortRange{cdpPort, cdpPort}, PortRange{52010, 52017}, 8)
		if err != nil {
			t.Fatal(err)
		}
		controller, err := NewNativeManager(ports, NativeConfig{WorkRoot: t.TempDir(), ReadyTimeout: 30 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		return controller.(*WindowsProcessManager)
	}
	first, err := NewSessionManager(newManager(), 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	first.SetKeepPreloaded(true)
	if err := first.EnableStore(store); err != nil {
		t.Fatal(err)
	}
	info, err := first.CreatePrewarmed(context.Background(), SessionOptions{TargetURL: target.URL, Resolution: "1280x720", FPS: 30})
	if err != nil {
		t.Fatal(err)
	}
	stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	if err := first.StopAll(stopCtx); err != nil {
		t.Fatal(err)
	}
	stop()
	restored, err := NewSessionManager(newManager(), 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	restored.SetKeepPreloaded(true)
	if err := restored.EnableStore(store); err != nil {
		t.Fatal(err)
	}
	preloadCtx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	if err := restored.PreloadSaved(preloadCtx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := restored.StopAll(ctx); err != nil {
			t.Error(err)
		}
	}()
	status := restored.List()
	if len(status) != 1 || status[0].Token != info.Token || !status[0].Running {
		t.Fatalf("saved project was not ready on restart: %+v", status)
	}
}
