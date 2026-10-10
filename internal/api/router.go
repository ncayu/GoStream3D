package api

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gostream3d/internal/cpulimit"
	"gostream3d/internal/monitor"
	"gostream3d/internal/orchestrator"
	"gostream3d/internal/viewport"
)

//go:embed static
var bundledWeb embed.FS

type RouterConfig struct {
	AdminAPIKey          string
	AdminUsername        string
	AdminPassword        string
	AdminCredentialsPath string
	PublicBaseURL        string
	WebDist              string
	Sessions             *orchestrator.SessionManager
	Proxy                SignalProxy
	Transport            string
	CPUPercent           int
	CPUControl           *cpulimit.Service
	ChannelControl       *orchestrator.ChannelSettingsService
	PerformanceControl   *orchestrator.PerformanceSettingsService
	Monitor              *monitor.Service
	Capacity             *orchestrator.CapacityService
	Appearance           AppearanceStore
	PreloadContext       context.Context
	PreloadTimeout       time.Duration
	Logger               *slog.Logger
}

type SignalProxy interface {
	Serve(http.ResponseWriter, *http.Request, string) error
}

func NewRouter(cfg RouterConfig) (*gin.Engine, error) {
	if cfg.Sessions == nil || cfg.Proxy == nil || cfg.PublicBaseURL == "" ||
		(cfg.AdminAPIKey == "" && (cfg.AdminUsername == "" || cfg.AdminPassword == "")) {
		return nil, errors.New("router requires sessions, proxy, admin credentials, and public URL")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Transport == "" {
		cfg.Transport = "webrtc"
	}
	if cfg.PreloadContext == nil {
		cfg.PreloadContext = context.Background()
	}
	if cfg.PreloadTimeout <= 0 {
		cfg.PreloadTimeout = 130 * time.Second
	}
	indexHTML, assets, err := loadWebUI(cfg.WebDist)
	if err != nil {
		return nil, err
	}
	r := gin.New()
	r.Use(gin.Recovery())
	auth := newAdminAuth(cfg.AdminUsername, cfg.AdminPassword, cfg.AdminAPIKey)
	if err := auth.restoreCredentials(cfg.AdminCredentialsPath); err != nil {
		return nil, fmt.Errorf("load administrator account: %w", err)
	}
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/admin") })
	r.POST("/api/v1/auth/login", auth.login)
	r.POST("/api/v1/auth/logout", auth.logout)
	admin := r.Group("/api/v1", auth.required)
	admin.GET("/auth/me", auth.me)
	admin.PUT("/auth/account", auth.updateAccount)
	admin.POST("/sessions", func(c *gin.Context) { createSession(c, cfg) })
	admin.GET("/admin/sessions", func(c *gin.Context) { listSessions(c, cfg) })
	registerManagementDiagnostics(admin, cfg)
	registerAppearance(admin, cfg)
	admin.GET("/admin/resources", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		percent := cfg.CPUPercent
		if cfg.CPUControl != nil {
			percent = cfg.CPUControl.Settings().Percent
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
			"cpu_max_percent":            percent,
			"cpu_limit_supported":        cfg.CPUControl != nil,
			"channel_capacity":           cfg.Sessions.ChannelCapacity(),
			"channel_capacity_source":    "configured",
			"hardware_capacity_detected": false,
			"transport":                  cfg.Transport,
			"memory_enabled":             true,
			"disk_cache_enabled":         true,
		}})
	})
	admin.GET("/admin/performance", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		settings := cfg.Sessions.PerformanceSettings()
		if cfg.PerformanceControl != nil {
			settings = cfg.PerformanceControl.Settings()
		}
		streams := []orchestrator.StreamPerformance{}
		supported := false
		if source, ok := cfg.Proxy.(interface {
			PerformanceSnapshot() []orchestrator.StreamPerformance
		}); ok {
			streams = source.PerformanceSnapshot()
			supported = true
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"settings": settings, "streams": streams, "jpeg_metrics_supported": supported && cfg.Transport == "jpeg", "webrtc_metrics_supported": supported && cfg.Transport == "webrtc"}})
	})
	admin.PUT("/admin/performance/settings", func(c *gin.Context) {
		if cfg.PerformanceControl == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"msg": "当前节点暂不支持调整性能策略。"})
			return
		}
		var input struct {
			Budget  *int  `json:"gpu_fps_budget"`
			Reclaim *bool `json:"reclaim_idle_preloads"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		if err := c.ShouldBindJSON(&input); err != nil || input.Budget == nil || input.Reclaim == nil || *input.Budget < 30 || *input.Budget > 1200 {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "请提供空闲回收策略及 30–1200 的 GPU 总帧率预算。"})
			return
		}
		value := orchestrator.PerformanceSettings{GPUFPSBudget: *input.Budget, ReclaimIdlePreloads: *input.Reclaim}
		if err := cfg.PerformanceControl.Update(value); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": "性能策略保存失败，原配置保持生效。"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg.PerformanceControl.Settings()})
	})
	admin.GET("/admin/channels/settings", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if cfg.ChannelControl == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"code": 501, "msg": "当前节点暂不支持在平台中调整渲染通道数。"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg.ChannelControl.Settings()})
	})
	admin.PUT("/admin/channels/settings", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if cfg.ChannelControl == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"code": 501, "msg": "当前节点暂不支持在平台中调整渲染通道数。"})
			return
		}
		var input struct {
			MaxChannels *int `json:"max_channels"`
		}
		if err := c.ShouldBindJSON(&input); err != nil || input.MaxChannels == nil || *input.MaxChannels < 1 || *input.MaxChannels > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "服务器最大渲染通道数须为 1–100 的整数。"})
			return
		}
		if err := cfg.ChannelControl.SetMaxChannels(*input.MaxChannels); err != nil {
			switch {
			case errors.Is(err, orchestrator.ErrInvalidChannelCapacity):
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "渲染通道数须为 1–100 的整数，且扩展后的端口范围须有效、互不冲突。"})
			case errors.Is(err, orchestrator.ErrChannelsInUse):
				c.JSON(http.StatusConflict, gin.H{"code": 409, "msg": "运行中的实例占用了缩减范围内的通道，请先停止对应渲染实例后重试。"})
			default:
				cfg.Logger.Error("save channel settings", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "渲染通道数保存失败，请刷新查看当前设置。"})
			}
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg.ChannelControl.Settings()})
	})
	admin.GET("/admin/cpu/settings", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		settings := cpulimit.Settings{Percent: cfg.CPUPercent, Supported: false}
		if cfg.CPUControl != nil {
			settings = cfg.CPUControl.Settings()
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": settings})
	})
	admin.PUT("/admin/cpu/settings", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if cfg.CPUControl == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"code": 501, "msg": "当前节点暂不支持在平台中调整 CPU 硬上限。"})
			return
		}
		var input struct {
			Percent *int `json:"cpu_max_percent"`
		}
		if err := c.ShouldBindJSON(&input); err != nil || input.Percent == nil || *input.Percent < 1 || *input.Percent > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "CPU 使用上限须为 1–100 的整数百分比。"})
			return
		}
		if err := cfg.CPUControl.SetPercent(*input.Percent); err != nil {
			cfg.Logger.Error("save CPU limit", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "CPU 上限设置失败，请刷新查看当前上限。"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg.CPUControl.Settings()})
	})
	admin.GET("/admin/monitor", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if cfg.Monitor == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "平台监控尚未就绪"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg.Monitor.Snapshot()})
	})
	admin.POST("/admin/monitor/refresh", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if cfg.Monitor == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "平台监控尚未就绪"})
			return
		}
		cfg.Monitor.Sample(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg.Monitor.Snapshot()})
	})
	admin.GET("/admin/monitor/settings", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if cfg.Monitor == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "平台监控尚未就绪"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg.Monitor.Settings()})
	})
	admin.PUT("/admin/monitor/settings", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if cfg.Monitor == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "平台监控尚未就绪"})
			return
		}
		var input monitor.Settings
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请提供采样间隔秒数"})
			return
		}
		if input.IntervalSeconds < int(monitor.MinInterval/time.Second) || input.IntervalSeconds > int(monitor.MaxInterval/time.Second) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "采样间隔须为 10–300 秒"})
			return
		}
		if err := cfg.Monitor.SetInterval(time.Duration(input.IntervalSeconds) * time.Second); err != nil {
			if errors.Is(err, monitor.ErrInvalidInterval) {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "采样间隔须为 10–300 秒"})
				return
			}
			cfg.Logger.Error("save monitor settings", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "监控设置保存失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg.Monitor.Settings()})
	})
	admin.DELETE("/admin/sessions/:token", func(c *gin.Context) { deleteSession(c, cfg) })
	admin.PUT("/admin/sessions/:token", func(c *gin.Context) { updateSession(c, cfg) })
	admin.PUT("/admin/sessions/:token/presentation", func(c *gin.Context) { updatePresentation(c, cfg) })
	admin.POST("/admin/sessions/:token/warm", func(c *gin.Context) { warmSession(c, cfg) })
	admin.POST("/admin/sessions/:token/start", func(c *gin.Context) { startRendering(c, cfg) })
	admin.POST("/admin/sessions/:token/stop", func(c *gin.Context) { stopRendering(c, cfg) })
	r.GET("/api/v1/sessions/:token", func(c *gin.Context) { getSession(c, cfg) })
	r.GET("/api/v1/ws/:token", func(c *gin.Context) { proxySession(c, cfg) })
	r.GET("/Stream3D/:token", func(c *gin.Context) {
		if _, ok := cfg.Sessions.Lookup(c.Param("token")); !ok {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	})
	r.GET("/v/:token", func(c *gin.Context) {
		if _, ok := cfg.Sessions.Lookup(c.Param("token")); !ok {
			c.Status(http.StatusNotFound)
			return
		}
		target := "/Stream3D/" + url.PathEscape(c.Param("token"))
		if c.Request.URL.RawQuery != "" {
			target += "?" + c.Request.URL.RawQuery
		}
		c.Header("Cache-Control", "no-store")
		c.Redirect(http.StatusTemporaryRedirect, target)
	})
	r.GET("/admin", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	})
	r.StaticFS("/assets", http.FS(assets))
	return r, nil
}

// NewAdminUIHandler serves the embedded management UI on a remote gateway.
// API and viewer requests remain the responsibility of the Linux node.
func NewAdminUIHandler(webDist string) (http.Handler, error) {
	indexHTML, assets, err := loadWebUI(webDist)
	if err != nil {
		return nil, err
	}
	assetHandler := http.StripPrefix("/assets/", http.FileServer(http.FS(assets)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/":
			http.Redirect(w, r, "/admin", http.StatusFound)
		case "/admin", "/admin/":
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(indexHTML)
		default:
			if !strings.HasPrefix(r.URL.Path, "/assets/") {
				http.NotFound(w, r)
				return
			}
			assetHandler.ServeHTTP(w, r)
		}
	}), nil
}

func loadWebUI(webDist string) ([]byte, fs.FS, error) {
	webFiles, err := fs.Sub(bundledWeb, "static")
	if err != nil {
		return nil, nil, err
	}
	if info, err := os.Stat(filepath.Join(webDist, "index.html")); err == nil && !info.IsDir() {
		webFiles = os.DirFS(webDist)
	}
	indexHTML, err := fs.ReadFile(webFiles, "index.html")
	if err != nil {
		return nil, nil, err
	}
	assets, err := fs.Sub(webFiles, "assets")
	if err != nil {
		return nil, nil, err
	}
	return indexHTML, assets, nil
}

type sessionRequest struct {
	TargetURL        string `json:"target_url"`
	Resolution       string `json:"resolution"`
	FPS              int    `json:"fps"`
	RenderMode       string `json:"render_mode"`
	ReadySelector    string `json:"ready_selector"`
	ConcurrencyMode  string `json:"concurrency_mode"`
	MaxViewers       int    `json:"max_viewers"`
	PreloadCount     int    `json:"preload_count"`
	Name             string `json:"name"`
	FramePolicy      string `json:"frame_policy"`
	ReservedPreloads int    `json:"reserved_preloads"`
	ShowFPS          *bool  `json:"show_fps"`
}

func decodeSessionRequest(c *gin.Context, cfg RouterConfig) (orchestrator.SessionOptions, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var req sessionRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "invalid request body"})
		return orchestrator.SessionOptions{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "request body must contain one JSON object"})
		return orchestrator.SessionOptions{}, false
	}
	if req.FPS > 60 && cfg.Transport != "jpeg" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "90/120 FPS is currently available only in Windows native JPEG mode"})
		return orchestrator.SessionOptions{}, false
	}
	return orchestrator.SessionOptions{
		TargetURL: req.TargetURL, Resolution: req.Resolution, FPS: req.FPS,
		RenderMode: req.RenderMode, ReadySelector: req.ReadySelector,
		ConcurrencyMode: req.ConcurrencyMode, MaxViewers: req.MaxViewers, PreloadCount: req.PreloadCount, Name: req.Name,
		FramePolicy: req.FramePolicy, ReservedPreloads: req.ReservedPreloads, ShowFPS: req.ShowFPS,
	}, true
}

func createSession(c *gin.Context, cfg RouterConfig) {
	options, ok := decodeSessionRequest(c, cfg)
	if !ok {
		return
	}
	async := c.Query("async") == "1"
	var info orchestrator.SessionInfo
	var err error
	if async {
		info, err = cfg.Sessions.CreatePublished(options)
	} else {
		info, err = cfg.Sessions.CreatePrewarmed(c.Request.Context(), options)
	}
	if err != nil {
		if errors.Is(err, orchestrator.ErrInvalidSessionOptions) || errors.Is(err, orchestrator.ErrInvalidReadySelector) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		cfg.Logger.Error("create session", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": err.Error()})
		return
	}
	status := http.StatusCreated
	if async {
		status = http.StatusAccepted
		startBackgroundWarm(cfg, info.Token)
	}
	c.JSON(status, gin.H{"code": 0, "msg": "success", "data": gin.H{
		"token": info.Token, "stream_url": streamURLForRequest(c, cfg, info.Token),
		"render_mode":      info.RenderMode,
		"concurrency_mode": info.ConcurrencyMode, "max_viewers": info.MaxViewers, "preload_count": info.PreloadCount,
		"preloaded": !async,
	}})
}

func updateSession(c *gin.Context, cfg RouterConfig) {
	options, ok := decodeSessionRequest(c, cfg)
	if !ok {
		return
	}
	info, shouldWarm, err := cfg.Sessions.Update(c.Request.Context(), c.Param("token"), options)
	if err != nil {
		switch {
		case errors.Is(err, orchestrator.ErrInvalidSessionOptions):
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		case errors.Is(err, orchestrator.ErrSessionNotFound):
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "session not found"})
		case errors.Is(err, orchestrator.ErrSessionBusy):
			c.JSON(http.StatusConflict, gin.H{"code": 409, "msg": "有观众正在观看，请等待观看结束后再编辑。"})
		default:
			cfg.Logger.Error("update session", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "could not save session"})
		}
		return
	}
	if shouldWarm {
		startBackgroundWarm(cfg, info.Token)
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
		"token": info.Token, "stream_url": streamURLForRequest(c, cfg, info.Token),
		"preloading": shouldWarm,
	}})
}

func startBackgroundWarm(cfg RouterConfig, token string) {
	go func() {
		preloadCtx, cancel := context.WithTimeout(cfg.PreloadContext, cfg.PreloadTimeout)
		defer cancel()
		if _, err := cfg.Sessions.Warm(preloadCtx, token); err != nil && !errors.Is(err, context.Canceled) {
			cfg.Logger.Error("background page preload failed", "token", token, "error", err)
		}
	}()
}

func listSessions(c *gin.Context, cfg RouterConfig) {
	c.Header("Cache-Control", "no-store")
	items := cfg.Sessions.List()
	for i := range items {
		items[i].StreamURL = streamURLForRequest(c, cfg, items[i].Token)
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": items})
}

// Use the platform's request host instead of the node's configured localhost.
// The console also derives links from window.location.origin, so an HTTPS
// reverse proxy keeps its public scheme and host without trusting forwarded headers.
func streamURLForRequest(c *gin.Context, cfg RouterConfig, token string) string {
	path := "/Stream3D/" + url.PathEscape(token)
	if c.Request.Host == "" {
		return strings.TrimRight(cfg.PublicBaseURL, "/") + path
	}
	scheme := "http"
	if c.Request.TLS != nil || c.Request.URL.Scheme == "https" {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host + path
}

func deleteSession(c *gin.Context, cfg RouterConfig) {
	if _, ok := cfg.Sessions.Lookup(c.Param("token")); !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "session not found"})
		return
	}
	if err := cfg.Sessions.Delete(c.Request.Context(), c.Param("token")); err != nil {
		cfg.Logger.Error("delete session", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "could not remove worker"})
		return
	}
	c.Status(http.StatusNoContent)
}

func warmSession(c *gin.Context, cfg RouterConfig) {
	info, ok := cfg.Sessions.Lookup(c.Param("token"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "session not found"})
		return
	}
	if info.RenderPaused {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "msg": "rendering is stopped by the administrator"})
		return
	}
	if c.Query("async") == "1" {
		startBackgroundWarm(cfg, c.Param("token"))
		c.JSON(http.StatusAccepted, gin.H{"code": 0, "data": gin.H{"token": c.Param("token"), "preloading": true}})
		return
	}
	worker, err := cfg.Sessions.Warm(c.Request.Context(), c.Param("token"))
	if err != nil {
		cfg.Logger.Error("warm session", "token", c.Param("token"), "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"token": worker.Token, "started_at": worker.StartedAt}})
}

func startRendering(c *gin.Context, cfg RouterConfig) {
	token := c.Param("token")
	if _, ok := cfg.Sessions.Lookup(token); !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "session not found"})
		return
	}
	if c.Query("async") == "1" {
		if err := cfg.Sessions.EnableRendering(token); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": err.Error()})
			return
		}
		startBackgroundWarm(cfg, token)
		c.JSON(http.StatusAccepted, gin.H{"code": 0, "data": gin.H{"token": token, "preloading": true}})
		return
	}
	worker, err := cfg.Sessions.ResumeRendering(c.Request.Context(), token)
	if err != nil {
		cfg.Logger.Error("start rendering", "token", token, "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"token": token, "started_at": worker.StartedAt}})
}

func stopRendering(c *gin.Context, cfg RouterConfig) {
	token := c.Param("token")
	if _, ok := cfg.Sessions.Lookup(token); !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "session not found"})
		return
	}
	if err := cfg.Sessions.PauseRendering(c.Request.Context(), token); err != nil {
		cfg.Logger.Error("stop rendering", "token", token, "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"token": token, "render_paused": true}})
}

func updatePresentation(c *gin.Context, cfg RouterConfig) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	var input struct {
		ShowFPS *bool `json:"show_fps"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.ShowFPS == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请提供 show_fps 布尔值"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请求体只能包含一个 JSON 对象"})
		return
	}
	info, err := cfg.Sessions.SetShowFPS(c.Param("token"), *input.ShowFPS)
	if err != nil {
		if errors.Is(err, orchestrator.ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "项目不存在"})
		} else {
			cfg.Logger.Error("save viewer presentation", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "FPS 显示设置保存失败，请重试"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"token": info.Token, "show_fps": info.ShowFPS}})
}

