package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"gostream3d/internal/viewport"
	"time"
)

// A short-lived, server-only admin connection performs one bounded screen
// operation. The viewer retains its ordinary user permissions and never sees
// credentials or administrator messages. Neko dynamically creates Xorg modes
// and restarts capture on screen changes without reloading the browser scene.
func (p *WebSocketProxy) setViewport(ctx context.Context, base string, size viewport.Size, fps int) error {
	if !size.Valid() || (fps != 30 && fps != 60) || p.adminPassword == "" {
		return errors.New("invalid Neko viewport settings")
	}
	endpoint, err := nekoURL(base, p.adminPassword)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, response, err := p.dialer.DialContext(ctx, endpoint, nil)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	conn.SetReadLimit(maxMessageBytes)
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteJSON(map[string]any{"event": "screen/set", "width": size.Width, "height": size.Height, "rate": fps}); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var result struct {
			Event  string `json:"event"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		}
		if json.Unmarshal(payload, &result) != nil {
			continue
		}
		if result.Event == "system/error" {
			return errors.New("Neko screen change rejected")
		}
		if result.Event == "screen/resolution" && result.Width == size.Width && result.Height == size.Height {
			return nil
		}
	}
}
