package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"zh.xyz/dv/sync/config"
	"zh.xyz/dv/sync/database"
	"zh.xyz/dv/sync/routes"
	"zh.xyz/dv/sync/service"
)

func main() {
	// 加载配置
	if err := config.LoadConfig("config.json"); err != nil {
		log.Printf("加载配置文件失败，使用默认配置: %v", err)
	}

	// 初始化数据库
	if err := database.InitDatabase(); err != nil {
		log.Fatal("数据库初始化失败:", err)
	}

	// 初始化定时任务管理器
	service.InitCronManager()

	// 设置Gin模式
	if config.GlobalConfig.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建路由
	r := gin.Default()

	// 设置路由
	routes.SetupRoutes(r)

	// 启动服务器
	port := config.GlobalConfig.Server.Port
	if port == "" {
		port = "8080"
	}

	log.Printf("服务器启动在端口 %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal("服务器启动失败:", err)
	}
}
