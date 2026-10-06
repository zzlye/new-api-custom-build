package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetConfiguredPricingUsers(c *gin.Context) {
	// 已定价名单和编辑接口使用相同的根用户权限。
	if c.GetInt("role") != common.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "仅根用户可以管理用户定价"})
		return
	}
	page := common.GetPageQuery(c)
	users, total, err := model.ListConfiguredPricingUsers(c.Query("keyword"), page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(total)
	page.SetItems(users)
	common.ApiSuccess(c, page)
}

func GetUserModelPricingConfig(c *gin.Context) {
	// 接口只对根用户开放，避免用户通过管理接口读取别人的协议价格。
	if c.GetInt("role") != common.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "仅根用户可以管理用户定价"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "用户编号无效"})
		return
	}
	snapshot, err := model.GetUserModelPricingSnapshot(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, snapshot)
}

func UpdateUserModelPricingConfig(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "仅根用户可以管理用户定价"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "用户编号无效"})
		return
	}
	var request struct {
		Changes []model.ModelPricingChange `json:"changes"`
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20), &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "用户定价参数无效"})
		return
	}
	if err := model.UpdateUserModelPricing(id, request.Changes); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, model.ErrModelPricingConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	names := make([]string, 0, len(request.Changes))
	for _, change := range request.Changes {
		names = append(names, change.ModelName)
	}
	recordManageAudit(c, "user.model_pricing.update", map[string]any{"user_id": id, "models": names})
	common.ApiSuccess(c, gin.H{"updated_models": names})
}
