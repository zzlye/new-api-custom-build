package service

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"gorm.io/gorm/clause"
)

// 在计费前校验素材内容、所有权和有效期，不把失效素材交给上游尝试生成。
func ValidateVideoAssets(input map[string]any, userID int) error {
	for _, key := range []string{"image_urls", "video_urls", "audio_urls", "first_frame", "last_frame"} {
		value, ok := input[key]
		if !ok {
			continue
		}
		values, array := value.([]any)
		if !array {
			values = []any{value}
		}
		kind := "image"
		if key == "video_urls" {
			kind = "video"
		}
		if key == "audio_urls" {
			kind = "audio"
		}
		for _, value := range values {
			if text, ok := value.(string); ok {
				if !strings.HasPrefix(text, "data:") {
					continue
				}
				parts := strings.SplitN(text, ",", 2)
				if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
					return fmt.Errorf("素材 Data URL 格式无效")
				}
				reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(parts[1]))
				head := make([]byte, 512)
				n, err := io.ReadFull(reader, head)
				if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
					return fmt.Errorf("素材编码无效")
				}
				if !strings.HasPrefix(http.DetectContentType(head[:n]), kind+"/") {
					return fmt.Errorf("素材内容与 %s 类型不一致", kind)
				}
				size, err := io.Copy(io.Discard, io.LimitReader(reader, common.AsyncMediaMaxFileBytes-int64(n)+1))
				if err != nil || size+int64(n) > common.AsyncMediaMaxFileBytes {
					return fmt.Errorf("素材编码无效或超过大小上限")
				}
				continue
			}
			object, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("素材格式无效")
			}
			var asset model.VideoAsset
			if err := model.DB.Where("id = ? AND user_id = ?", object["asset_id"], userID).First(&asset).Error; err != nil {
				return fmt.Errorf("素材不存在或已过期")
			}
			if asset.Kind != kind || asset.NodeName != common.NodeName {
				return fmt.Errorf("素材类型或存储节点不匹配，请重新上传")
			}
			if _, err := os.Stat(asset.Path); err != nil {
				return fmt.Errorf("素材已过期")
			}
			if asset.CreatedAt+86400 <= common.GetTimestamp() {
				var uses []model.VideoAssetUse
				if err := model.DB.Where("asset_id = ?", asset.ID).Find(&uses).Error; err != nil {
					return err
				}
				active := false
				for _, use := range uses {
					keep, err := model.VideoAssetUseActive(use, common.GetTimestamp())
					if err != nil {
						return err
					}
					active = active || keep
				}
				if !active {
					return fmt.Errorf("素材已过期")
				}
			}
		}
	}
	return nil
}

