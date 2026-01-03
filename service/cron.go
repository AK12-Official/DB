package service

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"zh.xyz/dv/sync/database"
	"zh.xyz/dv/sync/models"
)

var cronManager *cron.Cron

// InitCronManager 初始化定时任务管理器
func InitCronManager() {
	cronManager = cron.New(cron.WithSeconds())
	cronManager.Start()
}

// StartScheduledSync 启动定时同步任务
func StartScheduledSync(taskID uint) error {
	var task models.SyncTask
	if err := database.DB.First(&task, taskID).Error; err != nil {
		return fmt.Errorf("任务不存在: %v", err)
	}

	if task.SyncType != "scheduled" {
		return fmt.Errorf("任务不是定时任务类型")
	}

	if task.CronExpr == "" {
		return fmt.Errorf("定时任务缺少cron表达式")
	}

	// 添加cron任务
	_, err := cronManager.AddFunc(task.CronExpr, func() {
		syncService := &SyncService{}
		if err := syncService.SyncTable(&task); err != nil {
			fmt.Printf("定时同步任务 %d 执行失败: %v\n", taskID, err)
			task.Status = "error"
		} else {
			now := time.Now()
			task.LastSyncAt = &now
		}
		database.DB.Save(&task)
	})

	if err != nil {
		return fmt.Errorf("添加定时任务失败: %v", err)
	}

	task.Status = "running"
	database.DB.Save(&task)

	return nil
}

// StopScheduledSync 停止定时同步任务
func StopScheduledSync(taskID uint) error {
	var task models.SyncTask
	if err := database.DB.First(&task, taskID).Error; err != nil {
		return fmt.Errorf("任务不存在: %v", err)
	}

	task.Status = "stopped"
	return database.DB.Save(&task).Error
}
