package router

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetRouter(router *gin.Engine, assets WebAssets) {
	// 外观上传文件对外只读访问（图片/短视频）
	if err := os.MkdirAll("data/appearance", 0o755); err == nil {
		router.Use(appearanceMediaCache())
		router.Static("/uploads/appearance", "data/appearance")
	}
	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	SetVideoRouter(router)
	frontendBaseUrl := os.Getenv("FRONTEND_BASE_URL")
	if common.IsMasterNode && frontendBaseUrl != "" {
		frontendBaseUrl = ""
		common.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	if frontendBaseUrl == "" {
		SetWebRouter(router, assets)
	} else {
		frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
		router.NoRoute(func(c *gin.Context) {
			c.Set(middleware.RouteTagKey, "web")
			c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseUrl, c.Request.RequestURI))
		})
	}
}

// appearanceMediaCache 利用不可变文件名长期缓存外观媒体。
func appearanceMediaCache() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/uploads/appearance/") {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		}
		c.Next()
	}
}
