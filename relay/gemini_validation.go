package relay

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// 按最终协议与实际分组确定开关；透传关闭检查时无需把大请求重新读入内存。
func outboundGeminiValidationStatus(c *gin.Context, info *relaycommon.RelayInfo) int {
	if info.GetFinalRequestRelayFormat() != types.RelayFormatGemini {
		return 0
	}
	policy, ok := operation_setting.GetLocalRequestValidationPolicy()
	if !ok || !policy.GenerationConfigCheckEnabled() {
		return 0
	}
	group := info.UsingGroup
	if group == "auto" {
		if actual := common.GetContextKeyString(c, constant.ContextKeyAutoGroup); actual != "" {
			group = actual
		}
	}
	return policy.GroupStatus(group)
}

// 按最终协议、映射后的模型和实际分组检查；关闭策略时不增加模型限制。
func validateOutboundGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, body []byte) *types.NewAPIError {
	status := outboundGeminiValidationStatus(c, info)
	if status == 0 {
		return nil
	}
	if err := helper.ValidateGeminiOutboundParameters(body, info.UpstreamModelName); err != nil {
		// 使用本地错误类型，现有控制器会跳过上游冷却、重试和自动禁用，并保留错误日志。
		return types.NewErrorWithStatusCode(fmt.Errorf("本地参数校验失败：%w", err), types.ErrorCode("local_parameter_error"), status, types.ErrOptionWithSkipRetry())
	}
	return nil
}
