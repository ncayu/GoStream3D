package orchestrator

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed gpu_depth.js
var gpuDepthScript string

func gpuDepthProfileRequired(mode, target string) bool {
	u, err := url.Parse(target)
	return err == nil && mode == "nvidia" && u.Hostname() != "" &&
		(u.Scheme == "http" || u.Scheme == "https")
}

// Install before navigation so the first render target and subsequent resizes
// use the same precision, independently of the source platform or hostname.
// Only Linux GPU workers enter this path; CPU workers retain their own settings.
func initializeGPUPage(ctx context.Context, bindIP string, port int, targetURL string) (io.Closer, error) {
	endpoint := "http://" + net.JoinHostPort(bindIP, strconv.Itoa(port)) + "/json/list"
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err == nil {
			var targets []devtoolsTarget
			if resp.StatusCode == http.StatusOK {
				err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&targets)
			} else {
				err = fmt.Errorf("DevTools returned HTTP %d", resp.StatusCode)
			}
			resp.Body.Close()
			if err == nil {
				for _, t := range targets {
					if t.Type != "page" || t.WebSocketDebuggerURL == "" {
						continue
					}
					return navigateGPUPage(ctx, bindIP, port, t.WebSocketDebuggerURL, targetURL)
				}
			}
		}
		if err != nil {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return nil, errors.Join(ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}

func navigateGPUPage(ctx context.Context, bindIP string, port int, wsURL, targetURL string) (io.Closer, error) {
	parsed, err := url.Parse(wsURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return nil, errors.New("invalid DevTools WebSocket URL")
	}
	parsed.Host = net.JoinHostPort(bindIP, strconv.Itoa(port))
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	control := newGPUPageControl(conn)
	handedOff := false
	defer func() {
		if !handedOff {
			control.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	commands := []struct {
		method string
		params map[string]any
	}{
		{"Page.enable", map[string]any{}},
		{"Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": gpuDepthScript}},
		{"Target.setAutoAttach", gpuFrameAttachParams()},
		{"Page.navigate", map[string]any{"url": targetURL}},
	}
	for _, c := range commands {
		if err := control.command(ctx, "", c.method, c.params); err != nil {
			return nil, err
		}
	}
	// New-document scripts belong to their CDP session. Keep that session alive
	// for source-page reloads and navigation; the worker owns and closes it.
	handedOff = true
	return control, nil
}

// Only out-of-process frames need a separate Page session. Same-process frames
// inherit the page's new-document script. Leave workers and other targets alone.
func gpuFrameAttachParams() map[string]any {
	return map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true,
		"filter": []map[string]any{{"type": "iframe"}, {"exclude": true}}}
}

type gpuCDPReply struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Error  json.RawMessage `json:"error"`
	Result struct {
		ErrorText string `json:"errorText"`
	} `json:"result"`
	Params struct {
		SessionID  string `json:"sessionId"`
		TargetInfo struct {
			Type string `json:"type"`
		} `json:"targetInfo"`
	} `json:"params"`
}

// CDP replies and iframe attachment events share the same socket. A single
// reader dispatches replies so a child can initialize while the page navigates.
type gpuPageControl struct {
	conn      *websocket.Conn
	ctx       context.Context
	cancel    context.CancelFunc
	nextID    atomic.Int64
	writes    sync.Mutex
	mu        sync.Mutex
	pending   map[int64]chan gpuCDPReply
	closeOnce sync.Once
}

func newGPUPageControl(conn *websocket.Conn) *gpuPageControl {
	ctx, cancel := context.WithCancel(context.Background())
	c := &gpuPageControl{conn: conn, ctx: ctx, cancel: cancel, pending: make(map[int64]chan gpuCDPReply)}
	go c.read()
	return c
}

func (c *gpuPageControl) Close() error {
	var err error
	c.closeOnce.Do(func() { c.cancel(); err = c.conn.Close() })
	return err
}

func (c *gpuPageControl) command(ctx context.Context, session, method string, params map[string]any) error {
	id := c.nextID.Add(1)
	reply := make(chan gpuCDPReply, 1)
	c.mu.Lock()
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	request := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		request["sessionId"] = session
	}
	c.writes.Lock()
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.conn.SetWriteDeadline(deadline)
	err := c.conn.WriteJSON(request)
	c.writes.Unlock()
	if err != nil {
		return err
	}
	select {
	case r := <-reply:
		if len(r.Error) > 0 || r.Result.ErrorText != "" {
			return fmt.Errorf("%s failed: %s %s", method, r.Error, r.Result.ErrorText)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return io.EOF
	}
}

func (c *gpuPageControl) read() {
	defer c.Close()
	for {
		var r gpuCDPReply
		if c.conn.ReadJSON(&r) != nil {
			return
		}
		if r.ID != 0 {
			c.mu.Lock()
			reply := c.pending[int64(r.ID)]
			c.mu.Unlock()
			if reply != nil {
				select {
				case reply <- r:
				default:
				}
			}
		} else if r.Method == "Target.attachedToTarget" && r.Params.TargetInfo.Type == "iframe" {
			go c.initializeFrame(r.Params.SessionID)
		}
	}
}

func (c *gpuPageControl) initializeFrame(session string) {
	// Always resume a paused frame, including when initialization fails or a
	// frame detaches during navigation; never leave source content frozen.
	defer func() {
		ctx, cancel := context.WithTimeout(c.ctx, 2*time.Second)
		defer cancel()
		_ = c.command(ctx, session, "Runtime.runIfWaitingForDebugger", map[string]any{})
	}()
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	for _, cmd := range []struct {
		method string
		params map[string]any
	}{
		{"Page.enable", map[string]any{}},
		{"Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": gpuDepthScript}},
		{"Target.setAutoAttach", gpuFrameAttachParams()},
	} {
		if err := c.command(ctx, session, cmd.method, cmd.params); err != nil {
			if c.ctx.Err() == nil {
				slog.Warn("GPU iframe depth profile unavailable", "error", err)
			}
			return
		}
	}
}
