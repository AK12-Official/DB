package service

import (
	"database/sql"
	"fmt"
	"strings"

	"zh.xyz/dv/sync/database"
	"zh.xyz/dv/sync/models"
)

// DatabaseObjectService 数据库对象服务
type DatabaseObjectService struct{}

// SyncDatabaseObjects 同步数据库对象（存储过程、触发器、视图、函数等）
func (s *DatabaseObjectService) SyncDatabaseObjects(sourceDB, targetDB *sql.DB, sourceConn, targetConn *models.DatabaseConnection, task *models.SyncTask) error {
	objectTypes := []string{"procedure", "function", "view", "trigger"}

	for _, objType := range objectTypes {
		if err := s.syncObjectsByType(sourceDB, targetDB, sourceConn, targetConn, task, objType); err != nil {
			s.logObjectSync(task.ID, objType, "", "sync", "failed", fmt.Sprintf("同步%s失败: %v", objType, err))
			continue
		}
	}

	return nil
}

// syncObjectsByType 按类型同步数据库对象
func (s *DatabaseObjectService) syncObjectsByType(sourceDB, targetDB *sql.DB, sourceConn, targetConn *models.DatabaseConnection, task *models.SyncTask, objType string) error {
	// 获取源数据库对象列表
	sourceObjects, err := s.getObjects(sourceDB, sourceConn.Type, sourceConn.Database, objType)
	if err != nil {
		return fmt.Errorf("获取源数据库%s列表失败: %v", objType, err)
	}

	// 获取目标数据库对象列表
	targetObjects, err := s.getObjects(targetDB, targetConn.Type, targetConn.Database, objType)
	if err != nil {
		return fmt.Errorf("获取目标数据库%s列表失败: %v", objType, err)
	}

	// 创建目标对象名称映射
	targetMap := make(map[string]bool)
	for _, obj := range targetObjects {
		targetMap[strings.ToLower(obj.Name)] = true
	}

	// 同步每个对象
	for _, sourceObj := range sourceObjects {
		objNameLower := strings.ToLower(sourceObj.Name)

		// 获取对象定义
		definition, err := s.getObjectDefinition(sourceDB, sourceConn.Type, sourceConn.Database, objType, sourceObj.Name, sourceObj.TableName)
		if err != nil {
			s.logObjectSync(task.ID, objType, sourceObj.Name, "sync", "failed", fmt.Sprintf("获取%s定义失败: %v", sourceObj.Name, err))
			continue
		}

		// 如果目标数据库已存在，先删除（某些数据库需要）
		if targetMap[objNameLower] {
			if err := s.dropObject(targetDB, targetConn.Type, objType, sourceObj.Name, sourceObj.TableName); err != nil {
				s.logObjectSync(task.ID, objType, sourceObj.Name, "delete", "failed", fmt.Sprintf("删除旧%s失败: %v", sourceObj.Name, err))
			}
		}

		// 转换并创建对象
		convertedDefinition := s.convertDefinition(definition, sourceConn.Type, targetConn.Type, objType)
		if err := s.createObject(targetDB, targetConn.Type, convertedDefinition, objType); err != nil {
			s.logObjectSync(task.ID, objType, sourceObj.Name, "create", "failed", fmt.Sprintf("创建%s失败: %v", sourceObj.Name, err))
			continue
		}

		action := "update"
		if !targetMap[objNameLower] {
			action = "create"
		}
		s.logObjectSync(task.ID, objType, sourceObj.Name, action, "success", fmt.Sprintf("%s同步成功", sourceObj.Name))
	}

	return nil
}

// DatabaseObjectInfo 数据库对象信息
type DatabaseObjectInfo struct {
	Name      string
	TableName string
	Schema    string
}

// GetObjectsByType 获取指定类型的数据库对象列表（公开方法）
func (s *DatabaseObjectService) GetObjectsByType(db *sql.DB, dbType, dbName, objType string) ([]DatabaseObjectInfo, error) {
	return s.getObjects(db, dbType, dbName, objType)
}

// GetObjectDefinitionPublic 获取对象定义（公开方法）
func (s *DatabaseObjectService) GetObjectDefinitionPublic(db *sql.DB, dbType, dbName, objType, objName, tableName string) (string, error) {
	return s.getObjectDefinition(db, dbType, dbName, objType, objName, tableName)
}

