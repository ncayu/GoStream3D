package api

import (
	"github.com/gin-gonic/gin"
	"gostream3d/internal/orchestrator"
	"net/http"
)

func registerManagementDiagnostics(admin *gin.RouterGroup, cfg RouterConfig) {
	admin.GET("/admin/store", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"data": cfg.Sessions.StoreStatus()})
	})
	admin.POST("/admin/store/backup", func(c *gin.Context) {
		id, err := cfg.Sessions.BackupStore()
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"msg": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"backup_id": id}})
	})
	admin.POST("/admin/store/preview", func(c *gin.Context) {
		var input struct {
			ID string `json:"backup_id"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		if c.ShouldBindJSON(&input) != nil {
			c.JSON(400, gin.H{"msg": "请选择一个备份。"})
			return
		}
		p, err := cfg.Sessions.PreviewRestore(input.ID)
		if err != nil {
			c.JSON(400, gin.H{"msg": err.Error()})
			return
		}
		c.JSON(200, gin.H{"data": p})
	})
	admin.POST("/admin/store/restore", func(c *gin.Context) {
		var input struct {
			ID     string `json:"backup_id"`
			Digest string `json:"digest"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		if c.ShouldBindJSON(&input) != nil {
			c.JSON(400, gin.H{"msg": "请先预览备份。"})
			return
		}
		p, err := cfg.Sessions.RestoreMissing(input.ID, input.Digest)
		if err != nil {
			c.JSON(409, gin.H{"msg": err.Error()})
			return
		}
		c.JSON(200, gin.H{"data": p})
	})
	admin.GET("/admin/capacity", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if cfg.Capacity == nil {
			c.JSON(200, gin.H{"data": gin.H{"status": "idle", "supported": false, "stages": []any{}}})
			return
		}
		c.JSON(200, gin.H{"data": cfg.Capacity.Snapshot()})
	})
	admin.POST("/admin/capacity", func(c *gin.Context) {
		if cfg.Capacity == nil {
			c.JSON(501, gin.H{"msg": "当前节点不支持场景容量测试。"})
			return
		}
		var input orchestrator.CapacityRequest
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		if c.ShouldBindJSON(&input) != nil {
			c.JSON(400, gin.H{"msg": "测试参数不合法。"})
			return
		}
		info, ok := cfg.Sessions.Lookup(input.ProjectToken)
		if !ok {
			c.JSON(404, gin.H{"msg": "所选项目不存在。"})
			return
		}
		if err := cfg.Capacity.Start(cfg.PreloadContext, info, input); err != nil {
			c.JSON(409, gin.H{"msg": err.Error()})
			return
		}
		c.JSON(202, gin.H{"data": cfg.Capacity.Snapshot()})
	})
	admin.POST("/admin/capacity/cancel", func(c *gin.Context) {
		if cfg.Capacity != nil {
			cfg.Capacity.Cancel()
		}
		c.JSON(202, gin.H{"msg": "正在取消测试并清理测试实例。"})
	})
}
