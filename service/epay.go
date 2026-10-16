package service

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func GetCallbackAddress() string {
	if strings.TrimSpace(operation_setting.CustomCallbackAddress) == "" {
		return system_setting.PublicServerURL("")
	}
	// 专用支付回调域名不受媒体域名影响，也不随站点地址被覆盖。
	return strings.TrimRight(strings.TrimSpace(operation_setting.CustomCallbackAddress), "/")
}
