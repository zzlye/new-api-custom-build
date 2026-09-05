package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

// AsyncRelayContextKey 只在服务器重建的请求上下文中设置，客户端请求头没有跳过队列的权限。
const AsyncRelayContextKey = "internal_async_relay_task"

// AsyncRelayMedia 记录由服务器持有的生成文件；路径只供内部使用。
type AsyncRelayMedia struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Kind        string `json:"kind"`
}

// InsertWithLog 在同一事务中创建待处理任务及日志，保证返回编号时已经能查到记录。
func (task *AsyncRelayTask) InsertWithLog(group string, action string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		log := &Task{TaskID: task.TaskID, UserId: task.UserID, Group: group,
			Platform: constant.TaskPlatform("internal"), IsAsync: true, Action: action,
			Status: TaskStatusQueued, Progress: "0%", SubmitTime: task.CreatedAt,
			CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
			Properties: Properties{OriginModelName: task.ModelName},
		}
		if err := tx.Create(log).Error; err != nil {
			return err
		}
		task.LogID = log.ID
		return tx.Model(task).Update("log_id", log.ID).Error
	})
}

// SyncLog 只更新展示字段，避免覆盖上游子任务保留的计费快照。
func (task *AsyncRelayTask) SyncLog(tx *gorm.DB) error {
	if task.LogID == 0 {
		return nil
	}
	status, progress := TaskStatusInProgress, "10%"
	switch task.Status {
	case AsyncRelayTaskStatusPending:
		status, progress = TaskStatusQueued, "0%"
	case AsyncRelayTaskStatusSucceeded:
		status, progress = TaskStatusSuccess, "100%"
	case AsyncRelayTaskStatusFailed, AsyncRelayTaskStatusCancelled:
		status, progress = TaskStatusFailure, "100%"
	}
	return tx.Model(&Task{}).Where("id = ? AND is_async = ?", task.LogID, true).Updates(map[string]any{
		"status": status, "progress": progress, "fail_reason": task.Error,
		"start_time": task.StartedAt, "finish_time": task.FinishedAt, "updated_at": task.UpdatedAt,
	}).Error
}

// ClaimAsyncRelayTaskForNode 通过状态条件抢占本节点任务，同一任务只交给一个执行者。
func ClaimAsyncRelayTaskForNode(nodeID, workerID string) (*AsyncRelayTask, error) {
	for retry := 0; retry < 4; retry++ {
		var candidate AsyncRelayTask
		err := DB.Where("(node_id = ? OR node_id = ? OR node_id IS NULL) AND status IN ?", nodeID, "", []AsyncRelayTaskStatus{AsyncRelayTaskStatusPending, AsyncRelayTaskStatusWaiting}).
			Where("status = ? OR updated_at <= ?", AsyncRelayTaskStatusPending, common.GetTimestamp()-5).
			Where("next_attempt_at <= ? OR next_attempt_at IS NULL", common.GetTimestamp()).
			Order("updated_at asc, id asc").First(&candidate).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		previous := candidate.Status
		candidate.Status, candidate.WorkerID = AsyncRelayTaskStatusProcessing, workerID
		candidate.NodeID = nodeID
		if candidate.StartedAt == 0 {
			candidate.StartedAt = common.GetTimestamp()
		}
		won, err := candidate.UpdateWithStatus(previous)
		if err != nil {
			return nil, err
		}
		if won {
			return &candidate, nil
		}
	}
	return nil, nil
}

// AttachAsyncRelayNativeTask 原子关联已提交的原生视频任务，重启后继续查询而非重复提交。
func AttachAsyncRelayNativeTask(parentID string, child *Task) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		child.AsyncParentID = parentID
		if err := tx.Create(child).Error; err != nil {
			return err
		}
		result := tx.Model(&AsyncRelayTask{}).Where("task_id = ? AND status = ?", parentID, AsyncRelayTaskStatusProcessing).
			Updates(map[string]any{"linked_task_id": child.TaskID, "updated_at": common.GetTimestamp()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("后台任务状态已变化")
		}
		return tx.Model(&Task{}).Where("task_id = ? AND is_async = ?", parentID, true).
			Updates(map[string]any{"quota": child.Quota, "channel_id": child.ChannelId}).Error
	})
}

