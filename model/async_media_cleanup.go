package model

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// CleanupOrphanAsyncMediaFiles 回收异常退出后尚未来得及关联到任务的结果文件。
func CleanupOrphanAsyncMediaFiles() error {
	referenced := make(map[string]bool)
	var rows []AsyncRelayTask
	err := DB.Select("request_file_path", "result_file_path", "result_files", "id").
		Where("request_file_path <> ? OR result_file_path <> ? OR result_files <> ?", "", "", "").
		FindInBatches(&rows, 100, func(_ *gorm.DB, _ int) error {
			for i := range rows {
				paths, err := ListAsyncRelayTaskFiles(&rows[i])
				if err != nil {
					return err
				}
				for _, path := range paths {
					if path != "" {
						referenced[filepath.Clean(path)] = true
					}
				}
			}
			return nil
		}).Error
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(common.AsyncMediaDir())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// 正在执行的任务最多持续三十分钟，至少一小时后才回收未关联文件。
	cutoff := time.Now().Add(-time.Duration(common.AsyncMediaRetentionSeconds()) * time.Second)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "media-") {
			continue
		}
		path := filepath.Join(common.AsyncMediaDir(), entry.Name())
		if referenced[filepath.Clean(path)] {
			continue
		}
		info, err := entry.Info()
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := common.RemoveAsyncMediaFile(path); err != nil {
			return err
		}
	}
	return nil
}
