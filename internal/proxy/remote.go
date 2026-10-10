package proxy

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// NewRemoteHandler exposes a Linux GoStream3D node through another host.
// The media stream still uses the Linux node's ICE address directly.
func NewRemoteHandler(upstreamURL string, logger *slog.Logger) (http.Handler, error) {
	target, err := url.Parse(upstreamURL)
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") || target.User != nil || target.RawQuery != "" || target.Fragment != "" || (target.Path != "" && target.Path != "/") {
		return nil, errors.New("upstream must be an HTTP(S) origin")
	}
	if logger == nil {
		logger = slog.Default()
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()
			if strings.EqualFold(pr.In.Header.Get("Upgrade"), "websocket") {
				pr.Out.Header.Set("Origin", target.Scheme+"://"+target.Host)
			}
		},
		ModifyResponse: func(response *http.Response) error {
			if response.StatusCode != http.StatusNotFound || response.Request == nil {
				return nil
			}
			request := response.Request
			if !((request.Method == http.MethodGet && (request.URL.Path == "/api/v1/admin/sessions" || request.URL.Path == "/api/v1/auth/me")) ||
				(request.Method == http.MethodPost && (request.URL.Path == "/api/v1/sessions" || request.URL.Path == "/api/v1/auth/login" || request.URL.Path == "/api/v1/auth/logout"))) {
				return nil
			}
			_ = response.Body.Close()
			body := `{"code":502,"msg":"Linux 节点没有 GoStream3D 管理接口。请检查程序旁 config.env 的 GOSTREAM3D_REMOTE_BASE_URL：它必须指向 Linux GoStream3D 网关，不能指向 Neko 服务。"}`
			response.StatusCode = http.StatusBadGateway
			response.Status = fmt.Sprintf("%d %s", response.StatusCode, http.StatusText(response.StatusCode))
			response.Body = io.NopCloser(strings.NewReader(body))
			response.ContentLength = int64(len(body))
			response.Header.Set("Content-Type", "application/json; charset=utf-8")
			response.Header.Set("Content-Length", fmt.Sprint(len(body)))
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Error("remote upstream failed", "error", err, "path", r.URL.Path)
			if strings.Contains(r.Header.Get("Accept"), "text/html") {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Linux 节点不可用</title><style>body{font-family:system-ui,"Microsoft YaHei",sans-serif;background:#101828;color:#f8fafc;min-height:100vh;display:grid;place-items:center;margin:0}main{max-width:540px;padding:32px;background:#1e293b;border-radius:16px;line-height:1.7}p{color:#cbd5e1}</style><main><h1>Linux 渲染节点暂不可用</h1><p>请确认 Linux 上的 GoStream3D 已启动，并检查 Windows 程序旁 config.env 中的 GOSTREAM3D_REMOTE_BASE_URL。详细原因见 gostream3d.log。</p></main></html>`)
				return
			}
			http.Error(w, "Linux rendering node unavailable", http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && !sameOrigin(r) {
			http.Error(w, "invalid websocket origin", http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(w, r)
	}), nil
}
