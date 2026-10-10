//go:build windows

package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gostream3d/internal/viewport"
)

func (w *windowsWorker) dispatchViewportResizes(ctx context.Context, requests <-chan viewport.Size) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.closed:
			return errors.New("local worker stopped")
		case size := <-requests:
			resizeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := w.resizeViewport(resizeCtx, size)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}

func (w *windowsWorker) resizeViewport(ctx context.Context, size viewport.Size) error {
	if !w.Adaptive || !size.Valid() {
		return errors.New("invalid adaptive viewport")
	}
	w.captureMu.Lock()
	defer w.captureMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	unchanged := w.width == size.Width && w.height == size.Height && !w.viewportSettling
	casting := w.captureActive
	if !unchanged {
		w.viewportSettling = true
		w.latest = nil
		for viewer := range w.viewers {
			select {
			case <-viewer.frames:
			default:
			}
		}
	}
	w.mu.Unlock()
	if unchanged {
		return nil
	}
	if casting {
		if _, err := w.call(ctx, "Page.stopScreencast", map[string]any{}); err != nil {
			return err
		}
		w.mu.Lock()
		w.captureActive = false
		w.mu.Unlock()
	}
	if _, err := w.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": size.Width, "height": size.Height, "deviceScaleFactor": 1, "mobile": false,
	}); err != nil {
		return err
	}
	w.mu.Lock()
	w.width, w.height = size.Width, size.Height
	w.mu.Unlock()
	// CDP's metrics reply only confirms the viewport changed. WebGL engines may
	// debounce their canvas resize and recreate postprocessing targets later.
	// Keep the viewer's previous frame until the source has actually repainted.
	result, err := w.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":   viewportReadyExpression(w.targetURL, w.readySelector, size),
		"awaitPromise": true, "returnByValue": true,
	})
	if err != nil {
		// Cancel the browser-side wait too: a disconnected viewer must not leave
		// its temporary GPU frame rate running or restore it over a later budget.
		_ = w.sendNoReply("Runtime.evaluate", map[string]any{"expression": "window.__g3dCancelViewportWait && window.__g3dCancelViewportWait()"})
		return err
	}
	var evaluation struct {
		Result struct {
			Value bool `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(result, &evaluation) != nil || !evaluation.Result.Value {
		return errors.New("scene did not finish repainting after viewport resize")
	}
	w.mu.Lock()
	w.viewportSettling = false
	w.mu.Unlock()
	if casting {
		if _, err := w.call(ctx, "Page.startScreencast", w.screencastParams()); err != nil {
			return err
		}
		w.mu.Lock()
		w.captureActive = true
		w.mu.Unlock()
	}
	w.recordInput()
	return nil
}

func viewportReadyExpression(targetURL, selector string, size viewport.Size) string {
	thingJS := false
	if parsed, err := url.Parse(targetURL); err == nil {
		thingJS = strings.EqualFold(parsed.Hostname(), "campus.thingjs.com")
	}
	return fmt.Sprintf(`(async () => {
  const width = %d, height = %d, thingJS = %t;
  const deadline = performance.now() + 10000;
  const paint = () => new Promise(resolve => requestAnimationFrame(resolve));
  // Wake a preloaded GPU scene so a two-frame repaint does not run at idle FPS.
  const previousFPS = window.__g3dFrameRate;
  if (window.__g3dSetFrameRate) window.__g3dSetFrameRate(Math.max(30, previousFPS || 0));
  let cancelled = false, restored = false;
  const restore = () => {
    if (restored) return;
    restored = true;
    if (window.__g3dCancelViewportWait === cancel) delete window.__g3dCancelViewportWait;
    if (window.__g3dSetFrameRate && Number.isFinite(previousFPS)) window.__g3dSetFrameRate(previousFPS);
  };
  const cancel = () => { cancelled = true; restore(); };
  window.__g3dCancelViewportWait = cancel;
  try {
  let readyPaints = 0, baseline = null;
  while (!cancelled && performance.now() < deadline) {
    await paint();
    if (cancelled) return false;
    if (innerWidth !== width || innerHeight !== height || !(%s)) {
      readyPaints = 0; baseline = null; continue;
    }
    if (thingJS) {
      const canvas = app.renderer && app.renderer.domElement;
      const ratio = app._pixelRatio * devicePixelRatio;
      if (!canvas || app.resizeLock || app.width !== width || app.height !== height ||
          canvas.width !== Math.floor(width * ratio) || canvas.height !== Math.floor(height * ratio)) {
        baseline = null; readyPaints = 0; continue;
      }
      // A browser animation callback is not proof that the 3D engine rendered.
      // Count actual scene frames after the drawing buffer reaches its new size.
      if (baseline === null) baseline = app._currentFrame;
      if (app._currentFrame - baseline < 2) continue;
    }
    if (++readyPaints >= 2) return true;
  }
  return false;
  } finally {
    restore();
  }
})()`, size.Width, size.Height, thingJS, readinessExpression(targetURL, selector))
}
