package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"zh.xyz/dv/sync/database"
	"zh.xyz/dv/sync/models"
	"zh.xyz/dv/sync/service"
)

type SyncHandler struct{}

// CreateSyncTask 创建同步任务
func (h *SyncHandler) CreateSyncTask(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req struct {
		Name       string `json:"name" binding:"required"`
		SourceDBID uint   `json:"source_db_id" binding:"required"`
		TargetDBID uint   `json:"target_db_id" binding:"required"`
		TableName  string `json:"table_name"`                    // 空字符串表示整库同步
		SyncType   string `json:"sync_type" binding:"required,oneof=realtime scheduled"`
		CronExpr   string `json:"cron_expr"`                     // 定时任务需要
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.SyncType == "scheduled" && req.CronExpr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "定时任务需要提供cron表达式"})
		return
	}

	// 验证数据库连接是否存在
	var sourceDB, targetDB models.DatabaseConnection
	if err := database.DB.First(&sourceDB, req.SourceDBID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "源数据库连接不存在"})
		return
	}
	if err := database.DB.First(&targetDB, req.TargetDBID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "目标数据库连接不存在"})
		return
	}

	task := models.SyncTask{
		Name:       req.Name,
		SourceDBID: req.SourceDBID,
		TargetDBID: req.TargetDBID,
		TableName:  req.TableName,
		SyncType:   req.SyncType,
		CronExpr:   req.CronExpr,
		Status:     "stopped",
		CreatedBy:  userID.(uint),
	}

	if err := database.DB.Create(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建同步任务失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "同步任务创建成功",
		"data":    task,
	})
}

// ListSyncTasks 列出所有同步任务
func (h *SyncHandler) ListSyncTasks(c *gin.Context) {
	var tasks []models.SyncTask
	if err := database.DB.Preload("SourceDB").Preload("TargetDB").Preload("Creator").Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": tasks})
}

// GetSyncTask 获取单个同步任务
func (h *SyncHandler) GetSyncTask(c *gin.Context) {
	id := c.Param("id")

	var task models.SyncTask
	if err := database.DB.Preload("SourceDB").Preload("TargetDB").Preload("Creator").First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "同步任务不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": task})
}

// StartSyncTask 启动同步任务
func (h *SyncHandler) StartSyncTask(c *gin.Context) {
	id := c.Param("id")

	var task models.SyncTask
	if err := database.DB.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "同步任务不存在"})
		return
	}

	syncService := &service.SyncService{}

	if task.SyncType == "scheduled" {
		// 定时同步
		if err := service.StartScheduledSync(task.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		// 实时同步（立即执行一次）
		go func() {
			if err := syncService.SyncTable(&task); err != nil {
				now := time.Now()
				task.LastSyncAt = &now
				task.Status = "error"
			} else {
				now := time.Now()
				task.LastSyncAt = &now
				task.Status = "running"
			}
			database.DB.Save(&task)
		}()
		task.Status = "running"
		database.DB.Save(&task)
	}

	c.JSON(http.StatusOK, gin.H{"message": "同步任务已启动"})
}

// StopSyncTask 停止同步任务
func (h *SyncHandler) StopSyncTask(c *gin.Context) {
	id := c.Param("id")

	var task models.SyncTask
	if err := database.DB.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "同步任务不存在"})
		return
	}

	if task.SyncType == "scheduled" {
		if err := service.StopScheduledSync(task.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		task.Status = "stopped"
		database.DB.Save(&task)
	}

	c.JSON(http.StatusOK, gin.H{"message": "同步任务已停止"})
}

// ExecuteSyncTask 立即执行同步任务
func (h *SyncHandler) ExecuteSyncTask(c *gin.Context) {
	id := c.Param("id")

	var task models.SyncTask
	if err := database.DB.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "同步任务不存在"})
		return
	}

	syncService := &service.SyncService{}
	if err := syncService.SyncTable(&task); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "同步执行失败: " + err.Error()})
		return
	}

	now := time.Now()
	task.LastSyncAt = &now
	database.DB.Save(&task)

	c.JSON(http.StatusOK, gin.H{"message": "同步执行成功"})
}

// DeleteSyncTask 删除同步任务
func (h *SyncHandler) DeleteSyncTask(c *gin.Context) {
	id := c.Param("id")

	var task models.SyncTask
	if err := database.DB.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "同步任务不存在"})
		return
	}

	if err := database.DB.Delete(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// GetSyncLogs 获取同步日志
func (h *SyncHandler) GetSyncLogs(c *gin.Context) {
	taskID := c.Param("task_id")

	var logs []models.SyncLog
	query := database.DB.Where("task_id = ?", taskID).Order("created_at DESC").Limit(100)

	if err := query.Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": logs})
}
