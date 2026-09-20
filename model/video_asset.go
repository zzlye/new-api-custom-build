package model

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type VideoAsset struct {
	ID          string `gorm:"primaryKey;type:varchar(64)" json:"asset_id"`
	UserID      int    `gorm:"index" json:"-"`
	NodeName    string `gorm:"index;type:varchar(191)" json:"-"`
	Path        string `json:"-"`
	Kind        string `json:"kind"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	CreatedAt   int64  `gorm:"index" json:"-"`
}

type VideoAssetUse struct {
	AssetID   string `gorm:"primaryKey;type:varchar(64)"`
	TaskID    string `gorm:"primaryKey;type:varchar(191)"`
	CreatedAt int64
}

// 活跃任务保留素材，终态使用站点既有保留时长；首次提交落库前允许短暂读取。
func VideoAssetUseActive(use VideoAssetUse, now int64) (bool, error) {
	var parent AsyncRelayTask
	err := DB.Where("task_id = ?", use.TaskID).First(&parent).Error
	if err == nil {
		return !parent.Status.IsTerminal() || parent.FinishedAt+common.AsyncMediaRetentionSeconds() > now, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	var task Task
	err = DB.Where("task_id = ?", use.TaskID).First(&task).Error
	if err == nil {
		return task.Status != TaskStatusSuccess && task.Status != TaskStatusFailure || task.FinishTime+common.AsyncMediaRetentionSeconds() > now, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	return use.CreatedAt+1800 > now, nil
}

func CleanupVideoAssets() error {
	var assets []VideoAsset
	err := DB.Where("node_name = ?", common.NodeName).FindInBatches(&assets, 100, func(tx *gorm.DB, _ int) error {
		for _, asset := range assets {
			var uses []VideoAssetUse
			if e := DB.Where("asset_id = ?", asset.ID).Find(&uses).Error; e != nil {
				return e
			}
			// 未绑定上传保留一天；已绑定素材遵守任务终态的保留期限。
			active := len(uses) == 0 && asset.CreatedAt+86400 > common.GetTimestamp()
			for _, use := range uses {
				keep, e := VideoAssetUseActive(use, common.GetTimestamp())
				if e != nil {
					return e
				}
				active = active || keep
			}
			if active {
				continue
			}
			if e := common.RemoveAsyncMediaFile(asset.Path); e != nil {
				return e
			}
			if e := DB.Where("asset_id = ?", asset.ID).Delete(&VideoAssetUse{}).Error; e != nil {
				return e
			}
			if e := DB.Delete(&asset).Error; e != nil {
				return e
			}
		}
		return nil
	}).Error
	if err != nil {
		return err
	}
	// 上传中断留下的文件没有数据库记录，超过一天再清理。
	entries, err := os.ReadDir(filepath.Join(common.AsyncMediaDir(), "video-assets"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, e := entry.Info()
		if e != nil {
			return e
		}
		if info.ModTime().Unix()+86400 > common.GetTimestamp() {
			continue
		}
		path := filepath.Join(common.AsyncMediaDir(), "video-assets", entry.Name())
		var count int64
		if e = DB.Model(&VideoAsset{}).Where("path = ?", path).Count(&count).Error; e != nil {
			return e
		}
		if count == 0 {
			if e = common.RemoveAsyncMediaFile(path); e != nil {
				return e
			}
		}
	}
	return nil
}