func getSession(c *gin.Context, cfg RouterConfig) {
	status, ok := cfg.Sessions.Status(c.Param("token"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "session not found"})
		return
	}
	capacityFull := false
	if cfg.ChannelControl != nil {
		pool := cfg.ChannelControl.Settings()
		capacityFull = pool.ActiveChannels >= pool.MaxChannels && status.AvailableInstances == 0 && status.StartingInstances == 0
		if capacityFull && cfg.Sessions.CanReclaimIdle(status.Token) {
			capacityFull = false
		}
	}
	c.Header("Cache-Control", "no-store")
	maxWidth, maxHeight := 3840, 2160
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
		"token": status.Token, "resolution": status.Resolution, "fps": status.FPS, "transport": cfg.Transport,
		"render_mode": status.RenderMode,
		"show_fps":    status.ShowFPS,
		"max_width":   maxWidth, "max_height": maxHeight,
		"render_paused": status.RenderPaused, "running": status.Running, "preload_error": status.PreloadError,
		"ready_instances": status.ReadyInstances, "available_instances": status.AvailableInstances, "starting_instances": status.StartingInstances, "pool_error": status.PoolError,
		"viewer_limit_reached": status.Viewers >= status.MaxViewers, "channel_capacity_full": capacityFull,
	}})
}

