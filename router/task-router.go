package router

import (
	"strings"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

// SetTaskRouter 统一注册内部异步和官方插件任务，公共查询路径只能注册一次。
// 同一层通配参数统一为 key；提交时表示插件键，读取时表示任务编号。
func SetTaskRouter(router *gin.Engine) {
	taskSubmitRouter := router.Group("/v1/tasks")
	taskSubmitRouter.Use(middleware.RouteTag("relay"), middleware.TokenAuth())
	{
		taskSubmitRouter.POST("/:key", middleware.PrepareTaskPluginSubmit(), middleware.Distribute(), controller.RelayTask)
	}

	taskReadRouter := router.Group("/v1/tasks")
	taskReadRouter.Use(middleware.RouteTag("relay"))
	readOnlyAuth := middleware.TokenAuthReadOnly()
	tokenAuth := middleware.TokenAuth()
	{
		// 内部任务保留额度耗尽后的只读查询；官方任务仍使用原有令牌认证。
		taskReadRouter.GET("/:key", func(c *gin.Context) {
			if strings.HasPrefix(c.Param("key"), "async_") {
				readOnlyAuth(c)
				return
			}
			tokenAuth(c)
		}, controller.GetTask)
		taskReadRouter.GET("/:key/media/:index", readOnlyAuth, controller.GetAsyncRelayMedia)
		taskReadRouter.GET("/:key/artifacts", tokenAuth, controller.GetTaskArtifacts)
	}

	taskContentRouter := router.Group("/v1/tasks")
	taskContentRouter.Use(
		middleware.RouteTag("relay"),
		middleware.TokenOrTaskArtifactAccessAuth("key", "artifact_key"),
	)
	{
		taskContentRouter.GET("/:key/artifacts/:artifact_key/content", controller.TaskArtifactContent)
		taskContentRouter.HEAD("/:key/artifacts/:artifact_key/content", controller.TaskArtifactContent)
	}
}