// 受理任务即绑定上传素材，排队期间也保留引用；不在此步骤访问任何上游。
func BindVideoAssets(input map[string]any, userID int, taskID string) error {
	if err := ValidateVideoAssets(input, userID); err != nil {
		return err
	}
	for _, key := range []string{"image_urls", "video_urls", "audio_urls", "first_frame", "last_frame"} {
		value, ok := input[key]
		if !ok {
			continue
		}
		values, array := value.([]any)
		if !array {
			values = []any{value}
		}
		for _, value := range values {
			if object, ok := value.(map[string]any); ok {
				id, _ := object["asset_id"].(string)
				use := model.VideoAssetUse{AssetID: id, TaskID: taskID, CreatedAt: common.GetTimestamp()}
				if err := model.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&use).Error; err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func SaveVideoAsset(userID int, reader io.Reader) (*model.VideoAsset, error) {
	dir := filepath.Join(common.AsyncMediaDir(), "video-assets")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(dir, "asset-")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(file.Name())
		}
	}()
	buf := make([]byte, 512)
	n, err := io.ReadFull(reader, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	buf = buf[:n]
	mime := http.DetectContentType(buf)
	kind := strings.Split(mime, "/")[0]
	if kind != "image" && kind != "video" && kind != "audio" {
		return nil, fmt.Errorf("请上传有效的图片、视频或音频文件")
	}
	if _, err = file.Write(buf); err != nil {
		return nil, err
	}
	size, err := io.Copy(file, io.LimitReader(reader, common.AsyncMediaMaxFileBytes-int64(n)+1))
	if err != nil {
		return nil, err
	}
	size += int64(n)
	if size > common.AsyncMediaMaxFileBytes {
		return nil, fmt.Errorf("素材超过单文件大小上限")
	}
	if err = file.Sync(); err != nil {
		return nil, err
	}
	asset := &model.VideoAsset{ID: "va_" + common.GetUUID(), UserID: userID, NodeName: common.NodeName, Path: file.Name(), Kind: kind, ContentType: mime, Size: size, CreatedAt: common.GetTimestamp()}
	if err = model.DB.Create(asset).Error; err != nil {
		return nil, err
	}
	keep = true
	return asset, nil
}

func ResolveVideoMedia(input map[string]any, userID int, taskID string, urlOnly bool) error {
	for _, key := range []string{"image_urls", "video_urls", "audio_urls", "first_frame", "last_frame"} {
		value, ok := input[key]
		if !ok {
			continue
		}
		kind := "image"
		if key == "video_urls" {
			kind = "video"
		}
		if key == "audio_urls" {
			kind = "audio"
		}
		if values, ok := value.([]any); ok {
			for i, v := range values {
				next, err := resolveVideoMediaSource(v, userID, taskID, kind, urlOnly)
				if err != nil {
					return err
				}
				values[i] = next
			}
		} else {
			next, err := resolveVideoMediaSource(value, userID, taskID, kind, urlOnly)
			if err != nil {
				return err
			}
			input[key] = next
		}
	}
	return nil
}

func resolveVideoMediaSource(value any, userID int, taskID, kind string, urlOnly bool) (any, error) {
	var asset model.VideoAsset
	if text, ok := value.(string); ok {
		if !strings.HasPrefix(text, "data:") {
			return value, nil
		}
		if !urlOnly {
			return value, nil
		}
		// 内联素材同样落盘，任务快照只保留签名引用，避免重复保存大段 Base64。
		parts := strings.SplitN(text, ",", 2)
		if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
			return nil, fmt.Errorf("素材 Data URL 格式无效")
		}
		created, err := SaveVideoAsset(userID, base64.NewDecoder(base64.StdEncoding, strings.NewReader(parts[1])))
		if err != nil {
			return nil, err
		}
		asset = *created
	} else {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("素材格式无效")
		}
		id, _ := object["asset_id"].(string)
		if err := model.DB.Where("id = ? AND user_id = ?", id, userID).First(&asset).Error; err != nil {
			return nil, fmt.Errorf("素材不存在或已过期")
		}
	}
	if asset.Kind != kind {
		return nil, fmt.Errorf("素材类型与 %s 不一致", kind)
	}
	if asset.NodeName != common.NodeName {
		return nil, fmt.Errorf("素材不在当前节点，请重新上传")
	}
	if _, err := os.Stat(asset.Path); err != nil {
		return nil, fmt.Errorf("素材已过期，请重新上传")
	}
	use := model.VideoAssetUse{AssetID: asset.ID, TaskID: taskID, CreatedAt: common.GetTimestamp()}
	if err := model.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&use).Error; err != nil {
		return nil, err
	}
	base := strings.TrimRight(system_setting.ServerAddress, "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("请先配置可公开访问的站点地址")
	}
	path := "/v1/video/assets/" + asset.ID + "/content"
	signature := common.GenerateHMACWithKey([]byte(common.SessionSecret), "video-asset-v1\n"+asset.ID+"\n"+taskID)
	query := url.Values{"task": {taskID}, "signature": {signature}}
	return base + path + "?" + query.Encode(), nil
}

// 保留参数快照时通过序列化复制，避免后续构建请求修改同一张表。
func CloneVideoInput(input map[string]any) (map[string]any, error) {
	data, err := common.Marshal(input)
	if err != nil {
		return nil, err
	}
	var copy map[string]any
	err = common.DecodeJson(bytes.NewReader(data), &copy)
	return copy, err
}
