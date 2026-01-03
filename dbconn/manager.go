package dbconn

import (
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"zh.xyz/dv/sync/models"
)

var connectionPool = sync.Map{}

// GetConnection 获取数据库连接
func GetConnection(dbConn *models.DatabaseConnection) (*gorm.DB, error) {
	key := fmt.Sprintf("%d", dbConn.ID)
	
	// 从连接池获取
	if conn, ok := connectionPool.Load(key); ok {
		return conn.(*gorm.DB), nil
	}

	// 创建新连接
	var dsn string
	var dialector gorm.Dialector

	switch dbConn.Type {
	case "mysql":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			dbConn.Username, dbConn.Password, dbConn.Host, dbConn.Port, dbConn.Database)
		dialector = mysql.Open(dsn)
	case "oracle":
		// Oracle暂时不支持GORM，使用原生连接
		// 注意：Oracle需要通过GetRawConnection使用原生连接
		return nil, fmt.Errorf("Oracle数据库暂不支持GORM，请使用原生连接")
	case "postgres":
		dsn = fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
			dbConn.Host, dbConn.Username, dbConn.Password, dbConn.Database, dbConn.Port)
		dialector = postgres.Open(dsn)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbConn.Type)
	}

	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// 存储到连接池
	connectionPool.Store(key, db)
	return db, nil
}

// GetRawConnection 获取原生数据库连接（用于复杂查询）
func GetRawConnection(dbConn *models.DatabaseConnection) (*sql.DB, error) {
	var dsn string
	var driverName string

	switch dbConn.Type {
	case "mysql":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			dbConn.Username, dbConn.Password, dbConn.Host, dbConn.Port, dbConn.Database)
		driverName = "mysql"
	case "oracle":
		// Oracle原生连接需要使用godror或oci8驱动
		// 这里提供一个示例格式，实际需要安装对应的驱动
		// 格式: user/password@host:port/service_name
		dsn = fmt.Sprintf("%s/%s@%s:%s/%s",
			dbConn.Username, dbConn.Password, dbConn.Host, dbConn.Port, dbConn.Database)
		// 注意：需要导入对应的驱动，例如 _ "github.com/godror/godror"
		return nil, fmt.Errorf("Oracle驱动未配置，请安装godror或oci8驱动")
	case "postgres":
		dsn = fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
			dbConn.Host, dbConn.Username, dbConn.Password, dbConn.Database, dbConn.Port)
		driverName = "postgres"
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbConn.Type)
	}

	return sql.Open(driverName, dsn)
}

// CloseConnection 关闭数据库连接
func CloseConnection(dbConnID uint) {
	key := fmt.Sprintf("%d", dbConnID)
	if conn, ok := connectionPool.LoadAndDelete(key); ok {
		if db, ok := conn.(*gorm.DB); ok {
			if sqlDB, err := db.DB(); err == nil {
				sqlDB.Close()
			}
		}
	}
}
