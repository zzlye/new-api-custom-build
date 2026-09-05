package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var (
	ErrTaskLogRunning   = errors.New("请等待任务结束后再删除日志")
	ErrTaskLogChild     = errors.New("请删除对应的主任务日志")
	ErrTaskLogOtherNode = errors.New("该任务的文件位于其他节点，请在文件所属节点清理")
)

// DeleteTaskLogAndMedia 让单条删除和批量清理使用同一规则，先清文件，再移除任务及关联子记录。
func DeleteTaskLogAndMedia(ctx context.Context, id int64) error {
	var log Task
	if err := DB.WithContext(ctx).First(&log, id).Error; err != nil {
		return err
	}
	if log.AsyncParentID != "" {
		return ErrTaskLogChild
	}
	if log.Status != TaskStatusSuccess && log.Status != TaskStatusFailure {
		return ErrTaskLogRunning
	}
	var task AsyncRelayTask
	err := DB.WithContext(ctx).Where("task_id = ?", log.TaskID).First(&task).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if task.ID != 0 {
		if !task.Status.IsTerminal() {
			return ErrTaskLogRunning
		}
		var activeChildren int64
		if err := DB.WithContext(ctx).Model(&Task{}).Where("async_parent_id = ? AND status NOT IN ?", task.TaskID, []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).Count(&activeChildren).Error; err != nil {
			return err
		}
		if activeChildren > 0 {
			return ErrTaskLogRunning
		}
		// 其他节点仍有文件时保留记录，避免在当前节点误判文件已不存在。
		paths, err := ListAsyncRelayTaskFiles(&task)
		if err != nil {
			return err
		}
		if task.NodeID != "" && task.NodeID != common.NodeName {
			for _, path := range paths {
				if path != "" {
					return ErrTaskLogOtherNode
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ExpireAsyncRelayTaskFiles(&task); err != nil {
			return err
		}
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current Task
		if err := lockForUpdate(tx).First(&current, id).Error; err != nil {
			return err
		}
		if current.Status != TaskStatusSuccess && current.Status != TaskStatusFailure {
			return ErrTaskLogRunning
		}
		if current.AsyncParentID != "" {
			return ErrTaskLogChild
		}
		if task.ID != 0 {
			if err := tx.Where("async_parent_id = ?", task.TaskID).Delete(&Task{}).Error; err != nil {
				return err
			}
			if task.RequestFormat == "mj_proxy" && task.LinkedTaskID != "" {
				if err := tx.Where("user_id = ? AND async_parent_id = ?", task.UserID, task.TaskID).Delete(&Midjourney{}).Error; err != nil {
					return err
				}
			}
			if err := tx.Delete(&AsyncRelayTask{}, task.ID).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&current).Error
	})
}

// oldTaskLogs 只选择截止时间前已结束的主任务，提交很早但仍在执行的任务保持原样。
func oldTaskLogs(ctx context.Context, timestamp, afterID int64) *gorm.DB {
	return DB.WithContext(ctx).Model(&Task{}).
		Where("id > ? AND (async_parent_id = ? OR async_parent_id IS NULL)", afterID, "").
		Where("status IN ?", []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).
		Where("(finish_time > 0 AND finish_time < ?) OR ((finish_time = 0 OR finish_time IS NULL) AND submit_time > 0 AND submit_time < ?)", timestamp, timestamp)
}

func CountOldTaskLogs(ctx context.Context, timestamp, afterID int64) (int64, error) {
	var count int64
	err := oldTaskLogs(ctx, timestamp, afterID).Count(&count).Error
	return count, err
}

type TaskLogCleanupBatch struct {
	LastID  int64
	Deleted int64
	Skipped int64
}

// DeleteOldTaskLogBatch 使用主键游标推进；运行状态有变化或文件位于其他节点时跳过并保留记录。
func DeleteOldTaskLogBatch(ctx context.Context, timestamp, afterID int64, limit int) (TaskLogCleanupBatch, error) {
	result := TaskLogCleanupBatch{LastID: afterID}
	if limit <= 0 {
		limit = 100
	}
	var logs []Task
	if err := oldTaskLogs(ctx, timestamp, afterID).Select("id", "task_id").Order("id asc").Limit(limit).Find(&logs).Error; err != nil {
		return result, err
	}
	for _, log := range logs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		err := DeleteTaskLogAndMedia(ctx, log.ID)
		switch {
		case err == nil:
			result.Deleted++
		case errors.Is(err, ErrTaskLogRunning), errors.Is(err, ErrTaskLogOtherNode), errors.Is(err, gorm.ErrRecordNotFound):
			result.Skipped++
		default:
			return result, fmt.Errorf("清理任务 %s 的文件或日志失败：%w", log.TaskID, err)
		}
		result.LastID = log.ID
	}
	return result, nil
}
