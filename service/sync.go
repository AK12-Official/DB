package service

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"zh.xyz/dv/sync/database"
	"zh.xyz/dv/sync/dbconn"
	"zh.xyz/dv/sync/models"
)

// SyncService 同步服务
type SyncService struct{}

// SyncTable 同步表数据
func (s *SyncService) SyncTable(task *models.SyncTask) error {
	// 获取源数据库和目标数据库连接
	var sourceDB, targetDB models.DatabaseConnection
	if err := database.DB.First(&sourceDB, task.SourceDBID).Error; err != nil {
		return fmt.Errorf("源数据库连接不存在: %v", err)
	}
	if err := database.DB.First(&targetDB, task.TargetDBID).Error; err != nil {
		return fmt.Errorf("目标数据库连接不存在: %v", err)
	}

	// 获取源数据库原生连接用于查询
	sourceRaw, err := dbconn.GetRawConnection(&sourceDB)
	if err != nil {
		return fmt.Errorf("获取源数据库原生连接失败: %v", err)
	}
	defer sourceRaw.Close()

	// 测试源数据库连接
	if err := sourceRaw.Ping(); err != nil {
		return fmt.Errorf("源数据库连接不可用: %v", err)
	}

	targetRaw, err := dbconn.GetRawConnection(&targetDB)
	if err != nil {
		return fmt.Errorf("获取目标数据库原生连接失败: %v", err)
	}
	defer targetRaw.Close()

	// 测试目标数据库连接
	if err := targetRaw.Ping(); err != nil {
		return fmt.Errorf("目标数据库连接不可用: %v", err)
	}

	// 如果表名为空，同步整个数据库
	if task.TableName == "" {
		return s.syncDatabase(sourceRaw, targetRaw, &sourceDB, &targetDB, task)
	}

	// 同步单个表
	return s.syncSingleTable(sourceRaw, targetRaw, &sourceDB, &targetDB, task)
}

// syncDatabase 同步整个数据库
func (s *SyncService) syncDatabase(sourceDB, targetDB *sql.DB, sourceConn, targetConn *models.DatabaseConnection, task *models.SyncTask) error {
	// 1. 获取源数据库所有表
	tables, err := s.getTables(sourceDB, sourceConn.Type)
	if err != nil {
		return err
	}

	// 2. 先同步所有表的结构和数据（数据库对象依赖表，必须先创建表）
	for _, tableName := range tables {
		task.TableName = tableName
		if err := s.syncSingleTable(sourceDB, targetDB, sourceConn, targetConn, task); err != nil {
			s.logError(task.ID, fmt.Sprintf("同步表 %s 失败: %v", tableName, err))
			continue
		}
	}

	// 3. 表同步完成后，再同步数据库对象（存储过程、触发器、视图、函数等）
	// 注意：对象同步顺序很重要，应该按依赖关系：视图 → 存储过程/函数 → 触发器
	objectService := &DatabaseObjectService{}
	
	// 先同步视图（可能依赖表，但不依赖其他对象）
	if err := objectService.SyncObjectsByType(sourceDB, targetDB, sourceConn, targetConn, task, "view"); err != nil {
		s.logError(task.ID, fmt.Sprintf("同步视图失败: %v", err))
	}
	
	// 再同步存储过程和函数（可能引用表，但不依赖触发器）
	if err := objectService.SyncObjectsByType(sourceDB, targetDB, sourceConn, targetConn, task, "procedure"); err != nil {
		s.logError(task.ID, fmt.Sprintf("同步存储过程失败: %v", err))
	}
	if err := objectService.SyncObjectsByType(sourceDB, targetDB, sourceConn, targetConn, task, "function"); err != nil {
		s.logError(task.ID, fmt.Sprintf("同步函数失败: %v", err))
	}
	
	// 最后同步触发器（依赖表，必须最后同步）
	if err := objectService.SyncObjectsByType(sourceDB, targetDB, sourceConn, targetConn, task, "trigger"); err != nil {
		s.logError(task.ID, fmt.Sprintf("同步触发器失败: %v", err))
	}

	return nil
}

