//go:build windows

package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"gostream3d/internal/monitor"
	"image/jpeg"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type capacityReceiver struct {
	mu                 sync.Mutex
	frames, bytes      uint64
	failed             bool
	samples            []float64
	counts, byteCounts []uint64
}

// Real copies of the selected scene share the production allocator and budget.
// Receivers run on loopback and decode JPEG in Go; this measures server-side
// capacity and cannot stand in for a remote browser or an interactive camera.
func (m *WindowsProcessManager) RunCapacityStage(ctx context.Context, info SessionInfo, count int, duration time.Duration) (stage CapacityStage, err error) {
	stage = CapacityStage{Streams: count, Measurements: []CapacityStream{}}
	clients := []*websocket.Conn{}
	tokens := []string{}
	receivers := []*capacityReceiver{}
	workers := map[string]Worker{}
	var workersMu sync.RWMutex
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return stage, e
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		workersMu.RLock()
		w, ok := workers[r.URL.Path]
		workersMu.RUnlock()
		if !ok {
			http.NotFound(rw, r)
			return
		}
		_ = m.Serve(rw, r, w.SignalURL)
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		for _, c := range clients {
			_ = c.Close()
		}
		_ = server.Close()
		var wg sync.WaitGroup
		var cleanupMu sync.Mutex
		for _, token := range tokens {
			wg.Add(1)
			go func(token string) {
				defer wg.Done()
				stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if e := m.Stop(stopCtx, token); e != nil {
					cleanupMu.Lock()
					err = errors.Join(err, fmt.Errorf("测试实例 %s 清理失败：%w", token, e))
					cleanupMu.Unlock()
				} else {
					root, e := filepath.Abs(m.cfg.WorkRoot)
					if e == nil && filepath.Base(token) == token && strings.HasPrefix(token, "capacity-") {
						if e = os.RemoveAll(filepath.Join(root, token)); e != nil {
							cleanupMu.Lock()
							err = errors.Join(err, fmt.Errorf("测试缓存清理失败：%w", e))
							cleanupMu.Unlock()
						}
					}
				}
			}(token)
		}
		wg.Wait()
	}()
	for i := 0; i < count; i++ {
		token, e := newChannelToken("capacity")
		if e != nil {
			return stage, e
		}
		token = "capacity-" + token
		spec := channelSpec(info, token)
		spec.FramePolicy = "animation"
		spec.ProjectToken = "capacity:" + info.Token
		tokens = append(tokens, token) // Also clean an interrupted/partially started worker.
		w, e := m.StartWorker(ctx, spec)
		if e != nil {
			return stage, e
		}
		workersMu.Lock()
		workers["/"+token] = w
		workersMu.Unlock()
		conn, _, e := websocket.DefaultDialer.DialContext(ctx, "ws://"+listener.Addr().String()+"/"+token, nil)
		if e != nil {
			return stage, e
		}
		clients = append(clients, conn)
		conn.SetReadLimit(32 << 20)
		receiver := &capacityReceiver{}
		receivers = append(receivers, receiver)
		if e = conn.WriteJSON(map[string]any{"type": "telemetry"}); e != nil {
			return stage, e
		}
		go func() {
			var frameID uint64
			for {
				kind, data, e := conn.ReadMessage()
				if e != nil {
					receiver.mu.Lock()
					receiver.failed = true
					receiver.mu.Unlock()
					return
				}
				if kind == websocket.TextMessage {
					var meta struct {
						ID uint64 `json:"id"`
					}
					_ = json.Unmarshal(data, &meta)
					frameID = meta.ID
					continue
				}
				if kind != websocket.BinaryMessage {
					continue
				}
				if _, e = jpeg.Decode(bytes.NewReader(data)); e != nil {
					continue
				}
				receiver.mu.Lock()
				receiver.frames++
				receiver.bytes += uint64(len(data))
				receiver.mu.Unlock()
				if frameID > 0 {
					_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					if e = conn.WriteJSON(map[string]any{"type": "frameAck", "frameID": frameID}); e != nil {
						return
					}
				}
			}
		}()
	}
	select {
	case <-ctx.Done():
		return stage, ctx.Err()
	case <-time.After(10 * time.Second):
	}
	for _, r := range receivers {
		r.mu.Lock()
		r.frames = 0
		r.bytes = 0
		r.mu.Unlock()
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	last := time.Now()
	samples := int(duration / time.Second)
	for j := 0; j < samples; j++ {
		select {
		case <-ctx.Done():
			return stage, ctx.Err()
		case <-ticker.C:
		}
		elapsed := time.Since(last).Seconds()
		last = time.Now()
		for _, r := range receivers {
			r.mu.Lock()
			n, b, failed := r.frames, r.bytes, r.failed
			r.frames = 0
			r.bytes = 0
			r.samples = append(r.samples, float64(n)/elapsed)
			r.counts = append(r.counts, n)
			r.byteCounts = append(r.byteCounts, b)
			r.mu.Unlock()
			if failed {
				return stage, errors.New("测试连接中断，无法给出完整容量建议")
			}
		}
	}
	byToken := map[string]StreamPerformance{}
	for _, p := range m.PerformanceSnapshot() {
		byToken[p.Token] = p
	}
	stage.OtherWorkers = len(byToken) - len(tokens)
	stage.GPUsAtEnd, _ = monitor.SampleGPUs(ctx)
	for i, r := range receivers {
		sum, byteSum := float64(0), uint64(0)
		for _, fps := range r.samples {
			sum += fps
		}
		for _, b := range r.byteCounts {
			byteSum += b
		}
		p := byToken[tokens[i]]
		stage.Measurements = append(stage.Measurements, CapacityStream{Token: tokens[i], AverageFPS: sum / float64(len(r.samples)), LowFPS: lowFPS(r.samples), RenderFPS: p.RenderFPS, CaptureFPS: p.CaptureFPS, WriteMS: p.WriteMS, BytesPerSecond: float64(byteSum) / duration.Seconds(), Samples: len(r.samples)})
	}
	return stage, nil
}

var _ CapacityRunner = (*WindowsProcessManager)(nil)