// AsyncRelayTaskExpired 到期只让结果失效，任务的成功状态与日志仍予以保留。
func AsyncRelayTaskExpired(task *AsyncRelayTask, now int64) bool {
	return task.ResultExpiredAt > 0 || (task.FinishedAt > 0 && task.Status.IsTerminal() && task.FinishedAt+common.AsyncMediaRetentionSeconds() <= now)
}

// ListAsyncRelayTaskFiles 列出清理任务拥有的文件，不接收客户端传入的路径。
func ListAsyncRelayTaskFiles(task *AsyncRelayTask) ([]string, error) {
	paths := []string{task.RequestFilePath, task.ResponseFilePath, task.ResultFilePath}
	if task.ResultFiles == "" {
		return paths, nil
	}
	var media []AsyncRelayMedia
	if err := common.Unmarshal([]byte(task.ResultFiles), &media); err != nil {
		return nil, err
	}
	for _, item := range media {
		paths = append(paths, item.Path)
	}
	return paths, nil
}

// ExpireAsyncRelayTaskFiles 清理完成后才清空路径；删除失败时保留记录以便下次重试。
func ExpireAsyncRelayTaskFiles(task *AsyncRelayTask) error {
	if task == nil || !task.Status.IsTerminal() {
		return fmt.Errorf("任务结束后再清理文件")
	}
	paths, err := ListAsyncRelayTaskFiles(task)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := common.RemoveAsyncMediaFile(path); err != nil {
			return err
		}
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		// 上游子记录可能内含图片或视频的内嵌数据，到期时一并移除但保留计费快照。
		if task.RequestFormat == "task" {
			var children []*Task
			if err := tx.Where("async_parent_id = ?", task.TaskID).Find(&children).Error; err != nil {
				return err
			}
			for _, child := range children {
				child.PrivateData.ResultURL = ""
				if err := tx.Model(child).Updates(map[string]any{"data": nil, "private_data": child.PrivateData}).Error; err != nil {
					return err
				}
			}
		}
		if task.RequestFormat == "mj_proxy" {
			if err := tx.Model(&Midjourney{}).Where("async_parent_id = ?", task.TaskID).Updates(map[string]any{"image_url": "", "video_url": "", "video_urls": ""}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&AsyncRelayTask{}).Where("id = ? AND status IN ?", task.ID,
			[]AsyncRelayTaskStatus{AsyncRelayTaskStatusSucceeded, AsyncRelayTaskStatusFailed, AsyncRelayTaskStatusCancelled}).
			Updates(map[string]any{"request_file_path": "", "request_body": "", "request_files": "", "request_metadata": "", "result_file_path": "", "result_files": "", "response_file_path": "", "response_body": "", "result_expired_at": common.GetTimestamp()}).Error
	})
}

// CleanupExpiredAsyncRelayTasks 只清理本节点到期文件，数据库日志不自动删除。
func CleanupExpiredAsyncRelayTasks(nodeID string) error {
	var tasks []*AsyncRelayTask
	err := DB.Where("(node_id = ? OR node_id = ? OR node_id IS NULL) AND status IN ? AND finished_at > 0 AND finished_at <= ? AND (result_expired_at = 0 OR result_expired_at IS NULL)",
		nodeID, "", []AsyncRelayTaskStatus{AsyncRelayTaskStatusSucceeded, AsyncRelayTaskStatusFailed, AsyncRelayTaskStatusCancelled}, common.GetTimestamp()-common.AsyncMediaRetentionSeconds()).
		Order("id asc").Limit(100).Find(&tasks).Error
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err := ExpireAsyncRelayTaskFiles(task); err != nil {
			return err
		}
	}
	return nil
}
