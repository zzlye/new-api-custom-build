package relay

import (
	"bytes"
	"io"
	"net/http"
	"slices"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// 按请求明确声明的图片能力选择原生链路，不根据模型别名猜测协议。
func getGeminiRequestAdaptor(info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) channel.Adaptor {
	if info.ChannelType == constant.ChannelTypeOpenAI &&
		info.ApiType == constant.APITypeOpenAI &&
		info.RelayFormat == types.RelayFormatGemini &&
		info.RelayMode == relayconstant.RelayModeGemini && request != nil {
		imageConfig := bytes.TrimSpace(request.GenerationConfig.ImageConfig)
		hasImageConfig := len(imageConfig) > 0 && !bytes.Equal(imageConfig, []byte("null"))
		if hasImageConfig || slices.Contains(request.GenerationConfig.ResponseModalities, "IMAGE") {
			// 同时保留原生请求、模型映射后的地址和原生响应，避免只保留正文却仍发到 Chat 接口。
			return &geminiImageNativeAdaptor{}
		}
	}
	return GetAdaptor(info.ApiType)
}

// 普通 OpenAI 渠道中的 Gemini 图片请求复用原生编解码，但保留该渠道原有的鉴权方式。
type geminiImageNativeAdaptor struct {
	gemini.Adaptor
}

func (a *geminiImageNativeAdaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	// 继续使用实际渠道的密钥和管理员头覆盖，不把客户端令牌转发给上游。
	return (&openai.Adaptor{}).SetupRequestHeader(c, header, info)
}

func (a *geminiImageNativeAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	// 必须传入外层适配器，确保原生地址与上面的渠道鉴权在同一次请求中生效。
	return channel.DoApiRequest(a, c, info, body)
}
