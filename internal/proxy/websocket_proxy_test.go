package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketProxyForwardsBothDirections(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" || r.URL.Query().Get("password") != "secret" || r.URL.Query().Get("username") == "" {
			http.Error(w, "bad upstream authentication", http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			kind, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(kind, payload); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	p, err := NewWebSocketProxy("secret")
	if err != nil {
		t.Fatal(err)
	}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = p.Serve(w, r, upstream.URL)
	}))
	defer gateway.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for _, test := range []struct {
		kind int
		data string
	}{{websocket.TextMessage, `{"event":"signal/answer","sdp":"test"}`}, {websocket.BinaryMessage, "\x01\x02\x03"}} {
		if err := conn.WriteMessage(test.kind, []byte(test.data)); err != nil {
			t.Fatal(err)
		}
		kind, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind != test.kind || string(data) != test.data {
			t.Fatalf("forwarded frame mismatch: kind=%d data=%q", kind, data)
		}
	}
}
