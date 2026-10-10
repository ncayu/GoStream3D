package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestRemoteHandlerForwardsHTTPAndWebSocket(t *testing.T) {
	upgrader := websocket.Upgrader{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			kind, payload, err := conn.ReadMessage()
			if err == nil {
				_ = conn.WriteMessage(kind, payload)
			}
			return
		}
		_, _ = io.WriteString(w, r.URL.RequestURI())
	}))
	defer upstream.Close()
	handler, err := NewRemoteHandler(upstream.URL, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	response, err := http.Get(server.URL + "/Stream3D/example?x=1")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || string(body) != "/Stream3D/example?x=1" {
		t.Fatalf("HTTP forwarding: status=%d body=%q err=%v", response.StatusCode, body, err)
	}
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{server.URL}})
	if err != nil {
		if response != nil {
			response.Body.Close()
		}
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	_, message, err := conn.ReadMessage()
	if err != nil || string(message) != "ping" {
		t.Fatalf("WebSocket forwarding: message=%q err=%v", message, err)
	}
}

func TestRemoteHandlerRejectsForeignWebSocketOrigin(t *testing.T) {
	handler, err := NewRemoteHandler("http://127.0.0.1:9", nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://gateway/ws", nil)
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Origin", "https://elsewhere.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", response.Code)
	}
}

func TestRemoteHandlerExplainsUnavailableNode(t *testing.T) {
	handler, err := NewRemoteHandler("http://127.0.0.1:9", nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://gateway/admin", nil)
	request.Header.Set("Accept", "text/html")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "Linux 渲染节点暂不可用") {
		t.Fatalf("unexpected response: status=%d body=%q", response.Code, response.Body.String())
	}
}
