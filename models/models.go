package models

import (
	"time"
)

// User 用户模型
type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"type:varchar(100);uniqueIndex;not null" json:"username"`
	Password  string    `gorm:"type:varchar(255);not null" json:"-"` // 不返回给前端
	Email     string    `gorm:"type:varchar(255);not null" json:"email"`
	Role      string    `gorm:"type:varchar(50);default:user" json:"role"` // admin, user
	Status    string    `gorm:"type:varchar(50);default:active" json:"status"` // active, inactive
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DatabaseConnection 数据库连接配置
type DatabaseConnection struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"not null" json:"name"` // 连接名称
	Type        string    `gorm:"not null" json:"type"` // mysql, oracle, postgres
	Host        string    `gorm:"not null" json:"host"`
	Port        string    `gorm:"not null" json:"port"`
	Username    string    `gorm:"not null" json:"username"`
	Password    string    `gorm:"not null" json:"-"` // 加密存储
	Database    string    `gorm:"not null" json:"database"`
	Description string    `json:"description"`
	Status      string    `gorm:"default:active" json:"status"` // active, inactive
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SyncTask 同步任务
type SyncTask struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"type:varchar(255);not null" json:"name"`
	SourceDBID  uint      `gorm:"not null" json:"source_db_id"`  // 源数据库ID
	TargetDBID  uint      `gorm:"not null" json:"target_db_id"`  // 目标数据库ID
	SourceDB    DatabaseConnection `gorm:"foreignKey:SourceDBID" json:"source_db,omitempty"`
	TargetDB    DatabaseConnection `gorm:"foreignKey:TargetDBID" json:"target_db,omitempty"`
	TableName   string    `gorm:"type:varchar(255);not null" json:"table_name"`    // 表名，空字符串表示整库同步
	SyncType    string    `gorm:"type:varchar(50);not null" json:"sync_type"`     // realtime, scheduled
	CronExpr    string    `gorm:"type:varchar(100)" json:"cron_expr"`                     // 定时任务的cron表达式
	Status      string    `gorm:"type:varchar(50);default:stopped" json:"status"` // running, stopped, error
	LastSyncAt  *time.Time `json:"last_sync_at"`
	CreatedBy   uint      `gorm:"not null" json:"created_by"`
	Creator     User      `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// DataConflict 数据冲突记录
type DataConflict struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TaskID      uint      `gorm:"not null;index" json:"task_id"`
	Task        SyncTask  `gorm:"foreignKey:TaskID" json:"task,omitempty"`
	TableName   string    `gorm:"type:varchar(255);not null" json:"table_name"`
	PrimaryKey  string    `gorm:"type:varchar(500);not null;index" json:"primary_key"` // 主键值（JSON格式）
	SourceData  string    `gorm:"type:text" json:"source_data"`      // 源数据库数据（JSON格式）
	TargetData  string    `gorm:"type:text" json:"target_data"`      // 目标数据库数据（JSON格式）
	ConflictType string   `gorm:"type:varchar(50);not null" json:"conflict_type"`     // update_conflict, delete_conflict
	Status      string    `gorm:"type:varchar(50);default:pending" json:"status"`     // pending, resolved
	ResolvedBy  *uint     `json:"resolved_by,omitempty"`
	Resolver    *User     `gorm:"foreignKey:ResolvedBy" json:"resolver,omitempty"`
	Resolution  string    `gorm:"type:varchar(50)" json:"resolution"`       // resolved: source 或 target
	ResolvedAt  *time.Time `json:"resolved_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SyncLog 同步日志
type SyncLog struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TaskID      uint      `gorm:"not null;index" json:"task_id"`
	Task        SyncTask  `gorm:"foreignKey:TaskID" json:"task,omitempty"`
	LogType     string    `gorm:"type:varchar(50);not null" json:"log_type"` // info, warning, error
	Message     string    `gorm:"type:text" json:"message"`
	Details     string    `gorm:"type:text" json:"details"` // JSON格式的详细信息
	CreatedAt   time.Time `json:"created_at"`
}