// syncSingleTable 同步单个表
func (s *SyncService) syncSingleTable(sourceDB, targetDB *sql.DB, sourceConn, targetConn *models.DatabaseConnection, task *models.SyncTask) error {
	tableName := task.TableName

	// 1. 确保表结构一致
	if err := s.syncTableStructure(sourceDB, targetDB, sourceConn, targetConn, tableName); err != nil {
		return fmt.Errorf("同步表结构失败: %v", err)
	}

	// 2. 获取主键信息
	primaryKeys, err := s.getPrimaryKeys(sourceDB, sourceConn.Type, tableName)
	if err != nil {
		return fmt.Errorf("获取主键失败: %v", err)
	}

	// 3. 查询源表数据
	sourceRows, err := sourceDB.Query(fmt.Sprintf("SELECT * FROM %s", s.quoteIdentifier(tableName, sourceConn.Type)))
	if err != nil {
		return fmt.Errorf("查询源表数据失败: %v", err)
	}
	defer sourceRows.Close()

	columns, err := sourceRows.Columns()
	if err != nil {
		return err
	}

	// 4. 批量处理数据
	batchSize := 100
	batch := make([]map[string]interface{}, 0, batchSize)

	for sourceRows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := sourceRows.Scan(valuePtrs...); err != nil {
			continue
		}

		rowData := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			rowData[col] = s.normalizeValue(val)
		}

		batch = append(batch, rowData)

		if len(batch) >= batchSize {
			if err := s.syncBatch(targetDB, targetConn, tableName, batch, primaryKeys); err != nil {
				s.logError(task.ID, fmt.Sprintf("批量同步失败: %v", err))
			}
			batch = batch[:0]
		}
	}

	// 处理剩余数据
	if len(batch) > 0 {
		if err := s.syncBatch(targetDB, targetConn, tableName, batch, primaryKeys); err != nil {
			return err
		}
	}

	// 5. 检查冲突
	return s.checkConflicts(sourceDB, targetDB, sourceConn, targetConn, task, tableName, primaryKeys)
}

// syncTableStructure 同步表结构（简化版本，实际应该使用更复杂的DDL同步）
func (s *SyncService) syncTableStructure(sourceDB, targetDB *sql.DB, sourceConn, targetConn *models.DatabaseConnection, tableName string) error {
	// 检查目标表是否存在
	exists, err := s.tableExists(targetDB, targetConn.Type, tableName)
	if err != nil {
		return err
	}

	if !exists {
		// 创建表（需要根据源表结构生成DDL）
		// 这里简化处理，实际应该解析源表结构并生成目标数据库的DDL
		s.logInfo(0, fmt.Sprintf("目标表 %s 不存在，需要创建", tableName))
		// TODO: 实现DDL同步逻辑
	}

	return nil
}

// syncBatch 批量同步数据
func (s *SyncService) syncBatch(targetDB *sql.DB, targetConn *models.DatabaseConnection, tableName string, batch []map[string]interface{}, primaryKeys []string) error {
	if len(batch) == 0 {
		return nil
	}

	// 获取所有列名（使用 map 的键，但需要保持顺序）
	// 为了保持一致性，我们从第一条记录获取列名
	columns := make([]string, 0, len(batch[0]))
	columnMap := make(map[string]bool)
	for col := range batch[0] {
		columns = append(columns, col)
		columnMap[col] = true
	}

	// 验证所有记录都有相同的列
	for i, row := range batch {
		if len(row) != len(columns) {
			return fmt.Errorf("记录 %d 的列数与第一条记录不匹配", i)
		}
		for col := range row {
			if !columnMap[col] {
				return fmt.Errorf("记录 %d 包含未知列: %s", i, col)
			}
		}
	}

	quotedTableName := s.quoteIdentifier(tableName, targetConn.Type)

	// 根据数据库类型使用不同的 UPSERT 策略
	switch targetConn.Type {
	case "mysql":
		return s.syncBatchMySQL(targetDB, quotedTableName, batch, columns, primaryKeys)
	case "postgres":
		return s.syncBatchPostgres(targetDB, quotedTableName, batch, columns, primaryKeys)
	case "oracle":
		return s.syncBatchOracle(targetDB, quotedTableName, batch, columns, primaryKeys)
	default:
		return fmt.Errorf("不支持的数据库类型: %s", targetConn.Type)
	}
}

