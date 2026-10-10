package orchestrator

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWaitForPageChecksReadySelector(t *testing.T) {
	upgrader := websocket.Upgrader{}
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, r *http.Request) {
		ws := "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/page/1"
		_ = json.NewEncoder(w).Encode([]devtoolsTarget{{Type: "page", URL: "https://example.com/scene", WebSocketDebuggerURL: ws}})
	})
	mux.HandleFunc("/devtools/page/1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		var request struct {
			Params struct {
				Expression string `json:"expression"`
			} `json:"params"`
		}
		if err := conn.ReadJSON(&request); err != nil {
			t.Errorf("read CDP: %v", err)
			return
		}
		if !strings.Contains(request.Params.Expression, `document.querySelector("#ready")`) {
			t.Errorf("selector missing: %s", request.Params.Expression)
		}
		_ = conn.WriteJSON(map[string]any{"id": 1, "result": map[string]any{"result": map[string]any{"value": true}}})
	})
	server = httptest.NewServer(mux)
	defer server.Close()
	host, portString, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitForPage(context.Background(), host, port, "#ready", time.Second); err != nil {
		t.Fatal(err)
	}
}
