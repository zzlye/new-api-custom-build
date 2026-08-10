package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAppearanceMediaCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(appearanceMediaCache())
	engine.GET("/uploads/appearance/background.mp4", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	engine.GET("/api/status", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	t.Run("外观媒体使用长期缓存", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/uploads/appearance/background.mp4", nil)

		engine.ServeHTTP(response, request)

		require.Equal(t, "public, max-age=31536000, immutable", response.Header().Get("Cache-Control"))
	})

	t.Run("接口响应不受影响", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/status", nil)

		engine.ServeHTTP(response, request)

		require.Empty(t, response.Header().Get("Cache-Control"))
	})
}
