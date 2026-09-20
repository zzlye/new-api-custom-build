package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/video_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 保存使用数据库版本比较，多个后台窗口不能悄悄覆盖彼此发布的规则。
func SaveVideoAdapters(registry *video_setting.Registry) error {
	if err := registry.Validate(); err != nil {
		return err
	}
	var next string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var option Option
		err := lockForUpdate(tx).Where(&Option{Key: video_setting.RegistryKey}).First(&option).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var old video_setting.Registry
		if err == nil && option.Value != "" {
			if e := common.UnmarshalJsonStr(option.Value, &old); e != nil {
				return e
			}
		}
		if old.Version != registry.Version {
			return fmt.Errorf("配置已更新，请重新加载后再保存")
		}
		copy := *registry
		copy.Version++
		data, e := common.Marshal(copy)
		if e != nil {
			return e
		}
		next = string(data)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&Option{Key: video_setting.RegistryKey, Value: next}).Error
		}
		result := tx.Model(&Option{}).Where(&Option{Key: video_setting.RegistryKey}).Where("value = ?", option.Value).Update("value", next)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("配置已更新，请重新加载后再保存")
		}
		return nil
	})
	if err != nil {
		return err
	}
	registry.Version++
	return updateOptionMap(video_setting.RegistryKey, next)
}

func VideoChannelCandidates(groups []string, name string) ([]*Channel, error) {
	var ids []int
	if err := DB.Model(&Ability{}).Where(clause.IN{Column: clause.Column{Name: "group"}, Values: videoGroupValues(groups)}).Where("model IN ? AND enabled = ?", []string{name, ratio_setting.FormatMatchingModelName(name)}, true).Distinct("channel_id").Pluck("channel_id", &ids).Error; err != nil {
		return nil, err
	}
	channels := []*Channel{}
	if len(ids) == 0 {
		return channels, nil
	}
	err := DB.Where("id IN ? AND status = ?", ids, common.ChannelStatusEnabled).Find(&channels).Error
	return channels, err
}

func videoGroupValues(groups []string) []any {
	values := make([]any, len(groups))
	for i, group := range groups {
		values[i] = group
	}
	return values
}
