package routes

import (
	"zh.xyz/dv/sync/api"
	"zh.xyz/dv/sync/handlers"
	"zh.xyz/dv/sync/middleware"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(r *gin.Engine) {
	// CORS中间件
	r.Use(api.CORSMiddleware())

	// 健康检查端点（无需认证）
	r.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
			"service": "db-sync",
		})
	})

	// 公共路由
	public := r.Group("/api/v1")
	{
		userHandler := &handlers.UserHandler{}
		public.POST("/register", userHandler.Register)
		public.POST("/login", userHandler.Login)
	}

	// 需要认证的路由
	auth := r.Group("/api/v1")
	auth.Use(middleware.AuthMiddleware())
	{
		userHandler := &handlers.UserHandler{}
		auth.GET("/profile", userHandler.GetProfile)

		// 数据库连接管理
		dbHandler := &handlers.DBConnectionHandler{}
		auth.POST("/connections", dbHandler.CreateConnection)
		auth.GET("/connections", dbHandler.ListConnections)
		auth.POST("/connections/test", dbHandler.TestConnection)
		
		// 数据库连接子资源路由（必须在/:id路由之前，避免路由冲突）
		connections := auth.Group("/connections")
		{
			queryHandler := &handlers.QueryHandler{}
			connections.GET("/:id/tables", queryHandler.GetTables)
			
			objectHandler := &handlers.DBObjectHandler{}
			connections.GET("/:id/objects", objectHandler.ListObjects)
			connections.GET("/:id/objects/:type/definition", objectHandler.GetObjectDefinition)
			
			// 基础CRUD路由（必须在子资源路由之后）
			connections.GET("/:id", dbHandler.GetConnection)
			connections.PUT("/:id", dbHandler.UpdateConnection)
			connections.DELETE("/:id", dbHandler.DeleteConnection)
		}

		// 数据查询
		queryHandler := &handlers.QueryHandler{}
		auth.POST("/query", queryHandler.QueryData)
		auth.POST("/query/sql", queryHandler.ExecuteSQL)

		// 同步任务
		syncHandler := &handlers.SyncHandler{}
		objectHandler := &handlers.DBObjectHandler{}
		auth.POST("/sync/tasks", syncHandler.CreateSyncTask)
		auth.GET("/sync/tasks", syncHandler.ListSyncTasks)
		
		// 同步任务子资源路由
		tasks := auth.Group("/sync/tasks")
		{
			tasks.GET("/:id/logs", syncHandler.GetSyncLogs)
			tasks.GET("/:id/object-logs", objectHandler.GetObjectSyncLogs)
			// 基础路由
			tasks.GET("/:id", syncHandler.GetSyncTask)
			tasks.POST("/:id/start", syncHandler.StartSyncTask)
			tasks.POST("/:id/stop", syncHandler.StopSyncTask)
			tasks.POST("/:id/execute", syncHandler.ExecuteSyncTask)
			tasks.DELETE("/:id", syncHandler.DeleteSyncTask)
		}

		// 冲突处理
		conflictHandler := &handlers.ConflictHandler{}
		auth.GET("/conflicts", conflictHandler.ListConflicts)
		auth.GET("/conflicts/:id", conflictHandler.GetConflict)
		auth.POST("/conflicts/:id/resolve", conflictHandler.ResolveConflict)
	}

	// 管理员路由
	admin := r.Group("/api/v1/admin")
	admin.Use(middleware.AuthMiddleware())
	admin.Use(middleware.AdminMiddleware())
	{
		// 管理员相关接口可以在这里添加
	}

	// 公共冲突查看接口（通过token）
	r.GET("/api/v1/conflicts/view", func(c *gin.Context) {
		handler := &handlers.ConflictHandler{}
		handler.ViewConflictByToken(c)
	})
}
