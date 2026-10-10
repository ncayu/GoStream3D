package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"gostream3d/internal/orchestrator"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"gostream3d/internal/viewport"
)

const (
	maxMessageBytes = 2 << 20
	pingInterval    = 20 * time.Second
	pongWait        = 70 * time.Second
)

type WebSocketProxy struct {
	performanceMu       sync.Mutex
	performanceSequence uint64
	performance         map[string]orchestrator.StreamPerformance
	userPassword        string
	adminPassword       string
	dialer              websocket.Dialer
	upgrader            websocket.Upgrader
}

func NewWebSocketProxy(userPassword string, adminPassword ...string) (*WebSocketProxy, error) {
	if userPassword == "" {
		return nil, errors.New("Neko user password is required")
	}
	admin := ""
	if len(adminPassword) > 0 {
		admin = adminPassword[0]
	}
	return &WebSocketProxy{
		userPassword:  userPassword,
		adminPassword: admin,
		dialer: websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
			ReadBufferSize:   4096,
			WriteBufferSize:  4096,
		},
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     sameOrigin,
		},
	}, nil
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // Non-browser clients; the token is still required by the route.
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host)
}

func nekoURL(baseURL, password string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u == nil || u.Scheme != "http" || u.Host == "" || u.Path != "" {
		return "", errors.New("invalid internal Neko URL")
	}
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	u.Scheme = "ws"
	u.Path = "/ws"
	q := url.Values{}
	q.Set("password", password)
	q.Set("username", "gostream3d-"+hex.EncodeToString(id[:]))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Serve dials Neko before upgrading the browser, so upstream failures can
// still return an ordinary HTTP error. Frames are forwarded without JSON edits.
func (p *WebSocketProxy) Serve(w http.ResponseWriter, r *http.Request, internalBaseURL string) error {
	upstreamURL, err := nekoURL(internalBaseURL, p.userPassword)
	if err != nil {
		http.Error(w, "invalid worker endpoint", http.StatusBadGateway)
		return err
	}
	upstream, response, err := p.dialer.DialContext(r.Context(), upstreamURL, nil)
	if err != nil {
		if response != nil {
			response.Body.Close()
		}
		http.Error(w, "worker signaling unavailable", http.StatusBadGateway)
		return fmt.Errorf("dial Neko signaling: %w", err)
	}
	defer upstream.Close()
	client, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return fmt.Errorf("upgrade browser websocket: %w", err)
	}
	defer client.Close()
	performanceID := ""
	if seed, ok := r.Context().Value(performanceKey{}).(orchestrator.StreamPerformance); ok {
		performanceID = p.openPerformance(seed)
		defer p.closePerformance(performanceID)
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	errs := make(chan error, 2)
	settings := viewport.FromRequest(r)
	var clientWrite sync.Mutex
	sizes := make(chan viewport.Size, 1)
	if settings.Adaptive {
		go func() {
			var previous viewport.Size
			for {
				select {
				case <-ctx.Done():
					return
				case size := <-sizes:
					// Debounce repeated window changes and retain the latest size.
					timer := time.NewTimer(300 * time.Millisecond)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
					select {
					case size = <-sizes:
					default:
					}
					if size == previous {
						continue
					}
					if err := p.setViewport(ctx, internalBaseURL, size, settings.FPS); err != nil {
						clientWrite.Lock()
						_ = client.WriteJSON(map[string]string{"event": "viewport/error", "message": "自适应尺寸调整失败，请检查服务端显示配置"})
						clientWrite.Unlock()
					} else {
						previous = size
					}
				}
			}
		}()
	}
	hook := func(kind int, payload []byte) bool {
		if kind != websocket.TextMessage {
			return false
		}
		var size viewport.Size
		if json.Unmarshal(payload, &size) != nil {
			return false
		}
		if size.Type == "webrtc-performance" {
			p.recordPerformance(performanceID, payload, time.Now())
			return true // Private telemetry never reaches Neko signaling.
		}
		if size.Type != "resize" {
			return false
		}
		if settings.Adaptive && size.Valid() {
			select {
			case sizes <- size:
			default:
				select {
				case <-sizes:
				default:
				}
				select {
				case sizes <- size:
				default:
				}
			}
		}
		return true // Never forward viewer resize messages as Neko admin commands.
	}
	go func() { errs <- forward(client, upstream, nil, hook) }()
	go func() { errs <- forward(upstream, client, &clientWrite, nil) }()
	go keepAlive(ctx, client, upstream)
	first := <-errs
	cancel()
	client.Close()
	upstream.Close()
	<-errs
	return first
}

func forward(src, dst *websocket.Conn, writeMu *sync.Mutex, intercept func(int, []byte) bool) error {
	src.SetReadLimit(maxMessageBytes)
	_ = src.SetReadDeadline(time.Now().Add(pongWait))
	src.SetPongHandler(func(string) error {
		return src.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		messageType, payload, err := src.ReadMessage()
		if err != nil {
			return err
		}
		_ = src.SetReadDeadline(time.Now().Add(pongWait))
		if intercept != nil && intercept(messageType, payload) {
			continue
		}
		if writeMu != nil {
			writeMu.Lock()
		}
		_ = dst.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err = dst.WriteMessage(messageType, payload)
		if writeMu != nil {
			writeMu.Unlock()
		}
		if err != nil {
			return err
		}
	}
}

func keepAlive(ctx context.Context, connections ...*websocket.Conn) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deadline := time.Now().Add(5 * time.Second)
			for _, conn := range connections {
				if err := conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
					return
				}
			}
		}
	}
}
