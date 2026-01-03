package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"zh.xyz/dv/sync/database"
	"zh.xyz/dv/sync/dbconn"
	"zh.xyz/dv/sync/models"
)

type QueryHandler struct{}

// QueryData 查询数据库数据
func (h *QueryHandler) QueryData(c *gin.Context) {
	var req struct {
		ConnectionID uint   `json:"connection_id" binding:"required"`
		TableName    string `json:"table_name" binding:"required"`
		Page         int    `json:"page"`         // 页码，从1开始
		PageSize     int    `json:"page_size"`   // 每页大小
		Condition    string `json:"condition"`   // WHERE条件（可选）
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100 // 限制最大页大小
	}

	// 获取数据库连接
	var dbConn models.DatabaseConnection
	if err := database.DB.First(&dbConn, req.ConnectionID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "数据库连接不存在"})
		return
	}

	rawConn, err := dbconn.GetRawConnection(&dbConn)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "连接数据库失败: " + err.Error()})
		return
	}
	defer rawConn.Close()

	// 测试连接是否可用
	if err := rawConn.Ping(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接不可用: " + err.Error()})
		return
	}

	// 构建查询SQL
	tableName := quoteIdentifier(req.TableName, dbConn.Type)
	whereClause := ""
	if req.Condition != "" {
		whereClause = "WHERE " + req.Condition
	}

	// 查询总数
	var total int
	countSQL := "SELECT COUNT(*) FROM " + tableName + " " + whereClause
	if err := rawConn.QueryRow(countSQL).Scan(&total); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询总数失败: " + err.Error()})
		return
	}

	// 查询数据
	offset := (req.Page - 1) * req.PageSize
	var limitClause string
	switch dbConn.Type {
	case "mysql", "postgres":
		limitClause = "LIMIT " + strconv.Itoa(req.PageSize) + " OFFSET " + strconv.Itoa(offset)
	case "oracle":
		limitClause = "OFFSET " + strconv.Itoa(offset) + " ROWS FETCH NEXT " + strconv.Itoa(req.PageSize) + " ROWS ONLY"
	default:
		limitClause = "LIMIT " + strconv.Itoa(req.PageSize) + " OFFSET " + strconv.Itoa(offset)
	}

	querySQL := "SELECT * FROM " + tableName + " " + whereClause + " " + limitClause
	rows, err := rawConn.Query(querySQL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询数据失败: " + err.Error()})
		return
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取列信息失败"})
		return
	}

	// 解析数据
	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		rowData := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				rowData[col] = string(b)
			} else if val == nil {
				rowData[col] = nil
			} else {
				rowData[col] = val
			}
		}

		results = append(results, rowData)
	}

	// 检查迭代过程中是否有错误
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取结果失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": results,
		"pagination": gin.H{
			"total":      total,
			"page":       req.Page,
			"page_size":  req.PageSize,
			"total_page": (total + req.PageSize - 1) / req.PageSize,
		},
	})
}

// GetTables 获取数据库所有表
func (h *QueryHandler) GetTables(c *gin.Context) {
	connectionID := c.Param("id")

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

	// 测试连接是否可用
	if err := rawConn.Ping(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接不可用: " + err.Error()})
		return
	}

	var query string
	switch dbConn.Type {
	case "mysql":
		query = "SHOW TABLES"
	case "postgres":
		// 使用 information_schema 更标准，兼容性更好
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY table_name"
	case "oracle":
		query = "SELECT table_name FROM user_tables"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的数据库类型"})
		return
	}

	rows, err := rawConn.Query(query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败: " + err.Error()})
		return
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "扫描结果失败: " + err.Error()})
			return
		}
		tables = append(tables, tableName)
	}

	// 检查迭代过程中是否有错误
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取结果失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": tables})
}

// ExecuteSQL 执行自定义SQL查询（只读）
func (h *QueryHandler) ExecuteSQL(c *gin.Context) {
	var req struct {
		ConnectionID uint   `json:"connection_id" binding:"required"`
		SQL          string `json:"sql" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 安全检查：只允许SELECT语句
	if len(req.SQL) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只允许执行SELECT查询语句"})
		return
	}
	sqlUpper := ""
	for i := 0; i < 6 && i < len(req.SQL); i++ {
		sqlUpper += string(req.SQL[i])
	}
	// 简单检查前6个字符是否为SELECT（不区分大小写）
	isSelect := (sqlUpper[0] == 'S' || sqlUpper[0] == 's') &&
		(sqlUpper[1] == 'E' || sqlUpper[1] == 'e') &&
		(sqlUpper[2] == 'L' || sqlUpper[2] == 'l') &&
		(sqlUpper[3] == 'E' || sqlUpper[3] == 'e') &&
		(sqlUpper[4] == 'C' || sqlUpper[4] == 'c') &&
		(sqlUpper[5] == 'T' || sqlUpper[5] == 't')
	if !isSelect {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只允许执行SELECT查询语句"})
		return
	}

	var dbConn models.DatabaseConnection
	if err := database.DB.First(&dbConn, req.ConnectionID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "数据库连接不存在"})
		return
	}

	rawConn, err := dbconn.GetRawConnection(&dbConn)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "连接数据库失败: " + err.Error()})
		return
	}
	defer rawConn.Close()

	// 测试连接是否可用
	if err := rawConn.Ping(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接不可用: " + err.Error()})
		return
	}

	rows, err := rawConn.Query(req.SQL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "执行SQL失败: " + err.Error()})
		return
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取列信息失败"})
		return
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		rowData := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				rowData[col] = string(b)
			} else if val == nil {
				rowData[col] = nil
			} else {
				rowData[col] = val
			}
		}

		results = append(results, rowData)
	}

	// 检查迭代过程中是否有错误
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取结果失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": results})
}

func quoteIdentifier(name, dbType string) string {
	switch dbType {
	case "mysql":
		return "`" + name + "`"
	case "postgres", "oracle":
		return `"` + name + `"`
	default:
		return name
	}
}
