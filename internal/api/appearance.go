package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gostream3d/internal/appearance"
)

type AppearanceStore interface {
	Theme(context.Context) (string, error)
	SaveTheme(context.Context, string) error
}

func registerAppearance(admin *gin.RouterGroup, cfg RouterConfig) {
	handler := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if cfg.Appearance == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"msg": "当前节点暂不支持保存配色。"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		var theme string
		var err error
		if c.Request.Method == http.MethodPut {
			var input struct {
				Theme string `json:"theme"`
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
			decoder := json.NewDecoder(c.Request.Body)
			decoder.DisallowUnknownFields()
			if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !appearance.ValidTheme(input.Theme) {
				c.JSON(http.StatusBadRequest, gin.H{"msg": "请选择有效的界面配色。"})
				return
			}
			theme = input.Theme
			err = cfg.Appearance.SaveTheme(ctx, theme)
		} else {
			theme, err = cfg.Appearance.Theme(ctx)
		}
		if err != nil {
			cfg.Logger.Error("console appearance database request failed", "method", c.Request.Method, "error", err)
			message := "无法读取已保存的配色，请重试。"
			if c.Request.Method == http.MethodPut {
				message = "配色保存未完成，请稍后重试。"
			}
			c.JSON(http.StatusInternalServerError, gin.H{"msg": message})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"theme": theme}})
	}
	admin.GET("/admin/appearance", handler)
	admin.PUT("/admin/appearance", handler)
}
