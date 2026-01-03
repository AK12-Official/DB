package models

import (
	"time"
)

// DatabaseObject 数据库对象记录
type DatabaseObject struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	ConnectionID uint     `gorm:"not null;index" json:"connection_id"`
	Connection  DatabaseConnection `gorm:"foreignKey:ConnectionID" json:"connection,omitempty"`
	ObjectType  string    `gorm:"type:varchar(50);not null" json:"object_type"` // procedure, trigger, view, function
	ObjectName  string    `gorm:"type:varchar(255);not null;index" json:"object_name"`
	Definition  string    `gorm:"type:text" json:"definition"` // 对象定义SQL
	TableName   string    `gorm:"type:varchar(255)" json:"table_name"`                  // 触发器关联的表名
	SchemaName  string    `gorm:"type:varchar(255)" json:"schema_name"`                 // 模式名（PostgreSQL/Oracle）
	Status      string    `gorm:"type:varchar(50);default:active" json:"status"` // active, inactive
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ObjectSyncLog 对象同步日志
type ObjectSyncLog struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TaskID      uint      `gorm:"not null;index" json:"task_id"`
	Task        SyncTask  `gorm:"foreignKey:TaskID" json:"task,omitempty"`
	ObjectType  string    `gorm:"type:varchar(50);not null" json:"object_type"`
	ObjectName  string    `gorm:"type:varchar(255);not null" json:"object_name"`
	Action      string    `gorm:"type:varchar(50);not null" json:"action"`     // create, update, delete, skip
	Status      string    `gorm:"type:varchar(50);not null" json:"status"`     // success, failed
	Message     string    `gorm:"type:text" json:"message"`
	CreatedAt   time.Time `json:"created_at"`
}

