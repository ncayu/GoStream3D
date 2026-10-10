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
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type devtoolsTarget struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

var ErrInvalidReadySelector = errors.New("invalid ready_selector CSS syntax")

func waitForPage(parent context.Context, bindIP string, port int, selector string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	endpoint := "http://" + net.JoinHostPort(bindIP, strconv.Itoa(port)) + "/json/list"
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		ready, err := pageReady(ctx, client, endpoint, bindIP, port, selector)
		if errors.Is(err, ErrInvalidReadySelector) {
			return err
		}
		if err != nil {
			lastErr = err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("%w (last probe: %v)", ctx.Err(), lastErr)
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func pageReady(ctx context.Context, client *http.Client, endpoint, bindIP string, port int, selector string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("DevTools returned HTTP %d", resp.StatusCode)
	}
	var targets []devtoolsTarget
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&targets); err != nil {
		return false, err
	}
	for _, target := range targets {
		if target.Type != "page" || target.WebSocketDebuggerURL == "" || target.URL == "" || target.URL == "about:blank" {
			continue
		}
		parsed, err := url.Parse(target.WebSocketDebuggerURL)
		if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") {
			continue
		}
		parsed.Host = net.JoinHostPort(bindIP, strconv.Itoa(port))
		return evaluatePage(ctx, parsed.String(), selector)
	}
	return false, nil
}

func evaluatePage(ctx context.Context, wsURL, selector string) (bool, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, _, err := websocket.DefaultDialer.DialContext(probeCtx, wsURL, nil)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	expression := "document.readyState === 'complete'"
	if selector != "" {
		quoted, err := json.Marshal(selector)
		if err != nil {
			return false, err
		}
		expression += " && document.querySelector(" + string(quoted) + ") !== null"
	}
	request := map[string]any{"id": 1, "method": "Runtime.evaluate", "params": map[string]any{"expression": expression, "returnByValue": true}}
	if err := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return false, err
	}
	if err := conn.WriteJSON(request); err != nil {
		return false, err
	}
	for {
		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			return false, err
		}
		var message struct {
			ID    int `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result struct {
				ExceptionDetails json.RawMessage `json:"exceptionDetails"`
				Result           struct {
					Value bool `json:"value"`
				} `json:"result"`
			} `json:"result"`
		}
		if err := conn.ReadJSON(&message); err != nil {
			return false, err
		}
		if message.ID != 1 {
			continue
		}
		if message.Error != nil {
			return false, errors.New(message.Error.Message)
		}
		if len(message.Result.ExceptionDetails) != 0 {
			return false, ErrInvalidReadySelector
		}
		return message.Result.Result.Value, nil
	}
}

func validateReadySelector(selector string) error {
	if len(selector) > 512 || strings.ContainsAny(selector, "\x00\r\n") {
		return errors.New("invalid ready_selector")
	}
	return nil
}
