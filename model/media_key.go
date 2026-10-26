package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const TokenPurposeMedia = "media"
const MediaKeyGroupsOption = "MediaKeyGroups"

// 默认密钥不增加钱包余额；未配置分组时仍由鉴权入口拒绝调用。
// 与注册共用事务，唯一约束保证多实例同时补齐也只生成一把。
func EnsureDefaultMediaToken(tx *gorm.DB, userID int) error {
	if userID <= 0 {
		return errors.New("用户 ID 无效")
	}
	var count int64
	if err := tx.Unscoped().Model(&Token{}).Where("default_for_user = ?", userID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	key, err := common.GenerateKey()
	if err != nil {
		return err
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "default_for_user"}}, DoNothing: true}).Create(&Token{
		UserId: userID, DefaultForUser: &userID, Purpose: TokenPurposeMedia,
		Name: "默认生图和视频 Key", Key: key, Status: common.TokenStatusEnabled,
		CreatedTime: common.GetTimestamp(), AccessedTime: common.GetTimestamp(),
		ExpiredTime: -1, UnlimitedQuota: true, Group: "auto", CrossGroupRetry: true,
	}).Error
}

// 升级时按批补齐已有用户，重复启动不重置密钥、停用状态或用户设置。
func BackfillDefaultMediaTokens(db *gorm.DB) error {
	lastID := 0
	for {
		var ids []int
		err := db.Model(&User{}).Where("id > ?", lastID).
			Where("NOT EXISTS (SELECT 1 FROM tokens WHERE tokens.default_for_user = users.id)").
			Order("id").Limit(500).Pluck("id", &ids).Error
		if err != nil || len(ids) == 0 {
			return err
		}
		for _, id := range ids {
			if err := EnsureDefaultMediaToken(db, id); err != nil {
				return err
			}
		}
		lastID = ids[len(ids)-1]
	}
}

// 生图和视频密钥只保存类别，允许分组由 root 的当前配置统一决定。
func ValidateMediaKeyGroups(value string) error {
	var groups []string
	if err := common.UnmarshalJsonStr(value, &groups); err != nil {
		return fmt.Errorf("分组配置必须是数组：%w", err)
	}
	if strings.TrimSpace(value) == "null" {
		return errors.New("分组配置必须是数组")
	}
	seen := make(map[string]bool)
	for _, group := range groups {
		if group == "" || group == "auto" || !ratio_setting.ContainsGroupRatio(group) || seen[group] {
			return fmt.Errorf("分组不存在或重复：%s", group)
		}
		seen[group] = true
	}
	return nil
}

// 每个新请求读取已提交配置，避免多实例和旧令牌缓存延迟撤销分组权限。
func GetMediaKeyGroups() ([]string, error) {
	option := Option{}
	err := DB.Where(&Option{Key: MediaKeyGroupsOption}).First(&option).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var groups []string
	if err = common.UnmarshalJsonStr(option.Value, &groups); err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(groups))
	seen := make(map[string]bool)
	for _, group := range groups {
		if group != "" && group != "auto" && ratio_setting.ContainsGroupRatio(group) && !seen[group] {
			filtered = append(filtered, group)
			seen[group] = true
		}
	}
	return filtered, nil
}

// 不传类别时保留内部调用的全部密钥语义；页面列表显式传入类别。
func tokenPurposeScope(query *gorm.DB, purpose []string) *gorm.DB {
	if len(purpose) == 0 {
		return query
	}
	if purpose[0] == "" {
		return query.Where("purpose = ? OR purpose IS NULL", "")
	}
	return query.Where("purpose = ?", purpose[0])
}
