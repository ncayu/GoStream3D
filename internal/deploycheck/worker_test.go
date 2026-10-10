package deploycheck

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"gostream3d/internal/orchestrator"
	"gostream3d/internal/proxy"
	"gostream3d/internal/viewport"
)

// This opt-in check starts only its own worker; it never invokes CleanupStale.
func TestDockerDeploymentWorker(t *testing.T) {
	if os.Getenv("GOSTREAM3D_TEST_DOCKER_DEPLOY") != "1" {
		t.Skip("set GOSTREAM3D_TEST_DOCKER_DEPLOY=1 after building the CPU image")
	}
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><body id="ready"><canvas id="c" width="640" height="360"></canvas><script>const g=c.getContext('webgl',{preserveDrawingBuffer:true});g.clearColor(0.1,0.6,0.2,1);g.clear(g.COLOR_BUFFER_BIT);</script></body></html>`)
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()
	targetHost := os.Getenv("GOSTREAM3D_TEST_TARGET_HOST")
	if targetHost == "" {
		targetHost = "host.docker.internal" // Docker Desktop; native Linux can supply its host bridge IP.
	}
	target := "http://" + net.JoinHostPort(targetHost, strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	cli, err := orchestrator.NewDockerClientFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ports, err := orchestrator.NewPortAllocator(orchestrator.PortRange{Start: 43280, End: 43280},
		orchestrator.PortRange{Start: 43281, End: 43281}, orchestrator.PortRange{Start: 45240, End: 45247}, 8)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := orchestrator.NewDockerManager(cli, ports, orchestrator.DockerConfig{
		Image:        os.Getenv("GOSTREAM3D_TEST_WORKER_IMAGE"),
		UserPassword: "deployment-test-user-secret", AdminPassword: "deployment-test-admin-secret",
		HTTPBindIP: "127.0.0.1", ICEPublicIP: "127.0.0.1", ReadyTimeout: 90 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	token := "deploy-smoke-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	worker, err := manager.StartWorker(ctx, orchestrator.WorkerSpec{
		Token: token, TargetURL: target, Screen: "1280x720@30", RenderMode: "cpu", ReadySelector: "#ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if err := manager.Stop(stopCtx, token); err != nil {
			t.Error(err)
		}
	}()
	// A healthy browser alone does not prove that Neko accepts viewer logins.
	// Exercise the same legacy signaling proxy used by the real watching page.
	viewerProxy, err := proxy.NewWebSocketProxy("deployment-test-user-secret", "deployment-test-admin-secret")
	if err != nil {
		t.Fatal(err)
	}
	viewerGateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = viewerProxy.Serve(w, viewport.WithSettings(r, true, 30), worker.SignalURL)
	}))
	defer viewerGateway.Close()
	viewer, _, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(viewerGateway.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	_ = viewer.SetReadDeadline(time.Now().Add(12 * time.Second))
	for {
		var message struct {
			Event   string `json:"event"`
			Message string `json:"message"`
			SDP     string `json:"sdp"`
		}
		if err := viewer.ReadJSON(&message); err != nil {
			viewer.Close()
			t.Fatalf("viewer signaling failed before receiving a video offer: %v", err)
		}
		if message.Event == "system/disconnect" || message.Event == "system/error" {
			viewer.Close()
			t.Fatalf("Neko rejected viewer signaling: %s", message.Message)
		}
		if message.Event == "signal/provide" {
			if !strings.Contains(message.SDP, "m=video") {
				t.Fatal("Neko signaling did not provide a video offer")
			}
			t.Log("Neko accepted the real viewer proxy and provided a video offer")
			break
		}
	}
	response, err := http.Get("http://127.0.0.1:" + strconv.Itoa(worker.CDPPort) + "/json/list")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var pages []struct{ WebSocketDebuggerURL, Type string }
	if err := json.NewDecoder(response.Body).Decode(&pages); err != nil {
		t.Fatal(err)
	}
	for _, page := range pages {
		if page.Type != "page" {
			continue
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, page.WebSocketDebuggerURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		assertBrowserViewport(t, conn, 1280, 720)
		// The actual Xorg resize path must also resize Chromium, not just capture
		// a larger desktop with a small browser and black unused pixels.
		for _, size := range []viewport.Size{
			{Type: "resize", Width: 1600, Height: 800},
			{Type: "resize", Width: 560, Height: 960},
			{Type: "resize", Width: 1920, Height: 1080},
			{Type: "resize", Width: 1280, Height: 720},
		} {
			if err := viewer.WriteJSON(size); err != nil {
				t.Fatal(err)
			}
			assertBrowserViewport(t, conn, size.Width, size.Height)
		}
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteJSON(map[string]any{"id": 1, "method": "Page.captureScreenshot"}); err != nil {
			t.Fatal(err)
		}
		for {
			var message struct {
				ID     int
				Result struct{ Data string }
				Error  json.RawMessage
			}
			if err := conn.ReadJSON(&message); err != nil {
				t.Fatal(err)
			}
			if message.ID != 1 {
				continue
			}
			if len(message.Error) > 0 || len(strings.TrimSpace(message.Result.Data)) < 1000 {
				t.Fatal("Chromium did not return a rendered screenshot")
			}
			data, err := base64.StdEncoding.DecodeString(message.Result.Data)
			if err != nil {
				t.Fatal(err)
			}
			frame, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			r, g, b, _ := frame.At(32, 32).RGBA()
			if g < 25000 || g <= r*2 || g <= b*2 {
				t.Fatalf("expected a green WebGL canvas; got pixel RGB=(%d,%d,%d)", r, g, b)
			}
			t.Log("CPU worker started, test WebGL page became ready, and Chromium returned a screenshot")
			return
		}
	}
	t.Fatal("no Chromium page was found")
}

func assertBrowserViewport(t *testing.T, conn *websocket.Conn, width, height int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var actual struct{ Width, Height, ScreenWidth, ScreenHeight int }
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(deadline)
		if err := conn.WriteJSON(map[string]any{"id": 900, "method": "Runtime.evaluate", "params": map[string]any{
			"expression": "({width:innerWidth,height:innerHeight,screenWidth:screen.width,screenHeight:screen.height})", "returnByValue": true,
		}}); err != nil {
			t.Fatal(err)
		}
		for {
			var message struct {
				ID     int
				Result struct {
					Result struct{ Value json.RawMessage }
				}
			}
			if err := conn.ReadJSON(&message); err != nil {
				t.Fatal(err)
			}
			if message.ID != 900 {
				continue
			}
			if err := json.Unmarshal(message.Result.Result.Value, &actual); err != nil {
				t.Fatal(err)
			}
			break
		}
		if actual.Width == width && actual.Height == height && actual.ScreenWidth == width && actual.ScreenHeight == height {
			t.Logf("Chromium content and Xorg screen both match %dx%d", width, height)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("expected Chromium content and Xorg screen %dx%d; got %+v", width, height, actual)
}
