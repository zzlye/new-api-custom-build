// 来源：用户提供的 Sub2API 支付宝收款码接入包，遵循 LGPL-3.0，修改了 JSON 适配和注释。
// Package accountlog 提供固定支付宝正式网关的 V3 账务只读查询。
package alipayaccountlog

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	gateway          = "https://openapi.alipay.com"
	queryPath        = "/v3/alipay/data/bill/accountlog/query"
	maxResponseBytes = 2 * 1024 * 1024
	responseWindow   = 5 * time.Minute
)

var (
	chinaTime     = time.FixedZone("Asia/Shanghai", 8*60*60)
	decimalAmount = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]{1,2})?$`)
	digits        = regexp.MustCompile(`^[0-9]+$`)
	// 扫描器每轮重建签名客户端，但连接池应跨轮共用；账户私钥仍只保存在各自Client中。
	// Transport支持并发请求，不能在运行后再修改字段。
	queryTransport = func() *http.Transport {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.MaxResponseHeaderBytes = 64 * 1024
		return transport
	}()
)

// Record 保留支付宝原始字段；金额不经浮点转换，Raw 仅供业务核验，不应输出日志。
type Record struct {
	AccountLogID  string
	AlipayOrderNo string
	Direction     string
	Type          string
	Amount        string
	OccurredAt    time.Time
	Raw           json.RawMessage
}

// Page 表示已通过完整验签的一页账务记录。
type Page struct {
	Records []Record
	Total   int
}

// Client 不提供网关配置，防止签名请求和账务资料被发往任意地址。
type Client struct {
	appID      string
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	httpClient *http.Client
	now        func() time.Time
}

// New 使用应用私钥签请求，使用支付宝公钥验响应。支持裸 Base64 和 PEM 密钥。
func New(config map[string]string) (*Client, error) {
	appID := strings.TrimSpace(config["appId"])
	if !digits.MatchString(appID) {
		return nil, errors.New("支付宝账务配置缺少有效 appId")
	}
	privateKey, err := parsePrivateKey(config["privateKey"])
	if err != nil {
		return nil, errors.New("支付宝账务应用私钥格式无效")
	}
	publicText := strings.TrimSpace(config["publicKey"])
	if publicText == "" {
		publicText = config["alipayPublicKey"]
	}
	publicKey, err := parsePublicKey(publicText)
	if err != nil {
		return nil, errors.New("支付宝账务公钥格式无效")
	}
	return &Client{
		appID: appID, privateKey: privateKey, publicKey: publicKey,
		now: time.Now,
		httpClient: &http.Client{
			Timeout:       20 * time.Second,
			Transport:     queryTransport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// Query 按北京时间查询一页记录。只读 GET，不创建订单、不发起资金操作。
func (c *Client) Query(ctx context.Context, start, end time.Time, page, pageSize int) (*Page, error) {
	if start.IsZero() || end.IsZero() || !start.Before(end) || page < 1 || pageSize < 1 {
		return nil, errors.New("支付宝账务查询时间或分页参数无效")
	}
	params := url.Values{
		"start_time": {start.In(chinaTime).Format("2006-01-02 15:04:05")},
		"end_time":   {end.In(chinaTime).Format("2006-01-02 15:04:05")},
		"page_no":    {strconv.Itoa(page)}, "page_size": {strconv.Itoa(pageSize)},
	}
	path := queryPath + "?" + params.Encode()
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, errors.New("支付宝账务请求随机数生成失败")
	}
	auth := "app_id=" + c.appID + ",nonce=" + hex.EncodeToString(nonceBytes) + ",timestamp=" + strconv.FormatInt(c.now().UnixMilli(), 10)
	digest := sha256.Sum256([]byte(auth + "\nGET\n" + path + "\n\n"))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return nil, errors.New("支付宝账务请求签名失败")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gateway+path, nil)
	if err != nil {
		return nil, errors.New("支付宝账务请求构造失败")
	}
	req.Header.Set("Authorization", "ALIPAY-SHA256withRSA "+auth+",sign="+base64.StdEncoding.EncodeToString(signature))
	req.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(req)
	if err != nil {
		// 不透传网络错误，避免其中的 URL、签名或对方内容进入日志。
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("支付宝账务查询网络请求失败 [%s]", safeNetworkErrorCategory(err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("支付宝账务查询返回 HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("支付宝账务响应读取失败 [%s]", safeNetworkErrorCategory(err))
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("支付宝账务响应读取失败或超过大小限制")
	}
	return c.VerifyPage(response.Header, body)
}

// VerifyPage 先验签再解析，供查询接口与离线签名样本验证共用同一条路径。
func (c *Client) VerifyPage(header http.Header, body []byte) (*Page, error) {
	if len(body) > maxResponseBytes {
		return nil, errors.New("支付宝账务响应过大")
	}
	if err := c.verifyResponse(header, body); err != nil {
		return nil, err
	}
	return parsePage(body)
}

// 只输出固定类别，不包装原始错误；原错误可能包含签名URL、证书或响应正文。
func safeNetworkErrorCategory(err error) string {
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return "timeout"
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return "dns"
	}
	var certificateError *tls.CertificateVerificationError
	var recordError tls.RecordHeaderError
	if errors.As(err, &certificateError) || errors.As(err, &recordError) {
		return "tls"
	}
	var operationError *net.OpError
	if errors.As(err, &operationError) && operationError.Op == "dial" {
		return "connect"
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "connection_closed"
	}
	return "unknown"
}

func (c *Client) verifyResponse(header http.Header, body []byte) error {
	// 三项头必须唯一且非空；禁止仅验正文而忽略响应时间与随机数。
	for _, key := range []string{"Alipay-Timestamp", "Alipay-Nonce", "Alipay-Signature"} {
		if len(header.Values(key)) != 1 || strings.TrimSpace(header.Get(key)) == "" {
			return errors.New("支付宝账务响应签名头缺失或无效")
		}
	}
	timestamp := header.Get("Alipay-Timestamp")
	millis, err := strconv.ParseInt(timestamp, 10, 64)
	now := c.now().UnixMilli()
	if err != nil || millis < now-responseWindow.Milliseconds() || millis > now+responseWindow.Milliseconds() {
		return errors.New("支付宝账务响应时间戳超出有效窗口")
	}
	signature, err := base64.StdEncoding.DecodeString(header.Get("Alipay-Signature"))
	if err != nil {
		return errors.New("支付宝账务响应签名无效")
	}
	hash := sha256.New()
	hash.Write([]byte(timestamp + "\n" + header.Get("Alipay-Nonce") + "\n"))
	hash.Write(body)
	hash.Write([]byte("\n"))
	if rsa.VerifyPKCS1v15(c.publicKey, crypto.SHA256, hash.Sum(nil), signature) != nil {
		return errors.New("支付宝账务响应验签失败")
	}
	return nil
}

func parsePage(body []byte) (*Page, error) {
	var payload struct {
		Total   json.RawMessage `json:"total_size"`
		Details json.RawMessage `json:"detail_list"`
	}
	if common.Unmarshal(body, &payload) != nil {
		return nil, errors.New("支付宝账务响应格式无效")
	}
	// 接口可能返回字符串或整数，不能把缺失/小数/负数当成空账单。
	totalText := string(payload.Total)
	if strings.HasPrefix(totalText, "\"") {
		if common.Unmarshal(payload.Total, &totalText) != nil {
			return nil, errors.New("支付宝账务总数无效")
		}
	}
	total, err := strconv.Atoi(totalText)
	if err != nil || !digits.MatchString(totalText) || total < 0 {
		return nil, errors.New("支付宝账务总数无效")
	}
	var rows []json.RawMessage
	// 兼容零条账务时省略明细字段的响应。
	// 仅明确总数为零时接受缺失/null；正总数缺明细仍拒绝，避免误当空账单。
	if total == 0 && (len(payload.Details) == 0 || string(payload.Details) == "null") {
		return &Page{Records: []Record{}, Total: 0}, nil
	}
	if len(payload.Details) == 0 || string(payload.Details) == "null" || common.Unmarshal(payload.Details, &rows) != nil || len(rows) > total {
		return nil, errors.New("支付宝账务明细列表无效")
	}
	result := &Page{Records: make([]Record, 0, len(rows)), Total: total}
	for _, raw := range rows {
		var row struct {
			AccountLogID  string `json:"account_log_id"`
			AlipayOrderNo string `json:"alipay_order_no"`
			Direction     string `json:"direction"`
			Type          string `json:"type"`
			Amount        string `json:"trans_amount"`
			OccurredAt    string `json:"trans_dt"`
		}
		// 收入记录的交易类型允许为空，由业务白名单决定是否采用。
		if common.Unmarshal(raw, &row) != nil || row.Direction == "" || !decimalAmount.MatchString(row.Amount) {
			return nil, errors.New("支付宝账务明细字段无效")
		}
		occurredAt, err := time.ParseInLocation("2006-01-02 15:04:05", row.OccurredAt, chinaTime)
		if err != nil || occurredAt.Format("2006-01-02 15:04:05") != row.OccurredAt {
			return nil, errors.New("支付宝账务交易时间无效")
		}
		result.Records = append(result.Records, Record{
			AccountLogID: row.AccountLogID, AlipayOrderNo: row.AlipayOrderNo,
			Direction: row.Direction, Type: row.Type, Amount: row.Amount,
			OccurredAt: occurredAt, Raw: append(json.RawMessage(nil), raw...),
		})
	}
	return result, nil
}

func keyDER(text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "-----BEGIN") {
		block, rest := pem.Decode([]byte(text))
		if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
			return nil, errors.New("invalid key")
		}
		return block.Bytes, nil
	}
	return base64.StdEncoding.DecodeString(text)
}

func parsePrivateKey(text string) (*rsa.PrivateKey, error) {
	der, err := keyDER(text)
	if err != nil {
		return nil, err
	}
	var key *rsa.PrivateKey
	if parsed, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		key, _ = parsed.(*rsa.PrivateKey)
	}
	if key == nil {
		key, _ = x509.ParsePKCS1PrivateKey(der)
	}
	if key == nil || key.N.BitLen() < 2048 || key.Validate() != nil {
		return nil, errors.New("invalid RSA key")
	}
	return key, nil
}

func parsePublicKey(text string) (*rsa.PublicKey, error) {
	der, err := keyDER(text)
	if err != nil {
		return nil, err
	}
	var key *rsa.PublicKey
	if parsed, err := x509.ParsePKIXPublicKey(der); err == nil {
		key, _ = parsed.(*rsa.PublicKey)
	}
	if key == nil {
		key, _ = x509.ParsePKCS1PublicKey(der)
	}
	if key == nil || key.N.BitLen() < 2048 {
		return nil, errors.New("invalid RSA key")
	}
	return key, nil
}
