package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// 用户接口始终从登录态取用户编号，管理接口另外要求根用户权限。
func lotteryRoot(c *gin.Context) bool {
	if c.GetInt("role") != common.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "仅根用户可管理抽奖活动"})
		return false
	}
	return true
}

func lotteryID(c *gin.Context) (int, error) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		return 0, errors.New("活动编号无效")
	}
	return id, nil
}

func GetLotteryActivities(c *gin.Context) { listLotteryActivities(c, false) }
func GetAdminLotteryActivities(c *gin.Context) {
	if lotteryRoot(c) {
		listLotteryActivities(c, true)
	}
}

func listLotteryActivities(c *gin.Context, root bool) {
	page := common.GetPageQuery(c)
	items, total, err := model.ListLotteryActivities(c.GetInt("id"), page.GetStartIdx(), page.GetPageSize(), root)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	paid := 0
	if !root {
		paid, err = model.LotteryPaidTopUpQuota(model.DB, c.GetInt("id"))
		if err != nil {
			common.ApiError(c, err)
			return
		}
		// 用户接口只返回参与所需字段，不能仅靠页面隐藏奖池和剩余份数。
		visible := make([]gin.H, 0, len(items))
		for _, item := range items {
			visible = append(visible, gin.H{
				"id":                   item.ID,
				"name":                 item.Name,
				"status":               item.Status,
				"min_topup_quota":      item.MinTopUpQuota,
				"topup_scope":          item.TopUpScope,
				"topup_start":          item.TopUpStart,
				"topup_end":            item.TopUpEnd,
				"per_user_limit":       item.PerUserLimit,
				"used_draws":           item.UsedDraws,
				"eligible_topup_quota": item.EligibleTopUpQuota,
				"created_at":           item.CreatedAt,
				"started_at":           item.StartedAt,
				"ended_at":             item.EndedAt,
			})
		}
		common.ApiSuccess(c, gin.H{"items": visible, "total": total, "paid_topup_quota": paid})
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "paid_topup_quota": paid})
}

func SaveLotteryActivity(c *gin.Context) {
	if !lotteryRoot(c) {
		return
	}
	id := 0
	if c.Param("id") != "" {
		var err error
		id, err = lotteryID(c)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	var rules model.LotteryRules
	if err := c.ShouldBindJSON(&rules); err != nil {
		common.ApiErrorMsg(c, "抽奖设置格式无效")
		return
	}
	a, err := model.SaveLotteryActivity(id, c.GetInt("id"), rules)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, a)
}

func ChangeLotteryState(c *gin.Context) {
	if !lotteryRoot(c) {
		return
	}
	id, err := lotteryID(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var request struct {
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, "活动操作无效")
		return
	}
	a, err := model.ChangeLotteryState(id, request.Action)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, a)
}

func DrawLottery(c *gin.Context) {
	id, err := lotteryID(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var request struct {
		RequestID string `json:"request_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, "抽奖请求无效")
		return
	}
	draw, err := model.DrawLottery(id, c.GetInt("id"), request.RequestID, c.ClientIP())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, draw)
}

func GetLotteryDraws(c *gin.Context) { listLotteryDraws(c, false) }

func DeleteLotteryActivity(c *gin.Context) {
	if !lotteryRoot(c) {
		return
	}
	id, err := lotteryID(c)
	if err == nil {
		err = model.DeleteLotteryActivity(id)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func GetLotteryWinners(c *gin.Context) {
	id, err := lotteryID(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items, err := model.ListLotteryWinners(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

func GetAdminLotteryDraws(c *gin.Context) {
	if lotteryRoot(c) {
		listLotteryDraws(c, true)
	}
}

func listLotteryDraws(c *gin.Context, root bool) {
	id, err := lotteryID(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page := common.GetPageQuery(c)
	items, total, err := model.ListLotteryDraws(id, c.GetInt("id"), page.GetStartIdx(), page.GetPageSize(), root)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total})
}