// syncBatchMySQL 使用 MySQL 的 INSERT ... ON DUPLICATE KEY UPDATE
func (s *SyncService) syncBatchMySQL(targetDB *sql.DB, tableName string, batch []map[string]interface{}, columns []string, primaryKeys []string) error {
	if len(batch) == 0 {
		return nil
	}

	// 构建 INSERT 语句
	placeholders := ""
	for i := 0; i < len(columns); i++ {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}

	// 构建列名列表
	columnList := ""
	for i, col := range columns {
		if i > 0 {
			columnList += ","
		}
		columnList += fmt.Sprintf("`%s`", col)
	}

	// 构建 ON DUPLICATE KEY UPDATE 子句
	updateClause := ""
	for i, col := range columns {
		if i > 0 {
			updateClause += ","
		}
		updateClause += fmt.Sprintf("`%s`=VALUES(`%s`)", col, col)
	}

	// 构建批量插入的占位符
	batchPlaceholders := ""
	for i := 0; i < len(batch); i++ {
		if i > 0 {
			batchPlaceholders += ","
		}
		batchPlaceholders += "(" + placeholders + ")"
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s ON DUPLICATE KEY UPDATE %s",
		tableName, columnList, batchPlaceholders, updateClause)

	// 准备参数，确保字符串值是有效的 UTF-8
	args := make([]interface{}, 0, len(batch)*len(columns))
	for _, row := range batch {
		for _, col := range columns {
			args = append(args, s.sanitizeValueForPostgres(row[col]))
		}
	}

	_, err := targetDB.Exec(sql, args...)
	return err
}

// syncBatchPostgres 使用 PostgreSQL 的 INSERT ... ON CONFLICT ... DO UPDATE
func (s *SyncService) syncBatchPostgres(targetDB *sql.DB, tableName string, batch []map[string]interface{}, columns []string, primaryKeys []string) error {
	if len(batch) == 0 {
		return nil
	}

	// 构建列名列表
	columnList := ""
	for i, col := range columns {
		if i > 0 {
			columnList += ","
		}
		columnList += fmt.Sprintf(`"%s"`, col)
	}

	// 如果没有主键，使用简单的 INSERT（可能会因为唯一约束失败，但这是预期行为）
	if len(primaryKeys) == 0 {
		return s.syncBatchPostgresSimpleInsert(targetDB, tableName, batch, columns)
	}

	// 构建冲突目标（主键列）
	conflictTarget := ""
	for i, pk := range primaryKeys {
		if i > 0 {
			conflictTarget += ","
		}
		conflictTarget += fmt.Sprintf(`"%s"`, pk)
	}

	// 构建 UPDATE 子句
	updateClause := ""
	for i, col := range columns {
		if i > 0 {
			updateClause += ","
		}
		updateClause += fmt.Sprintf(`"%s"=EXCLUDED."%s"`, col, col)
	}

	// 为每条记录构建 VALUES 子句
	valuesClauses := make([]string, 0, len(batch))
	args := make([]interface{}, 0, len(batch)*len(columns))
	argIndex := 1

	for _, row := range batch {
		placeholders := ""
		for i := 0; i < len(columns); i++ {
			if i > 0 {
				placeholders += ","
			}
			placeholders += fmt.Sprintf("$%d", argIndex)
			// 确保字符串值是有效的 UTF-8
			args = append(args, s.sanitizeValueForPostgres(row[columns[i]]))
			argIndex++
		}
		valuesClauses = append(valuesClauses, "("+placeholders+")")
	}

	valuesClause := ""
	for i, v := range valuesClauses {
		if i > 0 {
			valuesClause += ","
		}
		valuesClause += v
	}

	sql := fmt.Sprintf(`INSERT INTO %s (%s) VALUES %s ON CONFLICT (%s) DO UPDATE SET %s`,
		tableName, columnList, valuesClause, conflictTarget, updateClause)

	_, err := targetDB.Exec(sql, args...)
	return err
}