func proxySession(c *gin.Context, cfg RouterConfig) {
	token := c.Param("token")
	info, ok := cfg.Sessions.Lookup(token)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "session not found"})
		return
	}
	worker, release, err := cfg.Sessions.Acquire(c.Request.Context(), token)
	if err != nil {
		if errors.Is(err, orchestrator.ErrViewerLimit) {
			c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "msg": "路数超出限额，请查看配置"})
			return
		}
		if errors.Is(err, orchestrator.ErrRenderPaused) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "rendering stopped by administrator"})
			return
		}
		if errors.Is(err, orchestrator.ErrPortsExhausted) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "路数超出限额，请查看配置"})
			return
		}
		cfg.Logger.Error("start worker", "token", token, "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "worker unavailable"})
		return
	}
	defer release()
	request := viewport.WithSettings(c.Request, info.Resolution == viewport.Adaptive, info.FPS)
	if source, ok := cfg.Proxy.(interface {
		ServeWorker(http.ResponseWriter, *http.Request, orchestrator.Worker, orchestrator.SessionInfo) error
	}); ok {
		if err := source.ServeWorker(c.Writer, request, worker, info); err != nil {
			cfg.Logger.Debug("signaling connection ended", "token", token, "error", err)
		}
		return
	}
	if err := cfg.Proxy.Serve(c.Writer, request, worker.SignalURL); err != nil {
		cfg.Logger.Debug("signaling connection ended", "token", token, "error", err)
	}
}
