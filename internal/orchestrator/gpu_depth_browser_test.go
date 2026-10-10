package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Opt-in integration test, launched by web/scripts/gpu-depth-browser-test.mjs.
// It exercises the actual production CDP controller, including isolated frames.
func TestGPUPageRealBrowser(t *testing.T) {
	target := os.Getenv("GOSTREAM3D_GPU_DEPTH_TEST_URL")
	if target == "" {
		t.Skip("real browser not requested")
	}
	port, err := strconv.Atoi(os.Getenv("GOSTREAM3D_GPU_DEPTH_CDP_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	control, err := initializeGPUPage(ctx, "127.0.0.1", port, target)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
	if err != nil {
		t.Fatal(err)
	}
	var targets []devtoolsTarget
	err = json.NewDecoder(resp.Body).Decode(&targets)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var ws string
	for _, p := range targets {
		if p.Type == "page" {
			ws = p.WebSocketDebuggerURL
			break
		}
	}
	driver, _, err := websocket.DefaultDialer.Dial(ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	_ = driver.SetReadDeadline(time.Now().Add(30 * time.Second))
	id := 0
	rpc := func(method string, params map[string]any) json.RawMessage {
		t.Helper()
		id++
		if err := driver.WriteJSON(map[string]any{"id": id, "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
		for {
			var reply struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if err := driver.ReadJSON(&reply); err != nil {
				t.Fatal(err)
			}
			if reply.ID == id {
				if len(reply.Error) > 0 {
					t.Fatal(string(reply.Error))
				}
				return reply.Result
			}
		}
	}
	evaluate := func(expression string) json.RawMessage {
		t.Helper()
		var reply struct {
			Result struct {
				Value json.RawMessage `json:"value"`
			} `json:"result"`
			Exception json.RawMessage `json:"exceptionDetails"`
		}
		if err := json.Unmarshal(rpc("Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true}), &reply); err != nil {
			t.Fatal(err)
		}
		if len(reply.Exception) > 0 {
			t.Fatal(string(reply.Exception))
		}
		return reply.Result.Value
	}
	wait := func(expression string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if string(evaluate(expression)) == "true" {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("browser did not become ready: " + expression)
	}
	reports := []map[string]any{}
	verify := func(expression, label string) {
		t.Helper()
		raw := evaluate(expression)
		var r struct {
			Bits        int                               `json:"bits"`
			Pixel       []int                             `json:"pixel"`
			Error       int                               `json:"error"`
			Offscreen   int                               `json:"offscreenBits"`
			Legacy      int                               `json:"legacyBits"`
			LegacyError int                               `json:"legacyError"`
			Texture     bool                              `json:"textureComplete"`
			CopyError   int                               `json:"depthCopyError"`
			CopyBits    int                               `json:"depthCopyBits"`
			Formats     []struct{ Requested, Actual int } `json:"preserved"`
			Samples     []struct {
				Requested, Expected, Actual int
				Complete, Unsupported       bool
			} `json:"msaa"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatal(err)
		}
		if r.Bits != 24 || fmt.Sprint(r.Pixel) != "[0 255 0 255]" || r.Error != 0 || r.Offscreen != 24 || r.Legacy != 16 || r.LegacyError != 0 || !r.Texture || r.CopyError != 0 || r.CopyBits != 16 {
			t.Fatalf("%s: %s", label, raw)
		}
		for _, f := range r.Formats {
			if f.Requested != f.Actual {
				t.Fatalf("%s changed another format: %s", label, raw)
			}
		}
		for _, s := range r.Samples {
			if !s.Unsupported && (s.Expected != s.Actual || !s.Complete) {
				t.Fatalf("%s changed MSAA count or broke framebuffer: %s", label, raw)
			}
		}
		var report map[string]any
		_ = json.Unmarshal(raw, &report)
		report["label"] = label
		reports = append(reports, report)
	}
	wait("typeof window.depthProbe === 'function'")
	verify("depthProbe()", "first navigation")
	previousDocument := evaluate("window.__depthSceneID")
	rpc("Page.reload", map[string]any{})
	wait("typeof window.depthProbe === 'function' && window.__depthSceneID !== " + string(previousDocument))
	verify("depthProbe()", "reload")
	evaluate("resizeScene(1600,900)")
	verify("depthProbe()", "resize")
	alternate, _ := url.Parse(target)
	alternate.Host = strings.Replace(alternate.Host, "127.0.0.1", "localhost", 1)
	rpc("Page.navigate", map[string]any{"url": alternate.String()})
	alternateJSON, _ := json.Marshal(alternate.String())
	wait("typeof window.depthProbe === 'function' && location.href === " + string(alternateJSON))
	verify("depthProbe()", "different hostname")
	encoded, _ := json.Marshal(target)
	evaluate("window.__iframeReport=null;addEventListener('message',e=>{if(e.data.depthReport)window.__iframeReport=e.data.depthReport});const f=document.createElement('iframe');f.src=" + string(encoded) + ";document.body.append(f)")
	wait("!!window.__iframeReport")
	verify("window.__iframeReport", "cross-origin iframe")
	evaluate("window.__nestedReport=null;addEventListener('message',e=>{if(e.data.nestedDepthReport)window.__nestedReport=e.data.nestedDepthReport});document.querySelector('iframe').contentWindow.postMessage({createNestedFrame:" + string(alternateJSON) + "},'*')")
	wait("!!window.__nestedReport")
	verify("window.__nestedReport", "nested cross-origin iframe")
	if output := os.Getenv("GOSTREAM3D_GPU_DEPTH_REPORT"); output != "" {
		data, _ := json.MarshalIndent(map[string]any{"checks": reports}, "", "  ")
		if err := os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
