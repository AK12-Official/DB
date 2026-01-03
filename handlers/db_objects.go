package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"zh.xyz/dv/sync/database"
	"zh.xyz/dv/sync/dbconn"
	"zh.xyz/dv/sync/models"
	"zh.xyz/dv/sync/service"
)

type DBObjectHandler struct{}

// ListObjects 列出数据库对象
func (h *DBObjectHandler) ListObjects(c *gin.Context) {
	connectionID := c.Param("id")
	objectType := c.Query("type") // procedure, function, view, trigger

	var dbConn models.DatabaseConnection
	if err := database.DB.First(&dbConn, connectionID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "数据库连接不存在"})
		return
	}

	rawConn, err := dbconn.GetRawConnection(&dbConn)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "连接数据库失败: " + err.Error()})
		return
	}
	defer rawConn.Close()

	objectService := &service.DatabaseObjectService{}

	var objects []service.DatabaseObjectInfo
	var err2 error

	if objectType != "" {
		// 查询指定类型的对象
		objects, err2 = objectService.GetObjectsByType(rawConn, dbConn.Type, dbConn.Database, objectType)
	} else {
		// 查询所有类型的对象
		allObjects := make([]service.DatabaseObjectInfo, 0)
		types := []string{"procedure", "function", "view", "trigger"}
		for _, t := range types {
			objs, err := objectService.GetObjectsByType(rawConn, dbConn.Type, dbConn.Database, t)
			if err == nil {
				allObjects = append(allObjects, objs...)
			}
		}
		objects = allObjects
	}

	if err2 != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询对象失败: " + err2.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": objects})
}

// GetObjectDefinition 获取对象定义
func (h *DBObjectHandler) GetObjectDefinition(c *gin.Context) {
	connectionID := c.Param("id")
	objectType := c.Param("type")
	objectName := c.Query("name")
	tableName := c.Query("table_name") // 触发器需要

	if objectName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少对象名称参数"})
		return
	}

	var dbConn models.DatabaseConnection
	if err := database.DB.First(&dbConn, connectionID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "数据库连接不存在"})
		return
	}

	rawConn, err := dbconn.GetRawConnection(&dbConn)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "连接数据库失败: " + err.Error()})
		return
	}
	defer rawConn.Close()

	objectService := &service.DatabaseObjectService{}
	definition, err := objectService.GetObjectDefinitionPublic(rawConn, dbConn.Type, dbConn.Database, objectType, objectName, tableName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取对象定义失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"object_type": objectType,
		"object_name": objectName,
		"definition":  definition,
	})
}

// GetObjectSyncLogs 获取对象同步日志
func (h *DBObjectHandler) GetObjectSyncLogs(c *gin.Context) {
	taskID := c.Param("task_id")

	var logs []models.ObjectSyncLog
	query := database.DB.Where("task_id = ?", taskID).Order("created_at DESC").Limit(100)

	objectType := c.Query("object_type")
	if objectType != "" {
		query = query.Where("object_type = ?", objectType)
	}

	if err := query.Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": logs})
}

