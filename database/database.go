package database

import (
	"fmt"
	"zh.xyz/dv/sync/config"
	"zh.xyz/dv/sync/models"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"zh.xyz/dv/sync/utils"
)

var DB *gorm.DB

// InitDatabase 初始化数据库连接
func InitDatabase() error {
	var err error
	cfg := config.GlobalConfig.Database

	var dsn string
	var dialector gorm.Dialector

	switch cfg.Type {
	case "mysql":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DBName)
		dialector = mysql.Open(dsn)
	case "postgres":
		dsn = fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
			cfg.Host, cfg.User, cfg.Password, cfg.DBName, cfg.Port)
		dialector = postgres.Open(dsn)
	default:
		return fmt.Errorf("unsupported database type: %s", cfg.Type)
	}

	DB, err = gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})

	if err != nil {
		return err
	}

	// 自动迁移
	err = DB.AutoMigrate(
		&models.User{},
		&models.DatabaseConnection{},
		&models.SyncTask{},
		&models.DataConflict{},
		&models.SyncLog{},
		&models.DatabaseObject{},
		&models.ObjectSyncLog{},
	)

	if err != nil {
		return err
	}

	// 创建默认管理员账户（如果不存在）
	createDefaultAdmin()

	return nil
}

func createDefaultAdmin() {
	var admin models.User
	result := DB.Where("username = ?", "admin").First(&admin)
	if result.Error != nil {
		// 默认密码：admin123（实际应用中应该使用更强的密码）
		hashedPassword, _ := utils.HashPassword("admin123")
		admin = models.User{
			Username: "admin",
			Password: hashedPassword,
			Email:    "admin@example.com",
			Role:     "admin",
			Status:   "active",
		}
		DB.Create(&admin)
	}
}
