//go:build windows

package orchestrator

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/gorilla/websocket"
	"image"
	"image/jpeg"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInputFeedbackRequiresAssociatedNewFrame(t *testing.T) {
	w := &windowsWorker{}
	v := &windowsViewer{}
	w.frameCount = 10
	w.markInputSent(v, 7)
	if v.inputID != 7 || v.inputFrame != 10 {
		t.Fatal("input not associated with current capture generation")
	}
	v.sentFrames[1] = sentFrameSample{id: 1, at: time.Now(), inputID: 7}
	w.recordFrameFeedback(v, 1, 1, 1, 30, 0, 120)
	if w.performance.InputFrameMS != 120 || w.performance.InputSampleAt.IsZero() {
		t.Fatal(w.performance)
	}
	v.sentFrames[2] = sentFrameSample{id: 2, at: time.Now()}
	w.recordFrameFeedback(v, 2, 1, 1, 30, 0, 1)
	if w.performance.InputFrameMS != 120 {
		t.Fatal("unassociated frame altered input latency")
	}
	w.markInputSent(v, 6)
	if v.inputID != 7 {
		t.Fatal("old input rewound metadata")
	}
}

func TestWindowsFrameFeedbackIsBoundedVerifiedAndDoesNotCountAsInput(t *testing.T) {
	w := &windowsWorker{performance: StreamPerformance{Token: "test"}, lastInputAt: time.Now().Add(-time.Hour)}
	v := &windowsViewer{}
	v.sentFrames[7] = sentFrameSample{id: 7, at: time.Now().Add(-100 * time.Millisecond)}
	w.recordFrameFeedback(v, 99, 4, 1, 30, 2)
	if !w.performance.FeedbackAt.IsZero() {
		t.Fatal("unverified feedback accepted")
	}
	w.recordFrameFeedback(v, 7, math.NaN(), 1, 30, 2)
	if !w.performance.FeedbackAt.IsZero() {
		t.Fatal("invalid metrics accepted")
	}
	w.recordFrameFeedback(v, 7, 4, 1, 30, 2)
	if w.performance.FrameAckMS < 100 || w.performance.DecodeMS != 4 || w.performance.DisplayFPS != 30 || w.performance.ClientDropped != 2 {
		t.Fatal(w.performance)
	}
	before := w.performance
	w.recordFrameFeedback(v, 7, 9000, 9000, 240, 999)
	if before != w.performance {
		t.Fatal("duplicate feedback skewed metrics")
	}
	if time.Since(w.lastInputAt) < time.Minute {
		t.Fatal("telemetry raised interaction priority")
	}
}

func TestWindowsTelemetryNegotiationKeepsLegacyBinaryFrames(t *testing.T) {
	w := &windowsWorker{Worker: Worker{Token: "telemetry", RenderMode: "nvidia"}, closed: make(chan struct{}), viewers: make(map[*windowsViewer]struct{}), captureWake: make(chan struct{}, 1)}
	m := &WindowsProcessManager{active: map[string]*windowsWorker{w.Token: w}}
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { _ = m.Serve(rw, r, "local-cdp://telemetry") }))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var encoded bytes.Buffer
	_ = jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 64, 64)), nil)
	frame := base64.StdEncoding.EncodeToString(encoded.Bytes())
	w.publishFrame(frame)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	typeID, _, err := conn.ReadMessage()
	if err != nil || typeID != websocket.BinaryMessage {
		t.Fatal("legacy frame changed", typeID, err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "telemetry"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		w.mu.Lock()
		enabled := false
		for v := range w.viewers {
			enabled = v.telemetry
		}
		w.mu.Unlock()
		if enabled {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("telemetry was not enabled")
		}
		time.Sleep(time.Millisecond)
	}
	w.publishFrame(frame)
	var metadata struct {
		Type string `json:"type"`
		ID   uint64 `json:"id"`
	}
	if err := conn.ReadJSON(&metadata); err != nil || metadata.Type != "frame" || metadata.ID == 0 {
		t.Fatal(metadata, err)
	}
	if kind, _, err := conn.ReadMessage(); err != nil || kind != websocket.BinaryMessage {
		t.Fatal(kind, err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "frameAck", "frameID": metadata.ID, "decodeMS": 2, "drawMS": 1, "displayFPS": 30}); err != nil {
		t.Fatal(err)
	}
	for {
		w.mu.Lock()
		ready := !w.performance.FeedbackAt.IsZero()
		w.mu.Unlock()
		if ready {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("display acknowledgement was not measured")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestWindowsPerformanceCapturesRateDropsAndSnapshotIsolation(t *testing.T) {
	v := &windowsViewer{visible: true, frames: make(chan []byte, 1)}
	w := &windowsWorker{Worker: Worker{Token: "stream", RenderMode: "nvidia"}, viewers: map[*windowsViewer]struct{}{v: {}}, performance: StreamPerformance{Token: "stream", TargetFPS: 60, Policy: "animation"}, performanceAt: time.Now().Add(-2 * time.Second), performanceFrames: 100}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 64, 64)), nil); err != nil {
		t.Fatal(err)
	}
	frame := base64.StdEncoding.EncodeToString(encoded.Bytes())
	w.publishFrame(frame)
	w.publishFrame(frame)
	if w.performance.CaptureFPS < 45 || w.performance.CaptureFPS > 55 || w.performance.ServerDropped != 1 {
		t.Fatal(w.performance)
	}
	m := &WindowsProcessManager{active: map[string]*windowsWorker{w.Token: w}}
	stats := m.PerformanceSnapshot()
	if len(stats) != 1 || !stats[0].Watching || stats[0].FrameBytes != encoded.Len() {
		t.Fatal(stats)
	}
	stats[0].FrameBytes = 0
	if w.performance.FrameBytes == 0 {
		t.Fatal("returned metrics aliased worker state")
	}
	if got := m.gpuBudgetFor(w.Token); got != 60 {
		t.Fatal("animation budget changed without input", got)
	}
}
