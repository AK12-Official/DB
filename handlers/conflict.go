package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"zh.xyz/dv/sync/database"
	"zh.xyz/dv/sync/models"
	"zh.xyz/dv/sync/service"
	"zh.xyz/dv/sync/utils"
)

type ConflictHandler struct{}

// ListConflicts 列出所有冲突
func (h *ConflictHandler) ListConflicts(c *gin.Context) {
	var conflicts []models.DataConflict
	query := database.DB.Preload("Task").Preload("Resolver")

	status := c.Query("status")
	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Order("created_at DESC").Find(&conflicts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": conflicts})
}

// GetConflict 获取单个冲突详情
func (h *ConflictHandler) GetConflict(c *gin.Context) {
	id := c.Param("id")

	var conflict models.DataConflict
	if err := database.DB.Preload("Task").Preload("Resolver").First(&conflict, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "冲突记录不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": conflict})
}

// ResolveConflict 处理冲突
func (h *ConflictHandler) ResolveConflict(c *gin.Context) {
	userID, _ := c.Get("user_id")
	id := c.Param("id")

	var req struct {
		Resolution string `json:"resolution" binding:"required,oneof=source target"` // source: 以源数据库为准, target: 以目标数据库为准
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var conflict models.DataConflict
	if err := database.DB.Preload("Task").First(&conflict, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "冲突记录不存在"})
		return
	}

	if conflict.Status == "resolved" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "冲突已处理"})
		return
	}

	// 处理冲突：根据resolution决定使用哪个数据库的数据
	resolvedBy := userID.(uint)
	conflict.Resolution = req.Resolution
	conflict.ResolvedBy = &resolvedBy
	conflict.Status = "resolved"
	now := conflict.CreatedAt
	conflict.ResolvedAt = &now

	if err := database.DB.Save(&conflict).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败"})
		return
	}

	// 执行同步（应用resolution）
	syncService := &service.SyncService{}
	if err := syncService.ApplyConflictResolution(&conflict); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "应用冲突解决失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "冲突处理成功", "data": conflict})
}

// ViewConflictByToken 通过token查看冲突（用于邮件链接）
func (h *ConflictHandler) ViewConflictByToken(c *gin.Context) {
	token := c.Query("token")

	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少token参数"})
		return
	}

	// 解析token
	tokenData, err := utils.ParseConflictViewToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的token"})
		return
	}

	conflictID, ok := tokenData["conflict_id"].(float64)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的token格式"})
		return
	}

	var conflict models.DataConflict
	if err := database.DB.Preload("Task").Preload("Resolver").First(&conflict, uint(conflictID)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "冲突记录不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": conflict})
}
