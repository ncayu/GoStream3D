//go:build windows

package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"gostream3d/internal/viewport"
	"image/jpeg"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWindowsAdaptiveViewportPreservesSceneAndIsolatesViewers(t *testing.T) {
	if _, err := findWindowsBrowser("chromium"); err != nil {
		t.Skip(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<body style="margin:0;background:#168065"><button style="position:fixed;right:10px;bottom:10px;width:100px;height:50px" onclick="window.clicks++">Click</button><script>window.boot=Math.random();window.clicks=0;document.body.dataset.ready='yes';</script></body>`)
	}))
	defer target.Close()
	hp, cp := freeTestPort(t, 34400), freeTestPort(t, 34500)
	ports, err := NewPortAllocator(PortRange{hp, hp + 2}, PortRange{cp, cp + 2}, PortRange{54400, 54423}, 8)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := NewNativeManager(ports, NativeConfig{WorkRoot: t.TempDir(), ReadyTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	m := controller.(*WindowsProcessManager)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	var workers []*windowsWorker
	var viewers []*websocket.Conn
	gateways := make([]*httptest.Server, 0, 2)
	defer func() {
		for _, v := range viewers {
			v.Close()
		}
		for _, s := range gateways {
			s.Close()
		}
		for _, w := range workers {
			m.Stop(context.Background(), w.Token)
		}
	}()
	for i, mode := range []string{"cpu", "nvidia"} {
		worker, err := m.StartWorker(ctx, WorkerSpec{Token: fmt.Sprintf("adaptive-%d", i), TargetURL: target.URL, Screen: "1920x1080@30", RenderMode: mode, Adaptive: true, ReadySelector: "body[data-ready='yes']"})
		if err != nil {
			t.Fatal(err)
		}
		workers = append(workers, m.active[worker.Token])
		gateway := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { _ = m.Serve(rw, r, worker.SignalURL) }))
		gateways = append(gateways, gateway)
		v, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		viewers = append(viewers, v)
	}
	state := func(w *windowsWorker) map[string]any {
		data, err := w.call(ctx, "Runtime.evaluate", map[string]any{"expression": "({width:innerWidth,height:innerHeight,boot:window.boot,clicks:window.clicks})", "returnByValue": true})
		if err != nil {
			t.Fatal(err)
		}
		var value struct {
			Result struct {
				Value map[string]any `json:"value"`
			} `json:"result"`
		}
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		return value.Result.Value
	}
	boots := []any{state(workers[0])["boot"], state(workers[1])["boot"]}
	resize := func(i int, width, height int) {
		if err := viewers[i].WriteJSON(viewport.Size{Type: "resize", Width: width, Height: height}); err != nil {
			t.Fatal(err)
		}
		_ = viewers[i].SetReadDeadline(time.Now().Add(12 * time.Second))
		for {
			_, frame, err := viewers[i].ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			size, err := jpeg.DecodeConfig(bytes.NewReader(frame))
			if err == nil && size.Width == width && size.Height == height {
				break
			}
		}
		got := state(workers[i])
		if got["width"] != float64(width) || got["height"] != float64(height) || got["boot"] != boots[i] {
			t.Fatalf("scene state changed: %+v", got)
		}
	}
	resize(0, 1280, 640)
	resize(1, 560, 960)
	resize(0, 1600, 800)
	other := state(workers[1])
	if other["width"] != float64(560) || other["height"] != float64(960) {
		t.Fatalf("another viewer was resized: %+v", other)
	}
	for i, size := range []viewport.Size{{Width: 1600, Height: 800}, {Width: 560, Height: 960}} {
		for _, kind := range []string{"down", "up"} {
			err := viewers[i].WriteJSON(map[string]any{"type": kind, "x": size.Width - 60, "y": size.Height - 35, "frameWidth": size.Width, "frameHeight": size.Height, "button": "left", "buttons": map[string]int{"down": 1, "up": 0}[kind]})
			if err != nil {
				t.Fatal(err)
			}
		}
		deadline := time.Now().Add(4 * time.Second)
		for state(workers[i])["clicks"] != float64(1) {
			if time.Now().After(deadline) {
				t.Fatalf("viewer %d click missed after resize", i)
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	_ = viewers[0].WriteJSON(viewport.Size{Type: "resize", Width: 20000, Height: 800})
	_ = viewers[0].WriteJSON(map[string]any{"type": "move", "x": 1, "y": 1, "frameWidth": 1600, "frameHeight": 800})
	time.Sleep(100 * time.Millisecond)
	if state(workers[0])["width"] != float64(1600) {
		t.Fatal("invalid viewport allocated")
	}
}

// Resizing a WebGL canvas clears its drawing buffer. The stream must wait for
// the source's ready marker and repaint, rather than publishing that clear.
func TestWindowsViewportDoesNotPublishResizePlaceholder(t *testing.T) {
	if _, err := findWindowsBrowser("chromium"); err != nil {
		t.Skip(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<body style="margin:0"><canvas></canvas><script>
const canvas=document.querySelector('canvas'), ctx=canvas.getContext('2d');
function paint(color){canvas.width=innerWidth;canvas.height=innerHeight;ctx.fillStyle=color;ctx.fillRect(0,0,innerWidth,innerHeight);}
paint('#168065');document.body.dataset.ready='yes';
window.app={inited:true,sceneRoot:{children:[{}]},campusLoader:{loadingCampus:false,waitingLoadCampuses:[]},renderer:{domElement:canvas},_pixelRatio:1,_currentFrame:1,width:innerWidth,height:innerHeight};
let phase=0;
function render(){app._currentFrame++;if(phase===1)phase=2;else if(phase===2){paint('#168065');phase=0;}requestAnimationFrame(render);}requestAnimationFrame(render);
addEventListener('resize',()=>{
 if(location.search==='?deferred')setTimeout(()=>{paint('#eeeeee');app.width=innerWidth;app.height=innerHeight;phase=1;},200);
 else{delete document.body.dataset.ready;paint('#eeeeee');setTimeout(()=>{paint('#168065');document.body.dataset.ready='yes';},350);}
});
</script></body>`)
	}))
	defer target.Close()
	for i, scenario := range []struct {
		mode     string
		deferred bool
	}{{"cpu", false}, {"nvidia", false}, {"nvidia", true}} {
		t.Run(fmt.Sprintf("%s/deferred-%t", scenario.mode, scenario.deferred), func(t *testing.T) {
			hp, cp := freeTestPort(t, 34600+i*10), freeTestPort(t, 34700+i*10)
			ports, err := NewPortAllocator(PortRange{hp, hp}, PortRange{cp, cp}, PortRange{54600, 54607}, 8)
			if err != nil {
				t.Fatal(err)
			}
			controller, err := NewNativeManager(ports, NativeConfig{WorkRoot: t.TempDir(), ReadyTimeout: 30 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			m := controller.(*WindowsProcessManager)
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			targetURL := target.URL
			if scenario.deferred {
				targetURL += "?deferred"
			}
			worker, err := m.StartWorker(ctx, WorkerSpec{Token: "resize-placeholder", TargetURL: targetURL, Screen: "1280x720@30", RenderMode: scenario.mode, Adaptive: true, ReadySelector: "body[data-ready='yes']"})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Stop(context.Background(), worker.Token)
			if scenario.deferred {
				m.active[worker.Token].targetURL = "https://campus.thingjs.com/s/p/fixture"
			}
			gateway := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { _ = m.Serve(rw, r, worker.SignalURL) }))
			defer gateway.Close()
			viewer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer viewer.Close()
			if err := viewer.WriteJSON(viewport.Size{Type: "resize", Width: 1600, Height: 800}); err != nil {
				t.Fatal(err)
			}
			viewer.SetReadDeadline(time.Now().Add(10 * time.Second))
			frames := 0
			for {
				_, data, err := viewer.ReadMessage()
				if err != nil {
					if timeout, ok := err.(net.Error); ok && timeout.Timeout() && frames > 0 {
						break
					}
					t.Fatal(err)
				}
				image, err := jpeg.Decode(bytes.NewReader(data))
				if err != nil || image.Bounds().Dx() != 1600 || image.Bounds().Dy() != 800 {
					continue
				}
				r, g, b, _ := image.At(800, 400).RGBA()
				if g <= r*2 || g <= b {
					t.Fatalf("resize placeholder reached viewer: RGB %d,%d,%d", r>>8, g>>8, b>>8)
				}
				frames++
				if frames == 1 {
					viewer.SetReadDeadline(time.Now().Add(time.Second))
				}
			}
			if scenario.deferred {
				// A cancelled repaint must restore its temporary budget once, and
				// must not subsequently overwrite a new viewing budget.
				w := m.active[worker.Token]
				w.captureMu.Lock()
				defer w.captureMu.Unlock()
				if _, err := w.call(ctx, "Runtime.evaluate", map[string]any{"expression": "window.__g3dSetFrameRate(6)"}); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() {
					_, err := w.call(ctx, "Runtime.evaluate", map[string]any{"expression": viewportReadyExpression(target.URL, "#never-ready", viewport.Size{Width: 1600, Height: 800}), "awaitPromise": true, "returnByValue": true})
					done <- err
				}()
				deadline := time.Now().Add(3 * time.Second)
				for {
					data, err := w.call(ctx, "Runtime.evaluate", map[string]any{"expression": "typeof window.__g3dCancelViewportWait === 'function'", "returnByValue": true})
					if err != nil {
						t.Fatal(err)
					}
					var result struct {
						Result struct {
							Value bool `json:"value"`
						} `json:"result"`
					}
					if json.Unmarshal(data, &result) == nil && result.Result.Value {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("repaint wait did not start")
					}
					time.Sleep(10 * time.Millisecond)
				}
				if _, err := w.call(ctx, "Runtime.evaluate", map[string]any{"expression": "window.__g3dCancelViewportWait(); window.__g3dSetFrameRate(12)"}); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("cancelled repaint kept waiting")
				}
				data, err := w.call(ctx, "Runtime.evaluate", map[string]any{"expression": "window.__g3dFrameRate === 12 && !window.__g3dCancelViewportWait", "returnByValue": true})
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					Result struct {
						Value bool `json:"value"`
					} `json:"result"`
				}
				if json.Unmarshal(data, &result) != nil || !result.Result.Value {
					t.Fatal("cancelled repaint changed a later frame budget")
				}
			}
		})
	}
}