// syncBatchPostgresSimpleInsert 当没有主键时，使用简单的 INSERT
func (s *SyncService) syncBatchPostgresSimpleInsert(targetDB *sql.DB, tableName string, batch []map[string]interface{}, columns []string) error {
	// 构建列名列表
	columnList := ""
	for i, col := range columns {
		if i > 0 {
			columnList += ","
		}
		columnList += fmt.Sprintf(`"%s"`, col)
	}

	// 为每条记录构建 VALUES 子句
	valuesClauses := make([]string, 0, len(batch))
	args := make([]interface{}, 0, len(batch)*len(columns))
	argIndex := 1

	for _, row := range batch {
		placeholders := ""
		for i := 0; i < len(columns); i++ {
			if i > 0 {
				placeholders += ","
			}
			placeholders += fmt.Sprintf("$%d", argIndex)
			// 确保字符串值是有效的 UTF-8
			args = append(args, s.sanitizeValueForPostgres(row[columns[i]]))
			argIndex++
		}
		valuesClauses = append(valuesClauses, "("+placeholders+")")
	}

	valuesClause := ""
	for i, v := range valuesClauses {
		if i > 0 {
			valuesClause += ","
		}
		valuesClause += v
	}

	sql := fmt.Sprintf(`INSERT INTO %s (%s) VALUES %s`, tableName, columnList, valuesClause)
	_, err := targetDB.Exec(sql, args...)
	return err
}

// syncBatchOracle 使用 Oracle 的 MERGE 语句
func (s *SyncService) syncBatchOracle(targetDB *sql.DB, tableName string, batch []map[string]interface{}, columns []string, primaryKeys []string) error {
	if len(batch) == 0 {
		return nil
	}

	// Oracle 使用 MERGE 语句
	// 由于 Oracle 的 MERGE 语法较复杂，这里使用逐条处理的方式
	for _, row := range batch {
		// 构建主键条件
		whereClause := ""
		whereArgs := make([]interface{}, 0)
		if len(primaryKeys) > 0 {
			for i, pk := range primaryKeys {
				if i > 0 {
					whereClause += " AND "
				}
				whereClause += fmt.Sprintf(`"%s"=:pk%d`, pk, i+1)
				whereArgs = append(whereArgs, row[pk])
			}
		}

		// 检查记录是否存在
		checkSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", tableName, whereClause)
		var count int
		err := targetDB.QueryRow(checkSQL, whereArgs...).Scan(&count)
		if err != nil {
			return fmt.Errorf("检查记录是否存在失败: %v", err)
		}

		if count > 0 {
			// 更新
			setClause := ""
			updateArgs := make([]interface{}, 0)
			argIndex := 1
			for i, col := range columns {
				if i > 0 {
					setClause += ","
				}
				setClause += fmt.Sprintf(`"%s"=:val%d`, col, argIndex)
				updateArgs = append(updateArgs, row[col])
				argIndex++
			}
			updateArgs = append(updateArgs, whereArgs...)

			updateSQL := fmt.Sprintf("UPDATE %s SET %s WHERE %s", tableName, setClause, whereClause)
			_, err = targetDB.Exec(updateSQL, updateArgs...)
			if err != nil {
				return fmt.Errorf("更新记录失败: %v", err)
			}
		} else {
			// 插入
			columnList := ""
			placeholders := ""
			insertArgs := make([]interface{}, 0)
			argIndex := 1
			for i, col := range columns {
				if i > 0 {
					columnList += ","
					placeholders += ","
				}
				columnList += fmt.Sprintf(`"%s"`, col)
				placeholders += fmt.Sprintf(":val%d", argIndex)
				insertArgs = append(insertArgs, row[col])
				argIndex++
			}

			insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tableName, columnList, placeholders)
			_, err = targetDB.Exec(insertSQL, insertArgs...)
			if err != nil {
				return fmt.Errorf("插入记录失败: %v", err)
			}
		}
	}

	return nil
}

