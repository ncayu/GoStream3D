//go:build windows

package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWindowsInputQueuePreservesTransitionsAndLatestMove(t *testing.T) {
	q := newWindowsInputQueue()
	ctx := context.Background()
	push := func(kind string, x, buttons int) {
		t.Helper()
		if err := q.push(ctx, windowsInputCommand{kind: kind, params: map[string]any{"x": x, "buttons": buttons}}); err != nil {
			t.Fatal(err)
		}
	}
	push("down", 0, 1)
	for i := 1; i <= 1000; i++ {
		push("move", i, 1)
	}
	push("up", 1000, 0)
	push("keyDown", 0, 0)
	push("move", 1001, 0)
	for i, expected := range []struct {
		kind string
		x    int
	}{{"down", 0}, {"move", 1000}, {"up", 1000}, {"keyDown", 0}, {"move", 1001}} {
		got, err := q.pop(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got.kind != expected.kind || got.params["x"] != expected.x {
			t.Fatalf("event %d: got %+v, expected %+v", i, got, expected)
		}
	}
	if len(q.items) != 0 {
		t.Fatal("stale movement retained")
	}
}

func TestWindowsCPUInputPipelineKeepsClickResponsiveAndBounded(t *testing.T) {
	received := make(chan cdpMessage, 16)
	acknowledge := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(rw, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		requests := make(chan cdpMessage, 16)
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-acknowledge:
			case <-done:
				return
			}
			for {
				select {
				case request := <-requests:
					if conn.WriteJSON(map[string]any{"id": request.ID, "result": map[string]any{}}) != nil {
						return
					}
				case <-done:
					return
				}
			}
		}()
		for {
			var request cdpMessage
			if conn.ReadJSON(&request) != nil {
				return
			}
			received <- request
			requests <- request
		}
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	w := &windowsWorker{cdp: conn, closed: make(chan struct{}), pending: make(map[int]chan cdpMessage)}
	go w.readCDP()
	q := newWindowsInputQueue()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, kind := range []string{"down", "up", "wheel", "keyDown", "keyUp"} {
		if err := q.push(ctx, windowsInputCommand{kind: kind, method: kind, params: map[string]any{}}); err != nil {
			t.Fatal(err)
		}
	}
	finished := make(chan error, 1)
	go func() { finished <- w.dispatchCPUInputs(ctx, q) }()
	for _, expected := range []string{"down", "up", "wheel", "keyDown"} {
		select {
		case message := <-received:
			if message.Method != expected {
				t.Fatalf("out of order: %s, want %s", message.Method, expected)
			}
		case <-time.After(time.Second):
			t.Fatal("click release waited for the press acknowledgement")
		}
	}
	select {
	case <-received:
		t.Fatal("more than four inputs queued inside the browser")
	case <-time.After(100 * time.Millisecond):
	}
	close(acknowledge)
	select {
	case message := <-received:
		if message.Method != "keyUp" {
			t.Fatal("lost key release")
		}
	case <-time.After(time.Second):
		t.Fatal("pipeline did not resume after acknowledgement")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("pending replies leaked after cancellation")
	}
	w.mu.Lock()
	retained := len(w.pending)
	w.mu.Unlock()
	if retained != 0 {
		t.Fatal("pending CDP requests retained after cancellation")
	}

	// Browser acknowledgements alone must not release another CPU move;
	// slow rendering needs time to paint before more scene work is queued.
	q = newWindowsInputQueue()
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	go func() { finished <- w.dispatchCPUInputs(ctx, q) }()
	pushMove := func(x int) {
		t.Helper()
		if err := q.push(ctx, windowsInputCommand{kind: "move", method: "move", params: map[string]any{"x": x, "buttons": 1}}); err != nil {
			t.Fatal(err)
		}
	}
	pushMove(1)
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("first move was not sent")
	}
	pushMove(2)
	time.Sleep(30 * time.Millisecond)
	for x := 3; x <= 1000; x++ {
		pushMove(x)
	}
	select {
	case <-received:
		t.Fatal("moves flooded the browser without a new frame")
	case <-time.After(100 * time.Millisecond):
	}
	w.mu.Lock()
	w.frameCount++
	w.mu.Unlock()
	select {
	case message := <-received:
		var params map[string]any
		if err := json.Unmarshal(message.Params, &params); err != nil {
			t.Fatal(err)
		}
		if params["x"] != float64(1000) {
			t.Fatalf("stale move replayed: %v", params)
		}
	case <-time.After(time.Second):
		t.Fatal("new frame did not release the latest move")
	}
	pushMove(1001)
	time.Sleep(30 * time.Millisecond)
	if err := q.push(ctx, windowsInputCommand{kind: "up", method: "up", params: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-received:
		if message.Method != "move" {
			t.Fatal("position was not flushed before release")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("position flush waited for a slow frame")
	}
	select {
	case message := <-received:
		if message.Method != "up" {
			t.Fatal("lost button release")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("button release waited for a frame")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("paced dispatcher leaked after cancellation")
	}
}

func TestWindowsInputQueueCancellationUnblocksBackpressure(t *testing.T) {
	q := newWindowsInputQueue()
	ctx, cancel := context.WithCancel(context.Background())
	for i := 0; i < 64; i++ {
		if err := q.push(ctx, windowsInputCommand{kind: "keyDown"}); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	if err := q.push(ctx, windowsInputCommand{kind: "keyUp"}); err == nil {
		t.Fatal("full queue ignored cancellation")
	}
	if _, err := q.pop(ctx); err == nil {
		t.Fatal("cancelled queue dispatched stale input")
	}
}

func TestWindowsInputQueueKeepsWheelTravelAndDirection(t *testing.T) {
	q := newWindowsInputQueue()
	ctx := context.Background()
	for _, delta := range []float64{120, 120, -60} {
		if err := q.push(ctx, windowsInputCommand{kind: "wheel", params: map[string]any{"buttons": 0, "deltaX": float64(0), "deltaY": delta}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, expected := range []float64{120, 120, -60} {
		got, err := q.pop(ctx)
		if err != nil || got.params["deltaY"] != expected {
			t.Fatalf("wheel: %+v, %v; expected %v", got, err, expected)
		}
	}
}