// getObjects 获取数据库对象列表
func (s *DatabaseObjectService) getObjects(db *sql.DB, dbType, dbName, objType string) ([]DatabaseObjectInfo, error) {
	var query string
	var args []interface{}

	switch objType {
	case "procedure":
		query, args = s.getProceduresQuery(dbType, dbName)
	case "function":
		query, args = s.getFunctionsQuery(dbType, dbName)
	case "view":
		query, args = s.getViewsQuery(dbType, dbName)
	case "trigger":
		query, args = s.getTriggersQuery(dbType, dbName)
	default:
		return nil, fmt.Errorf("不支持的对象类型: %s", objType)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var objects []DatabaseObjectInfo
	for rows.Next() {
		var obj DatabaseObjectInfo
		var err error

		switch objType {
		case "procedure", "function":
			err = rows.Scan(&obj.Name, &obj.Schema)
		case "view":
			err = rows.Scan(&obj.Name, &obj.Schema)
		case "trigger":
			err = rows.Scan(&obj.Name, &obj.TableName, &obj.Schema)
		}

		if err != nil {
			continue
		}

		objects = append(objects, obj)
	}

	return objects, nil
}

// getProceduresQuery 获取存储过程查询SQL
func (s *DatabaseObjectService) getProceduresQuery(dbType, dbName string) (string, []interface{}) {
	switch dbType {
	case "mysql":
		return "SELECT ROUTINE_NAME, ROUTINE_SCHEMA FROM information_schema.ROUTINES WHERE ROUTINE_TYPE = 'PROCEDURE' AND ROUTINE_SCHEMA = ?", []interface{}{dbName}
	case "postgres":
		return "SELECT routine_name, routine_schema FROM information_schema.routines WHERE routine_type = 'PROCEDURE' AND routine_schema NOT IN ('pg_catalog', 'information_schema')", []interface{}{}
	case "oracle":
		return "SELECT object_name, owner FROM all_procedures WHERE object_type = 'PROCEDURE' AND owner = USER", []interface{}{}
	default:
		return "", nil
	}
}

// getFunctionsQuery 获取函数查询SQL
func (s *DatabaseObjectService) getFunctionsQuery(dbType, dbName string) (string, []interface{}) {
	switch dbType {
	case "mysql":
		return "SELECT ROUTINE_NAME, ROUTINE_SCHEMA FROM information_schema.ROUTINES WHERE ROUTINE_TYPE = 'FUNCTION' AND ROUTINE_SCHEMA = ?", []interface{}{dbName}
	case "postgres":
		return "SELECT routine_name, routine_schema FROM information_schema.routines WHERE routine_type = 'FUNCTION' AND routine_schema NOT IN ('pg_catalog', 'information_schema')", []interface{}{}
	case "oracle":
		return "SELECT object_name, owner FROM all_objects WHERE object_type = 'FUNCTION' AND owner = USER", []interface{}{}
	default:
		return "", nil
	}
}

// getViewsQuery 获取视图查询SQL
func (s *DatabaseObjectService) getViewsQuery(dbType, dbName string) (string, []interface{}) {
	switch dbType {
	case "mysql":
		return "SELECT TABLE_NAME, TABLE_SCHEMA FROM information_schema.VIEWS WHERE TABLE_SCHEMA = ?", []interface{}{dbName}
	case "postgres":
		return "SELECT table_name, table_schema FROM information_schema.views WHERE table_schema NOT IN ('pg_catalog', 'information_schema')", []interface{}{}
	case "oracle":
		return "SELECT view_name, owner FROM all_views WHERE owner = USER", []interface{}{}
	default:
		return "", nil
	}
}

// getTriggersQuery 获取触发器查询SQL
func (s *DatabaseObjectService) getTriggersQuery(dbType, dbName string) (string, []interface{}) {
	switch dbType {
	case "mysql":
		return "SELECT TRIGGER_NAME, EVENT_OBJECT_TABLE, TRIGGER_SCHEMA FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = ?", []interface{}{dbName}
	case "postgres":
		return "SELECT trigger_name, event_object_table, trigger_schema FROM information_schema.triggers WHERE trigger_schema NOT IN ('pg_catalog', 'information_schema')", []interface{}{}
	case "oracle":
		return "SELECT trigger_name, table_name, owner FROM all_triggers WHERE owner = USER", []interface{}{}
	default:
		return "", nil
	}
}

// getObjectDefinition 获取对象定义
func (s *DatabaseObjectService) getObjectDefinition(db *sql.DB, dbType, dbName, objType, objName, tableName string) (string, error) {
	var query string
	var args []interface{}

	switch dbType {
	case "mysql":
		switch objType {
		case "procedure", "function":
			query = "SHOW CREATE " + strings.ToUpper(objType) + " " + quoteIdentifier(objName, dbType)
		case "view":
			query = "SHOW CREATE VIEW " + quoteIdentifier(objName, dbType)
		case "trigger":
			query = "SHOW CREATE TRIGGER " + quoteIdentifier(objName, dbType)
		}
	case "postgres":
		switch objType {
		case "procedure", "function":
			query = fmt.Sprintf("SELECT pg_get_functiondef(oid) FROM pg_proc WHERE proname = $1")
			args = []interface{}{objName}
		case "view":
			query = fmt.Sprintf("SELECT definition FROM pg_views WHERE viewname = $1")
			args = []interface{}{objName}
		case "trigger":
			query = fmt.Sprintf("SELECT pg_get_triggerdef(oid) FROM pg_trigger WHERE tgname = $1")
			args = []interface{}{objName}
		}
	case "oracle":
		switch objType {
		case "procedure", "function":
			query = "SELECT text FROM all_source WHERE type = ? AND name = ? AND owner = USER ORDER BY line"
			args = []interface{}{strings.ToUpper(objType), objName}
		case "view":
			query = "SELECT text FROM all_views WHERE view_name = ? AND owner = USER"
			args = []interface{}{objName}
		case "trigger":
			query = "SELECT trigger_body FROM all_triggers WHERE trigger_name = ? AND owner = USER"
			args = []interface{}{objName}
		}
	}

	if query == "" {
		return "", fmt.Errorf("不支持获取%s的定义", objType)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var definition strings.Builder
	var line string

	for rows.Next() {
		if err := rows.Scan(&line); err != nil {
			continue
		}
		definition.WriteString(line)
		if dbType == "oracle" && objType != "trigger" {
			definition.WriteString("\n")
		}
	}

	result := definition.String()

	// MySQL的SHOW CREATE结果需要解析
	if dbType == "mysql" {
		result = s.parseMySQLShowCreate(result, objType)
	}

	return result, nil
}

// parseMySQLShowCreate 解析MySQL的SHOW CREATE结果
func (s *DatabaseObjectService) parseMySQLShowCreate(result, objType string) string {
	// MySQL返回的格式类似：Create Procedure: CREATE PROCEDURE name() ...
	// 需要提取CREATE语句部分
	titleObjType := strings.ToUpper(objType[:1]) + strings.ToLower(objType[1:])
	if strings.Contains(result, "Create "+titleObjType+":") {
		parts := strings.SplitN(result, ":", 2)
		if len(parts) == 2 {
			return strings.TrimSpace(parts[1])
		}
	}
	return result
}

// dropObject 删除数据库对象
func (s *DatabaseObjectService) dropObject(db *sql.DB, dbType, objType, objName, tableName string) error {
	var dropSQL string

	switch dbType {
	case "mysql":
		switch objType {
		case "procedure", "function":
			dropSQL = fmt.Sprintf("DROP %s IF EXISTS %s", strings.ToUpper(objType), quoteIdentifier(objName, dbType))
		case "view":
			dropSQL = fmt.Sprintf("DROP VIEW IF EXISTS %s", quoteIdentifier(objName, dbType))
		case "trigger":
			dropSQL = fmt.Sprintf("DROP TRIGGER IF EXISTS %s", quoteIdentifier(objName, dbType))
		}
	case "postgres":
		switch objType {
		case "procedure", "function":
			dropSQL = fmt.Sprintf("DROP %s IF EXISTS %s CASCADE", strings.ToUpper(objType), quoteIdentifier(objName, dbType))
		case "view":
			dropSQL = fmt.Sprintf("DROP VIEW IF EXISTS %s CASCADE", quoteIdentifier(objName, dbType))
		case "trigger":
			dropSQL = fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON %s CASCADE", quoteIdentifier(objName, dbType), quoteIdentifier(tableName, dbType))
		}
	case "oracle":
		switch objType {
		case "procedure", "function":
			dropSQL = fmt.Sprintf("DROP %s %s", strings.ToUpper(objType), quoteIdentifier(objName, dbType))
		case "view":
			dropSQL = fmt.Sprintf("DROP VIEW %s", quoteIdentifier(objName, dbType))
		case "trigger":
			dropSQL = fmt.Sprintf("DROP TRIGGER %s", quoteIdentifier(objName, dbType))
		}
	}

	if dropSQL == "" {
		return fmt.Errorf("不支持删除%s", objType)
	}

	_, err := db.Exec(dropSQL)
	return err
}

// createObject 创建数据库对象
func (s *DatabaseObjectService) createObject(db *sql.DB, dbType, definition, objType string) error {
	// 直接执行定义SQL
	_, err := db.Exec(definition)
	return err
}

// convertDefinition 转换对象定义（在不同数据库类型间转换）
func (s *DatabaseObjectService) convertDefinition(definition, sourceType, targetType, objType string) string {
	// 如果源和目标数据库类型相同，直接返回
	if sourceType == targetType {
		return definition
	}

	// 这里可以实现不同数据库间的语法转换
	// 由于不同数据库语法差异较大，当前实现为基础版本
	// 实际应用中可能需要更复杂的转换逻辑

	converted := definition

	if sourceType == "mysql" && targetType == "postgres" {
		// MySQL到PostgreSQL的转换
		converted = strings.ReplaceAll(converted, "`", `"`)
		converted = strings.ReplaceAll(converted, "AUTO_INCREMENT", "SERIAL")
	}

	if sourceType == "postgres" && targetType == "mysql" {
		// PostgreSQL到MySQL的转换
		converted = strings.ReplaceAll(converted, `"`, "`")
		converted = strings.ReplaceAll(converted, "SERIAL", "AUTO_INCREMENT")
	}

	return converted
}

// logObjectSync 记录对象同步日志
func (s *DatabaseObjectService) logObjectSync(taskID uint, objType, objName, action, status, message string) {
	log := models.ObjectSyncLog{
		TaskID:     taskID,
		ObjectType: objType,
		ObjectName: objName,
		Action:     action,
		Status:     status,
		Message:    message,
	}
	database.DB.Create(&log)
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