// checkConflicts 检查数据冲突
func (s *SyncService) checkConflicts(sourceDB, targetDB *sql.DB, sourceConn, targetConn *models.DatabaseConnection, task *models.SyncTask, tableName string, primaryKeys []string) error {
	if len(primaryKeys) == 0 {
		return nil // 没有主键，无法检测冲突
	}

	// 查询源数据库和目标数据库的所有数据
	sourceRows, err := sourceDB.Query(fmt.Sprintf("SELECT * FROM %s", s.quoteIdentifier(tableName, sourceConn.Type)))
	if err != nil {
		return err
	}
	defer sourceRows.Close()

	targetRows, err := targetDB.Query(fmt.Sprintf("SELECT * FROM %s", s.quoteIdentifier(tableName, targetConn.Type)))
	if err != nil {
		return err
	}
	defer targetRows.Close()

	// 构建主键条件SQL
	sourceCols, _ := sourceRows.Columns()
	targetCols, _ := targetRows.Columns()

	// 将数据加载到内存进行比较（实际应用中应该使用更高效的方法）
	sourceDataMap := make(map[string]map[string]interface{})
	sourceRows2, _ := sourceDB.Query(fmt.Sprintf("SELECT * FROM %s", s.quoteIdentifier(tableName, sourceConn.Type)))
	defer sourceRows2.Close()
	for sourceRows2.Next() {
		values := make([]interface{}, len(sourceCols))
		valuePtrs := make([]interface{}, len(sourceCols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		sourceRows2.Scan(valuePtrs...)

		rowData := make(map[string]interface{})
		for i, col := range sourceCols {
			val := values[i]
			rowData[col] = s.normalizeValue(val)
		}

		// 构建主键字符串
		pkValue := s.buildPrimaryKeyValue(rowData, primaryKeys)
		sourceDataMap[pkValue] = rowData
	}

	// 比较目标数据库数据
	targetRows2, _ := targetDB.Query(fmt.Sprintf("SELECT * FROM %s", s.quoteIdentifier(tableName, targetConn.Type)))
	defer targetRows2.Close()
	for targetRows2.Next() {
		values := make([]interface{}, len(targetCols))
		valuePtrs := make([]interface{}, len(targetCols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		targetRows2.Scan(valuePtrs...)

		rowData := make(map[string]interface{})
		for i, col := range targetCols {
			val := values[i]
			rowData[col] = s.normalizeValue(val)
		}

		pkValue := s.buildPrimaryKeyValue(rowData, primaryKeys)
		sourceRow, exists := sourceDataMap[pkValue]

		if exists {
			// 比较数据是否一致
			if !s.compareRows(sourceRow, rowData) {
				// 发现冲突
				if err := s.createConflict(task, tableName, pkValue, sourceRow, rowData, "update_conflict"); err != nil {
					s.logError(task.ID, fmt.Sprintf("创建冲突记录失败: %v", err))
				}
			}
			delete(sourceDataMap, pkValue)
		}
	}

	return nil
}

// buildPrimaryKeyValue 构建主键值字符串
func (s *SyncService) buildPrimaryKeyValue(row map[string]interface{}, primaryKeys []string) string {
	pkMap := make(map[string]interface{})
	for _, key := range primaryKeys {
		if val, ok := row[key]; ok {
			pkMap[key] = val
		}
	}
	data, _ := json.Marshal(pkMap)
	return string(data)
}

// compareRows 比较两行数据是否一致
func (s *SyncService) compareRows(row1, row2 map[string]interface{}) bool {
	if len(row1) != len(row2) {
		return false
	}

	for k, v1 := range row1 {
		v2, ok := row2[k]
		if !ok {
			return false
		}
		// 使用规范化比较，处理时间类型等特殊情况
		if !s.valuesEqual(v1, v2) {
			return false
		}
	}

	return true
}

// valuesEqual 比较两个值是否相等，处理时间类型等特殊情况
func (s *SyncService) valuesEqual(v1, v2 interface{}) bool {
	// 如果都是 nil，相等
	if v1 == nil && v2 == nil {
		return true
	}
	// 如果一个是 nil，另一个不是，不相等
	if v1 == nil || v2 == nil {
		return false
	}

	// 尝试将值转换为时间类型进行比较
	t1, ok1 := s.parseTimeValue(v1)
	t2, ok2 := s.parseTimeValue(v2)

	// 如果两个值都能解析为时间，则比较时间
	if ok1 && ok2 {
		// 比较时间（忽略纳秒精度差异，只比较到秒）
		return t1.Unix() == t2.Unix()
	}

	// 如果只有一个能解析为时间，不相等
	if ok1 || ok2 {
		return false
	}

	// 对于其他类型，使用标准比较
	return v1 == v2
}

// parseTimeValue 尝试将值解析为时间类型
func (s *SyncService) parseTimeValue(v interface{}) (time.Time, bool) {
	// 处理字符串类型的时间值
	if str, ok := v.(string); ok {
		// 尝试多种时间格式
		timeFormats := []string{
			time.RFC3339,                    // 2006-01-02T15:04:05Z07:00
			time.RFC3339Nano,                // 2006-01-02T15:04:05.999999999Z07:00
			"2006-01-02 15:04:05",           // MySQL datetime 格式
			"2006-01-02T15:04:05",           // ISO 8601 无时区
			"2006-01-02T15:04:05Z",          // UTC 时间
			"2006-01-02T15:04:05.000Z",      // UTC 时间（毫秒）
			"2006-01-02T15:04:05.000000Z",   // UTC 时间（微秒）
			"2006-01-02T15:04:05.000000000Z", // UTC 时间（纳秒）
			"2006-01-02 15:04:05.000000",    // MySQL datetime(6)
			"2006-01-02 15:04:05.000",       // MySQL datetime(3)
		}

		for _, format := range timeFormats {
			if t, err := time.Parse(format, str); err == nil {
				return t, true
			}
		}

		// 尝试解析带时区偏移的格式（如 +08:00）
		if strings.Contains(str, "+") || strings.HasSuffix(str, "Z") {
			// 移除时区信息，统一转换为 UTC 进行比较
			if t, err := time.Parse(time.RFC3339, str); err == nil {
				return t.UTC(), true
			}
			// 尝试其他带时区的格式
			if t, err := time.Parse("2006-01-02T15:04:05-07:00", str); err == nil {
				return t.UTC(), true
			}
			if t, err := time.Parse("2006-01-02T15:04:05.000-07:00", str); err == nil {
				return t.UTC(), true
			}
		}
	}

	// 处理 time.Time 类型
	if t, ok := v.(time.Time); ok {
		return t.UTC(), true
	}

	return time.Time{}, false
}

// createConflict 创建冲突记录并发送通知
func (s *SyncService) createConflict(task *models.SyncTask, tableName, primaryKey string, sourceData, targetData map[string]interface{}, conflictType string) error {
	sourceDataJSON, _ := json.Marshal(sourceData)
	targetDataJSON, _ := json.Marshal(targetData)

	conflict := models.DataConflict{
		TaskID:       task.ID,
		TableName:    tableName,
		PrimaryKey:   primaryKey,
		SourceData:   string(sourceDataJSON),
		TargetData:   string(targetDataJSON),
		ConflictType: conflictType,
		Status:       "pending",
	}

	if err := database.DB.Create(&conflict).Error; err != nil {
		return err
	}

	// 获取管理员邮箱并发送通知
	var admins []models.User
	database.DB.Where("role = ? AND status = ?", "admin", "active").Find(&admins)

	for _, admin := range admins {
		token, err := GenerateConflictToken(conflict.ID, admin.ID, admin.Username)
		if err != nil {
			continue
		}

		if err := SendConflictNotification(admin.Email, conflict.ID, token, conflictType); err != nil {
			s.logError(task.ID, fmt.Sprintf("发送冲突通知邮件失败: %v", err))
		}
	}

	return nil
}

// ApplyConflictResolution 应用冲突解决方案
func (s *SyncService) ApplyConflictResolution(conflict *models.DataConflict) error {
	// 获取任务信息
	var task models.SyncTask
	if err := database.DB.First(&task, conflict.TaskID).Error; err != nil {
		return err
	}

	// 获取数据库连接
	var targetDB models.DatabaseConnection
	database.DB.First(&targetDB, task.TargetDBID)

	// 根据resolution决定使用哪个数据
	var dataToApply string
	if conflict.Resolution == "source" {
		dataToApply = conflict.SourceData
	} else {
		dataToApply = conflict.TargetData
	}

	// 解析JSON数据
	var rowData map[string]interface{}
	if err := json.Unmarshal([]byte(dataToApply), &rowData); err != nil {
		return err
	}

	// 解析主键
	var primaryKey map[string]interface{}
	if err := json.Unmarshal([]byte(conflict.PrimaryKey), &primaryKey); err != nil {
		return err
	}

	// 应用数据到目标数据库
	_, err := dbconn.GetConnection(&targetDB)
	if err != nil {
		return err
	}

	// TODO: 实现具体的更新/插入逻辑
	// 这里简化处理，实际应该构建UPDATE或INSERT语句
	_ = rowData
	_ = primaryKey

	return nil
}

// 辅助函数
func (s *SyncService) getTables(db *sql.DB, dbType string) ([]string, error) {
	var query string
	switch dbType {
	case "mysql":
		query = "SHOW TABLES"
	case "postgres":
		// 使用 information_schema 更标准，兼容性更好
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY table_name"
	case "oracle":
		query = "SELECT table_name FROM user_tables"
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			continue
		}
		tables = append(tables, tableName)
	}

	return tables, nil
}

func (s *SyncService) getPrimaryKeys(db *sql.DB, dbType, tableName string) ([]string, error) {
	var query string
	switch dbType {
	case "mysql":
		// 使用 information_schema 查询主键，更标准
		query = fmt.Sprintf(`SELECT column_name FROM information_schema.key_column_usage 
			WHERE table_schema = DATABASE() AND table_name = '%s' 
			AND constraint_name = 'PRIMARY'
			ORDER BY ordinal_position`, tableName)
	case "postgres":
		// 使用 information_schema 查询主键，需要指定 schema
		query = fmt.Sprintf(`SELECT column_name FROM information_schema.key_column_usage 
			WHERE table_schema = 'public' AND table_name = '%s' 
			AND constraint_name IN (
				SELECT constraint_name FROM information_schema.table_constraints 
				WHERE table_schema = 'public' AND table_name = '%s' AND constraint_type = 'PRIMARY KEY'
			)
			ORDER BY ordinal_position`, tableName, tableName)
	case "oracle":
		query = fmt.Sprintf("SELECT column_name FROM user_cons_columns WHERE constraint_name = (SELECT constraint_name FROM user_constraints WHERE table_name = '%s' AND constraint_type = 'P')", tableName)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			continue
		}
		keys = append(keys, key)
	}

	return keys, nil
}

func (s *SyncService) tableExists(db *sql.DB, dbType, tableName string) (bool, error) {
	var query string
	switch dbType {
	case "mysql":
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?"
	case "postgres":
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = $1"
	case "oracle":
		query = "SELECT COUNT(*) FROM user_tables WHERE table_name = :1"
	default:
		return false, fmt.Errorf("unsupported database type: %s", dbType)
	}

	var count int
	err := db.QueryRow(query, tableName).Scan(&count)
	return count > 0, err
}

func (s *SyncService) quoteIdentifier(name, dbType string) string {
	switch dbType {
	case "mysql":
		return fmt.Sprintf("`%s`", name)
	case "postgres":
		return fmt.Sprintf(`"%s"`, name)
	case "oracle":
		return fmt.Sprintf(`"%s"`, name)
	default:
		return name
	}
}

func (s *SyncService) logInfo(taskID uint, message string) {
	log := models.SyncLog{
		TaskID:  taskID,
		LogType: "info",
		Message: message,
	}
	database.DB.Create(&log)
}

func (s *SyncService) logError(taskID uint, message string) {
	log := models.SyncLog{
		TaskID:  taskID,
		LogType: "error",
		Message: message,
	}
	database.DB.Create(&log)
}

// normalizeValue 规范化值，确保字符串是有效的 UTF-8
func (s *SyncService) normalizeValue(val interface{}) interface{} {
	if val == nil {
		return nil
	}

	// 处理字节数组
	if b, ok := val.([]byte); ok {
		// 检查是否是有效的 UTF-8 字符串
		if utf8.Valid(b) {
			// 检查是否已经是有效的 UUID 格式字符串
			str := string(b)
			if s.isValidUUIDString(str) {
				return str
			}
			return s.ensureUTF8(str)
		}
		// 对于二进制数据，检查是否是 UUID (16 字节)
		if len(b) == 16 {
			// 可能是 UUID 的二进制格式，转换为标准 UUID 字符串
			return s.binaryToUUID(b)
		}
		// 其他二进制数据转换为十六进制字符串
		return hex.EncodeToString(b)
	}

	// 处理字符串
	if str, ok := val.(string); ok {
		// 检查是否包含无效 UTF-8 字符（可能是二进制数据被错误转换为字符串）
		if !utf8.ValidString(str) {
			// 尝试将字符串转换回字节数组处理
			b := []byte(str)
			if len(b) == 16 {
				// 可能是 UUID 的二进制数据被错误转换为字符串
				return s.binaryToUUID(b)
			}
			// 清理无效字符
			return s.ensureUTF8(str)
		}
		// 检查是否已经是有效的 UUID 格式
		if s.isValidUUIDString(str) {
			return str
		}
		return s.ensureUTF8(str)
	}

	// 其他类型直接返回
	return val
}

// isValidUUIDString 检查字符串是否是有效的 UUID 格式
func (s *SyncService) isValidUUIDString(str string) bool {
	if len(str) != 36 {
		return false
	}
	// UUID 格式: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	// 检查格式：8-4-4-4-12
	if str[8] != '-' || str[13] != '-' || str[18] != '-' || str[23] != '-' {
		return false
	}
	// 检查是否都是十六进制字符
	for i, r := range str {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
		} else {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

// binaryToUUID 将 16 字节的二进制数据转换为 UUID 字符串格式
// MySQL 的 UUID_TO_BIN 默认使用交换字节序（swap_flag=1），需要反转
func (s *SyncService) binaryToUUID(b []byte) string {
	if len(b) != 16 {
		// 如果不是 16 字节，返回十六进制字符串
		return hex.EncodeToString(b)
	}
	
	// UUID 标准格式: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	// 格式：time_low(4) time_mid(2) time_hi(2) clock_seq_hi(1) clock_seq_low(1) node(6)
	// 直接按大端序读取字节
	timeLow := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	timeMid := uint16(b[4])<<8 | uint16(b[5])
	timeHi := uint16(b[6])<<8 | uint16(b[7])
	clockSeqHi := b[8]
	clockSeqLow := b[9]
	node := fmt.Sprintf("%02x%02x%02x%02x%02x%02x", b[10], b[11], b[12], b[13], b[14], b[15])
	
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%s",
		timeLow, timeMid, timeHi, clockSeqHi, clockSeqLow, node)
}

// ensureUTF8 确保字符串是有效的 UTF-8，清理无效字节
func (s *SyncService) ensureUTF8(str string) string {
	if utf8.ValidString(str) {
		return str
	}

	// 如果包含无效的 UTF-8 字节，清理它们
	// 遍历字符串，只保留有效的 UTF-8 字符
	v := make([]rune, 0, len(str))
	for i := 0; i < len(str); {
		r, size := utf8.DecodeRuneInString(str[i:])
		if r == utf8.RuneError && size == 1 {
			// 无效字节，用替换字符代替（U+FFFD）
			v = append(v, '\uFFFD')
			i++
		} else {
			// 有效的 rune，保留
			v = append(v, r)
			i += size
		}
	}
	return string(v)
}

// sanitizeValueForPostgres 清理值，确保字符串是有效的 UTF-8（用于 PostgreSQL）
func (s *SyncService) sanitizeValueForPostgres(val interface{}) interface{} {
	if val == nil {
		return nil
	}
	// 如果已经是规范化后的值（如 UUID 字符串），直接返回
	if str, ok := val.(string); ok {
		// 如果是有效的 UUID 格式，直接返回（不需要进一步处理）
		if s.isValidUUIDString(str) {
			return str
		}
		// 否则确保是有效的 UTF-8
		return s.ensureUTF8(str)
	}
	// 对于其他类型，使用 normalizeValue 处理（可能包含二进制数据）
	return s.normalizeValue(val)
}
