//go:build windows

package orchestrator

import (
	"context"
	"sync"
	"time"
)

type windowsInputCommand struct {
	method string
	params map[string]any
	kind   string
	sent   func()
}

// A mouse press must not wait for its slow compositor acknowledgement before
// the corresponding release can reach the page. Keep four requests in flight,
// preserving send order and bounding browser work; pending moves still merge.
func (w *windowsWorker) dispatchCPUInputs(parent context.Context, q *windowsInputQueue) error {
	ctx, cancel := context.WithCancelCause(parent)
	slots := make(chan struct{}, 4)
	var replies sync.WaitGroup
	defer func() { cancel(nil); replies.Wait() }()
	var lastMoveFrame uint64
	var lastMoveAt time.Time
	for {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
		command, err := q.pop(ctx)
		if err != nil {
			return context.Cause(ctx)
		}
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if command.kind == "move" {
			// CDP may acknowledge a move before the 3D program has painted.
			// Sending dozens of such moves starves slow software frames. Keep
			// one move per delivered frame, with a one-second stall fallback.
			if !lastMoveAt.IsZero() {
				ticker := time.NewTicker(25 * time.Millisecond)
				for time.Since(lastMoveAt) < time.Second {
					// Flush a pending position before clicks/keys immediately;
					// a slow frame must not keep a button held after release.
					if q.hasButtonOrKeyTransition() {
						break
					}
					w.mu.Lock()
					changed := w.frameCount != lastMoveFrame
					w.mu.Unlock()
					if changed {
						break
					}
					select {
					case <-ctx.Done():
						ticker.Stop()
						return context.Cause(ctx)
					case <-ticker.C:
					}
				}
				ticker.Stop()
			}
			command = q.latestConsecutiveMove(command)
			w.mu.Lock()
			lastMoveFrame = w.frameCount
			w.mu.Unlock()
			lastMoveAt = time.Now()
		}
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		id, result, err := w.beginCall(command.method, command.params)
		if err != nil {
			return err
		}
		if command.sent != nil {
			command.sent()
		}
		replies.Add(1)
		go func() {
			defer replies.Done()
			defer func() { <-slots }()
			replyCtx, stop := context.WithTimeout(ctx, 15*time.Second)
			defer stop()
			if _, err := w.awaitCall(replyCtx, id, result); err != nil {
				cancel(err)
			}
		}()
	}
}

func (q *windowsInputQueue) hasButtonOrKeyTransition() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, command := range q.items {
		if command.kind == "move" {
			continue
		}
		return command.kind == "down" || command.kind == "up" || command.kind == "keyDown" || command.kind == "keyUp" || command.kind == "text"
	}
	return false
}

func (q *windowsInputQueue) latestConsecutiveMove(command windowsInputCommand) windowsInputCommand {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) > 0 && q.items[0].kind == "move" && q.items[0].params["buttons"] == command.params["buttons"] {
		command = q.items[0]
		q.items[0] = windowsInputCommand{}
		q.items = q.items[1:]
		select {
		case q.space <- struct{}{}:
		default:
		}
	}
	return command
}

// Bound queued inputs when a software renderer takes longer to process an
// event than the mouse takes to produce it. Button/key transitions retain
// their order; consecutive moves retain only the latest position.
type windowsInputQueue struct {
	mu    sync.Mutex
	items []windowsInputCommand
	ready chan struct{}
	space chan struct{}
}

func newWindowsInputQueue() *windowsInputQueue {
	return &windowsInputQueue{ready: make(chan struct{}, 1), space: make(chan struct{}, 1)}
}

func (q *windowsInputQueue) push(ctx context.Context, command windowsInputCommand) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		q.mu.Lock()
		if len(q.items) > 0 && command.kind == "move" {
			last := &q.items[len(q.items)-1]
			if last.kind == "move" && last.params["buttons"] == command.params["buttons"] {
				*last = command
				q.mu.Unlock()
				return nil
			}
		}
		if len(q.items) < 64 {
			q.items = append(q.items, command)
			q.mu.Unlock()
			select {
			case q.ready <- struct{}{}:
			default:
			}
			return nil
		}
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-q.space:
		}
	}
}

func (q *windowsInputQueue) pop(ctx context.Context) (windowsInputCommand, error) {
	for {
		if err := ctx.Err(); err != nil {
			return windowsInputCommand{}, err
		}
		q.mu.Lock()
		if len(q.items) > 0 {
			command := q.items[0]
			q.items[0] = windowsInputCommand{}
			q.items = q.items[1:]
			q.mu.Unlock()
			select {
			case q.space <- struct{}{}:
			default:
			}
			return command, nil
		}
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return windowsInputCommand{}, ctx.Err()
		case <-q.ready:
		}
	}
}
