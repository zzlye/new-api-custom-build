package controller

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUploadAppearanceMediaRemovesFileWhenSettingsUpdateFails(t *testing.T) {
	previousDB := model.DB
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
	})

	// 将上传目录限制在测试临时目录，验证失败请求不会留下媒体文件。
	previousDirectory, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(t.TempDir()))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(previousDirectory))
	})

	requestBody := bytes.NewBuffer(nil)
	writer := multipart.NewWriter(requestBody)
	require.NoError(t, writer.WriteField("target", "global"))
	filePart, err := writer.CreateFormFile("file", "background.png")
	require.NoError(t, err)
	_, err = filePart.Write([]byte("appearance-test-image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/api/appearance/upload", requestBody)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request

	UploadAppearanceMedia(context)

	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	require.False(t, payload.Success)
	require.Equal(t, "更新外观设置失败", payload.Message)

	files, err := filepath.Glob(filepath.Join("data", "appearance", "*"))
	require.NoError(t, err)
	require.Empty(t, files)
}
