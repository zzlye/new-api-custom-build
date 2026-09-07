package controller

import (
	"crypto/hmac"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// asyncRelayMediaPreviewURL 签名只授权这一份文件，不包含账户令牌；到期时间与文件保存设置一致。
func asyncRelayMediaPreviewURL(task *model.AsyncRelayTask, kind string, index int) string {
	expires := common.GetTimestamp() + common.AsyncMediaRetentionSeconds()
	if task.FinishedAt > 0 {
		expires = task.FinishedAt + common.AsyncMediaRetentionSeconds()
	}
	path := "/task-media/" + task.TaskID + "/" + kind + "/" + strconv.Itoa(index)
	deadline := strconv.FormatInt(expires, 10)
	signature := common.GenerateHMACWithKey([]byte(common.SessionSecret), "async-media-v1\n"+path+"\n"+deadline)
	query := url.Values{"expires": {deadline}, "signature": {signature}}
	return path + "?" + query.Encode()
}

// GetAsyncRelayMediaDirect 返回原始图片或视频，不进入控制台页面；签名同时绑定任务、文件种类和序号。
func GetAsyncRelayMediaDirect(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	// 文件即使包含主动内容也不运行脚本，避免预览页共享控制台的登录环境。
	c.Header("Content-Security-Policy", "sandbox; script-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	query := c.Request.URL.Query()
	deadline := query.Get("expires")
	expires, err := strconv.ParseInt(deadline, 10, 64)
	signature, signatureErr := hex.DecodeString(query.Get("signature"))
	expected, _ := hex.DecodeString(common.GenerateHMACWithKey([]byte(common.SessionSecret), "async-media-v1\n"+c.Request.URL.Path+"\n"+deadline))
	if err != nil || expires <= 0 || signatureErr != nil || len(query) != 2 || len(query["expires"]) != 1 || len(query["signature"]) != 1 || !hmac.Equal(signature, expected) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "文件地址无效，请从任务详情重新打开"}})
		return
	}
	if expires <= common.GetTimestamp() {
		c.JSON(http.StatusGone, gin.H{"error": gin.H{"message": "文件地址已到期，任务日志仍保留"}})
		return
	}
	task, err := model.GetAsyncRelayTaskByTaskID(c.Param("task_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "读取任务失败"}})
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "生成文件不存在"}})
		return
	}
	// 保存期限调短或任务已删除时，旧签名也不会延长文件的可访问时间。
	c.Header("Content-Disposition", "inline")
	if c.Param("kind") == "reference" {
		c.Set("id", task.UserID)
		c.Set("role", common.RoleCommonUser)
		GetAsyncRelayReference(c)
		return
	}
	if c.Param("kind") != "media" {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "文件类型不存在"}})
		return
	}
	serveAsyncRelayMedia(c, task, c.Param("index"))
}
