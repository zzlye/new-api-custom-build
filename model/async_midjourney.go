package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// InsertMidjourneyForAsyncRelay 保留原有绘图记录，并在同一事务内保存后台任务关联。
func InsertMidjourneyForAsyncRelay(task *Midjourney, parentID string) error {
	if parentID == "" {
		return task.Insert()
	}
	task.AsyncParentID = parentID
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		result := tx.Model(&AsyncRelayTask{}).Where("task_id = ? AND status = ?", parentID, AsyncRelayTaskStatusProcessing).
			Updates(map[string]any{"linked_task_id": task.MjId, "updated_at": common.GetTimestamp()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("后台绘图任务状态已变化")
		}
		return nil
	})
}
