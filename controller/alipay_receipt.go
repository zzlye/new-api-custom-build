package controller

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func GetAlipayReceiptSettings(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	cfg, err := model.GetAlipayReceiptConfig()
	if err != nil {
		common.ApiError(c, errors.New("读取收款码设置失败"))
		return
	}
	configured := cfg.PrivateKey != ""
	cfg.PrivateKey = ""
	var scan model.ReceiptGate
	model.DB.Where("id = ?", 1).Find(&scan)
	common.ApiSuccess(c, gin.H{"config": cfg, "private_key_configured": configured, "last_scan_at": scan.LastScanAt, "last_error": scan.LastError})
}

func SaveAlipayReceiptSettings(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	var cfg model.AlipayReceiptConfig
	if err := common.DecodeJson(c.Request.Body, &cfg); err != nil {
		common.ApiError(c, errors.New("收款码配置格式无效"))
		return
	}
	if cfg.Enabled && !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiError(c, errors.New("请先完成支付设置确认"))
		return
	}
	if err := model.SaveAlipayReceiptConfig(cfg); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func RequestAlipayReceipt(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	var req AmountRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.Amount < getMinTopup() || req.Amount > getMaxTopUpAmount() {
		common.ApiError(c, errors.New("充值数量无效"))
		return
	}
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiError(c, errors.New("当前未开放充值"))
		return
	}
	userID := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, userID, req.Amount) {
		return
	}
	group, err := model.GetUserGroup(userID, true)
	if err != nil {
		common.ApiError(c, errors.New("读取用户分组失败"))
		return
	}
	price := getPayMoney(req.Amount, group)
	if math.IsNaN(price) || math.IsInf(price, 0) || price < 0.01 || price > 100000 {
		common.ApiError(c, errors.New("充值金额无效或过大"))
		return
	}
	quota, err := getTopUpQuota(req.Amount)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	order, err := model.CreateReceiptOrder(userID, req.Amount, decimal.NewFromFloat(price).Round(2).Mul(decimal.NewFromInt(100)).IntPart(), quota, c.ClientIP(), time.Now())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	order.Receipt.QRCode = ""
	common.ApiSuccess(c, order)
}

func GetAlipayReceiptOrder(c *gin.Context) {
	if c.GetInt("id") <= 0 {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	order, err := model.GetReceiptOrder(c.Param("trade_no"), c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, order)
}

func ConfirmAlipayReceiptOrder(c *gin.Context) {
	if c.GetInt("id") <= 0 {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	now := time.Now()
	if err := model.ConfirmReceiptOrder(c.Param("trade_no"), c.GetInt("id"), now); err != nil {
		common.ApiError(c, err)
		return
	}
	// 避免支付宝秒级账务将确认之前的收款误判为本单。
	timer := time.NewTimer(time.Until(time.Unix(now.Unix()+1, 0)))
	defer timer.Stop()
	select {
	case <-c.Request.Context().Done():
		return
	case <-timer.C:
	}
	GetAlipayReceiptOrder(c)
}

func CloseAlipayReceiptOrder(c *gin.Context) {
	if c.GetInt("id") <= 0 {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	if err := model.ClosePaymentOrder(c.Param("trade_no"), c.GetInt("id"), c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	GetAlipayReceiptOrder(c)
}

func GetOrderLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	page = max(1, min(page, 10000))
	keyword := strings.TrimSpace(c.Query("keyword"))
	if len(keyword) > 128 {
		common.ApiError(c, errors.New("搜索内容过长"))
		return
	}
	rows, count, err := model.ListOrderLogs(keyword, c.Query("status"), c.Query("provider"), (page-1)*20)
	if err != nil {
		common.ApiError(c, errors.New("读取订单日志失败"))
		return
	}
	common.ApiSuccess(c, gin.H{"items": rows, "total": count})
}

func GetOrderLogDetail(c *gin.Context) {
	detail, err := model.GetOrderLogDetail(c.Param("trade_no"))
	if err != nil {
		common.ApiError(c, errors.New("订单日志不存在"))
		return
	}
	common.ApiSuccess(c, detail)
}

func RetryOrderCallback(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	order, err := model.GetReceiptOrder(c.Param("trade_no"), 0)
	if err != nil {
		common.ApiError(c, errors.New("此通道请在原支付平台核对并重发回调"))
		return
	}
	if order.Status == common.TopUpStatusSuccess {
		common.ApiSuccess(c, nil)
		return
	}
	if order.Status != common.TopUpStatusPending {
		common.ApiError(c, errors.New("订单已结束"))
		return
	}
	// 只核验账务并重试已有收款，不接受客户端提交成功标记或支付金额。
	if err := model.RequestReceiptRecheck(order.Id, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	// 已核验的收入只需重试入账，不让后续支付宝网络故障阻断原单结算。
	if order.PaidTime == 0 {
		scanErr := service.ScanAlipayReceipts(c.Request.Context())
		current, readErr := model.GetReceiptOrder(order.TradeNo, 0)
		if readErr == nil && current.Status == common.TopUpStatusSuccess {
			common.ApiSuccess(c, nil)
			return
		}
		if scanErr != nil {
			common.ApiError(c, scanErr)
			return
		}
	}
	if err := model.SettleReceiptOrder(order.Id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func CloseOrderLog(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	if err := model.ClosePaymentOrder(c.Param("trade_no"), 0, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
