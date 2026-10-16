package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var receiptWorkerOnce sync.Once

func StartAlipayReceiptWorker() {
	receiptWorkerOnce.Do(func() {
		go func() {
			// 页面关闭不影响查账；数据库租约限制多个实例的实际查询频率。
			for range time.NewTicker(5 * time.Second).C {
				if err := ScanAlipayReceipts(context.Background()); err != nil {
					common.SysError("支付宝收款码查账失败：" + err.Error())
				}
			}
		}()
	})
}

func ScanAlipayReceipts(parent context.Context) (scanErr error) {
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	token := common.GetUUID()
	ok, err := model.AcquireReceiptScan(time.Now().Unix(), token)
	if err != nil || !ok {
		return err
	}
	defer func() {
		message := ""
		if scanErr != nil {
			message = scanErr.Error()
		}
		_ = model.FinishReceiptScan(token, message, time.Now().Unix())
	}()
	cfg, err := model.GetAlipayReceiptConfig()
	if err != nil {
		return errors.New("读取收款码配置失败")
	}
	var pending []model.TopUp
	if err := model.DB.WithContext(ctx).Where("payment_provider = ? AND status = ?", model.AlipayReceiptProvider, common.TopUpStatusPending).Order("id").Limit(1000).Find(&pending).Error; err != nil {
		return errors.New("读取待支付订单失败")
	}
	if len(pending) == 0 {
		return nil
	}
	// 已核验的收款入账失败时，只重试原订单；不把该收入匹配给其他订单。
	var linked []int
	if err := model.DB.WithContext(ctx).Table("receipt_payments").Joins("JOIN top_ups ON top_ups.id = receipt_payments.top_up_id").Where("top_ups.status = ?", common.TopUpStatusPending).Limit(100).Pluck("top_up_id", &linked).Error; err != nil {
		return errors.New("读取待入账流水失败")
	}
	for _, id := range linked {
		if err := model.SettleReceiptOrder(id); err != nil {
			scanErr = errors.New("部分已收款订单入账失败，请检查订单日志")
		}
	}
	client, err := cfg.Client()
	if err != nil {
		return err
	}
	start := time.Unix(pending[0].CreateTime, 0).Add(-time.Second)
	end := time.Now()
	if end.Sub(start) > 24*time.Hour {
		end = start.Add(24 * time.Hour)
	}
	completed := false
	for pageNo := 1; pageNo <= 10; pageNo++ {
		page, err := client.Query(ctx, start, end, pageNo, 100)
		if err != nil {
			return err
		}
		for _, row := range page.Records {
			if row.OccurredAt.Before(start) || row.OccurredAt.After(end) {
				return errors.New("账务返回的付款时间超出查询范围")
			}
			id, err := model.AcceptReceipt(cfg, row)
			if err != nil {
				return errors.New("保存支付宝收款流水失败")
			}
			if id > 0 {
				if err := model.SettleReceiptOrder(id); err != nil {
					scanErr = errors.New("部分已收款订单入账失败，请检查订单日志")
				}
			}
		}
		if pageNo*100 >= page.Total {
			completed = true
			break
		}
		if len(page.Records) == 0 {
			return errors.New("账务分页不完整")
		}
	}
	if !completed {
		return errors.New("账务记录超出单轮查询上限")
	}
	// 仅完整查询成功后才结束到期未支付订单，为账务延迟留出十分钟窗口。
	cutoff := min(end.Unix(), time.Now().Unix()-600)
	for _, top := range pending {
		var receipt model.ReceiptOrder
		if err := model.DB.WithContext(ctx).First(&receipt, "top_up_id = ?", top.Id).Error; err != nil {
			return errors.New("读取收款码订单失败")
		}
		if receipt.ExpiresAt < cutoff {
			if err := model.ExpireReceiptOrder(top.Id); err != nil {
				return err
			}
		}
	}
	return scanErr
}
