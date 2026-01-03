package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"zh.xyz/dv/sync/database"
	"zh.xyz/dv/sync/dbconn"
	"zh.xyz/dv/sync/models"
)

type DBConnectionHandler struct{}

// CreateConnection 创建数据库连接
func (h *DBConnectionHandler) CreateConnection(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Type        string `json:"type" binding:"required,oneof=mysql oracle postgres"`
		Host        string `json:"host" binding:"required"`
		Port        string `json:"port" binding:"required"`
		Username    string `json:"username" binding:"required"`
		Password    string `json:"password" binding:"required"`
		Database    string `json:"database" binding:"required"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 测试连接
	testConn := &models.DatabaseConnection{
		Type:     req.Type,
		Host:     req.Host,
		Port:     req.Port,
		Username: req.Username,
		Password: req.Password,
		Database: req.Database,
	}

	_, err := dbconn.GetConnection(testConn)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "数据库连接测试失败: " + err.Error()})
		return
	}

	// 创建连接记录
	dbConn := models.DatabaseConnection{
		Name:        req.Name,
		Type:        req.Type,
		Host:        req.Host,
		Port:        req.Port,
		Username:    req.Username,
		Password:    req.Password, // 实际应用中应该加密存储
		Database:    req.Database,
		Description: req.Description,
		Status:      "active",
	}

	if err := database.DB.Create(&dbConn).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建数据库连接失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "数据库连接创建成功",
		"data":    dbConn,
	})
}

// ListConnections 列出所有数据库连接
func (h *DBConnectionHandler) ListConnections(c *gin.Context) {
	var connections []models.DatabaseConnection
	if err := database.DB.Find(&connections).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": connections})
}

// GetConnection 获取单个数据库连接
func (h *DBConnectionHandler) GetConnection(c *gin.Context) {
	id := c.Param("id")

	var conn models.DatabaseConnection
	if err := database.DB.First(&conn, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "数据库连接不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": conn})
}

// UpdateConnection 更新数据库连接
func (h *DBConnectionHandler) UpdateConnection(c *gin.Context) {
	id := c.Param("id")

	var conn models.DatabaseConnection
	if err := database.DB.First(&conn, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "数据库连接不存在"})
		return
	}

	var req struct {
		Name        string `json:"name"`
		Host        string `json:"host"`
		Port        string `json:"port"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		Database    string `json:"database"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 更新字段
	if req.Name != "" {
		conn.Name = req.Name
	}
	if req.Host != "" {
		conn.Host = req.Host
	}
	if req.Port != "" {
		conn.Port = req.Port
	}
	if req.Username != "" {
		conn.Username = req.Username
	}
	if req.Password != "" {
		conn.Password = req.Password
	}
	if req.Database != "" {
		conn.Database = req.Database
	}
	if req.Description != "" {
		conn.Description = req.Description
	}
	if req.Status != "" {
		conn.Status = req.Status
	}

	if err := database.DB.Save(&conn).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "更新成功",
		"data":    conn,
	})
}

// DeleteConnection 删除数据库连接
func (h *DBConnectionHandler) DeleteConnection(c *gin.Context) {
	id := c.Param("id")

	var conn models.DatabaseConnection
	if err := database.DB.First(&conn, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "数据库连接不存在"})
		return
	}

	// 关闭连接
	dbconn.CloseConnection(conn.ID)

	// 删除记录
	if err := database.DB.Delete(&conn).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// TestConnection 测试数据库连接
func (h *DBConnectionHandler) TestConnection(c *gin.Context) {
	var req struct {
		Type     string `json:"type" binding:"required,oneof=mysql oracle postgres"`
		Host     string `json:"host" binding:"required"`
		Port     string `json:"port" binding:"required"`
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Database string `json:"database" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	testConn := &models.DatabaseConnection{
		Type:     req.Type,
		Host:     req.Host,
		Port:     req.Port,
		Username: req.Username,
		Password: req.Password,
		Database: req.Database,
	}

	_, err := dbconn.GetConnection(testConn)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "连接成功",
	})
}
