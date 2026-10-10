package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

func verifyHardwareGPU(parent context.Context, bindIP string, port int) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	endpoint := "http://" + net.JoinHostPort(bindIP, strconv.Itoa(port))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/json/list", nil)
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var targets []devtoolsTarget
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&targets); err != nil {
		return "", err
	}
	for _, target := range targets {
		if target.Type != "page" || target.URL == "about:blank" {
			continue
		}
		parsed, err := url.Parse(target.WebSocketDebuggerURL)
		if err != nil || parsed.Scheme != "ws" {
			continue
		}
		parsed.Host = net.JoinHostPort(bindIP, strconv.Itoa(port))
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, parsed.String(), nil)
		if err != nil {
			return "", err
		}
		defer conn.Close()
		_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		expression := `(()=>{const c=document.createElement('canvas'),g=c.getContext('webgl2')||c.getContext('webgl');if(!g)return '';const d=g.getExtension('WEBGL_debug_renderer_info');const name=d?g.getParameter(d.UNMASKED_RENDERER_WEBGL):'';g.getExtension('WEBGL_lose_context')?.loseContext();return name})()`
		if err := conn.WriteJSON(map[string]any{"id": 1, "method": "Runtime.evaluate", "params": map[string]any{"expression": expression, "returnByValue": true}}); err != nil {
			return "", err
		}
		for {
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var message struct {
				ID     int             `json:"id"`
				Error  json.RawMessage `json:"error"`
				Result struct {
					Result struct {
						Value string `json:"value"`
					} `json:"result"`
				} `json:"result"`
			}
			if err := conn.ReadJSON(&message); err != nil {
				return "", err
			}
			if message.ID != 1 {
				continue
			}
			renderer := message.Result.Result.Value
			if len(message.Error) > 0 || !hardwareRenderer(renderer) {
				return "", fmt.Errorf("GPU requested but hardware WebGL is unavailable (renderer=%q); refusing CPU fallback", renderer)
			}
			return renderer, nil
		}
	}
	return "", errors.New("GPU verification found no Chromium page")
}
