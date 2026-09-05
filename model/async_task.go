package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// AsyncRelayTaskStatus 表示异步图片或视频请求的生命周期状态。
type AsyncRelayTaskStatus string

const (
	// AsyncRelayTaskDefaultProcessingTimeoutSeconds 是处理任务的默认租约时长。
	AsyncRelayTaskDefaultProcessingTimeoutSeconds int64 = 10 * 60

	AsyncRelayTaskStatusPending    AsyncRelayTaskStatus = "pending"
	AsyncRelayTaskStatusProcessing AsyncRelayTaskStatus = "processing"
	AsyncRelayTaskStatusWaiting    AsyncRelayTaskStatus = "waiting"
	AsyncRelayTaskStatusSucceeded  AsyncRelayTaskStatus = "succeeded"
	AsyncRelayTaskStatusFailed     AsyncRelayTaskStatus = "failed"
	AsyncRelayTaskStatusCancelled  AsyncRelayTaskStatus = "cancelled"

	// 下面的别名用于兼容“排队中/运行中/成功/失败”的调用习惯。
	AsyncRelayTaskStatusQueued  = AsyncRelayTaskStatusPending
	AsyncRelayTaskStatusRunning = AsyncRelayTaskStatusProcessing
	AsyncRelayTaskStatusSuccess = AsyncRelayTaskStatusSucceeded
	AsyncRelayTaskStatusFailure = AsyncRelayTaskStatusFailed
)

// AsyncRelayTask 保存需要后台处理的图片、视频以及 Gemini 生图请求。
// 文件字段只保存服务器本地路径或对象存储地址，不把大文件直接写入数据库。
type AsyncRelayTask struct {
	// 文件下载重试独立于上游提交，重试结果保存时不再次扣费或生成。
	MediaAttempts int    `json:"-"`
	NextAttemptAt int64  `json:"-" gorm:"bigint;index"`
	ID            int64  `json:"id" gorm:"primaryKey"`
	TaskID        string `json:"task_id" gorm:"type:varchar(191);uniqueIndex"`
	UserID        int    `json:"user_id" gorm:"index"`
	TokenID       int    `json:"token_id" gorm:"index"`
	ModelName     string `json:"model,omitempty" gorm:"type:varchar(128);index"`

	// 任务固定在持有请求文件的节点执行；关联编号用于复用上游原生任务的查询与计费。
	NodeID            string `json:"-" gorm:"type:varchar(191);index"`
	LinkedTaskID      string `json:"-" gorm:"type:varchar(191);index"`
	LogID             int64  `json:"-" gorm:"index"`
	RequestMetadata   string `json:"-" gorm:"type:text"`
	DispatchStartedAt int64  `json:"-" gorm:"bigint"`
	ResultExpiredAt   int64  `json:"result_expired_at,omitempty" gorm:"bigint;index"`

	RequestMethod      string `json:"request_method" gorm:"type:varchar(16)"`
	RequestPath        string `json:"request_path" gorm:"type:varchar(255);index"`
	RequestQuery       string `json:"request_query" gorm:"type:text"`
	RequestContentType string `json:"request_content_type" gorm:"type:varchar(128)"`
	RequestFormat      string `json:"request_format,omitempty" gorm:"type:varchar(32)"`
	RequestBody        string `json:"request_body,omitempty" gorm:"type:text"`
	RequestFilePath    string `json:"request_file_path,omitempty" gorm:"type:text"`
	RequestFiles       string `json:"request_files,omitempty" gorm:"type:text"`

	// RequestDetails 保存脱敏后的提示词、参数及参考媒体清单，供任务详情独立读取。
	RequestDetails      string `json:"-"`
	ResponseCompletedAt int64  `json:"response_completed_at,omitempty" gorm:"bigint"`

	// ResponseFilePath 单独保存原接口响应，原生视频继续处理时也能保留提交回执。
	ResponseFilePath    string `json:"-" gorm:"type:text"`
	ResponseStatusCode  int    `json:"response_status_code"`
	ResponseContentType string `json:"response_content_type,omitempty" gorm:"type:varchar(128)"`
	ResponseBody        string `json:"response_body,omitempty" gorm:"type:text"`
	ResultContentType   string `json:"result_content_type,omitempty" gorm:"type:varchar(128)"`
	ResultFilePath      string `json:"result_file_path,omitempty" gorm:"type:text"`
	ResultFiles         string `json:"result_files,omitempty" gorm:"type:text"`

	Status     AsyncRelayTaskStatus `json:"status" gorm:"type:varchar(32);index"`
	Error      string               `json:"error,omitempty" gorm:"type:text"`
	WorkerID   string               `json:"worker_id,omitempty" gorm:"type:varchar(128);index"`
	CreatedAt  int64                `json:"created_at" gorm:"bigint;index"`
	UpdatedAt  int64                `json:"updated_at" gorm:"bigint;index"`
	StartedAt  int64                `json:"started_at,omitempty" gorm:"bigint;index"`
	FinishedAt int64                `json:"finished_at,omitempty" gorm:"bigint;index"`
}

// IsTerminal 判断任务是否已经进入不可继续执行的终态。
func (status AsyncRelayTaskStatus) IsTerminal() bool {
	return status == AsyncRelayTaskStatusSucceeded ||
		status == AsyncRelayTaskStatusFailed ||
		status == AsyncRelayTaskStatusCancelled
}

// GenerateAsyncRelayTaskID 生成对外返回的异步任务编号。
func GenerateAsyncRelayTaskID() (string, error) {
	key, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return "", err
	}
	return "async_" + key, nil
}

// BeforeCreate 为新任务补齐编号、状态和创建时间。
func (task *AsyncRelayTask) BeforeCreate(_ *gorm.DB) error {
	if task.TaskID == "" {
		taskID, err := GenerateAsyncRelayTaskID()
		if err != nil {
			return err
		}
		task.TaskID = taskID
	}
	if task.Status == "" {
		task.Status = AsyncRelayTaskStatusPending
	}
	now := common.GetTimestamp()
	if task.CreatedAt == 0 {
		task.CreatedAt = now
	}
	if task.UpdatedAt == 0 {
		task.UpdatedAt = now
	}
	return nil
}

// CreateAsyncRelayTask 持久化一个新的异步任务。
func CreateAsyncRelayTask(task *AsyncRelayTask) error {
	if task == nil {
		return errors.New("异步任务不能为空")
	}
	return DB.Create(task).Error
}

// Insert 保留与其他模型一致的实例方法调用方式。
func (task *AsyncRelayTask) Insert() error {
	return CreateAsyncRelayTask(task)
}

// GetAsyncRelayTaskByTaskID 按对外任务编号查询任务。
func GetAsyncRelayTaskByTaskID(taskID string) (*AsyncRelayTask, error) {
	if taskID == "" {
		return nil, nil
	}
	var task AsyncRelayTask
	err := DB.Where("task_id = ?", taskID).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// GetAsyncRelayTaskByTaskId 是旧代码常用的 Id 命名方式别名。
func GetAsyncRelayTaskByTaskId(taskID string) (*AsyncRelayTask, error) {
	return GetAsyncRelayTaskByTaskID(taskID)
}

// GetAsyncRelayTaskByUserAndTaskID 只允许任务所属用户读取任务。
func GetAsyncRelayTaskByUserAndTaskID(userID int, taskID string) (*AsyncRelayTask, error) {
	if taskID == "" {
		return nil, nil
	}
	var task AsyncRelayTask
	err := DB.Where("user_id = ? AND task_id = ?", userID, taskID).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// ListAsyncRelayTasksByUserID 返回用户自己的任务，结果按创建顺序倒序排列。
func ListAsyncRelayTasksByUserID(userID int, offset int, limit int) ([]*AsyncRelayTask, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var tasks []*AsyncRelayTask
	err := DB.Where("user_id = ?", userID).
		Order("id desc").Limit(limit).Offset(offset).Find(&tasks).Error
	return tasks, err
}

// GetAllUserAsyncRelayTasks 是用户任务列表的兼容别名。
func GetAllUserAsyncRelayTasks(userID int, offset int, limit int) ([]*AsyncRelayTask, error) {
	return ListAsyncRelayTasksByUserID(userID, offset, limit)
}

// FindPendingAsyncRelayTasks 获取最早提交的待处理任务，供后台 worker 领取。
func FindPendingAsyncRelayTasks(limit int) ([]*AsyncRelayTask, error) {
	if limit <= 0 {
		limit = 1
	}
	var tasks []*AsyncRelayTask
	err := DB.Where("status = ?", AsyncRelayTaskStatusPending).
		Order("id asc").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// HasPendingAsyncRelayTasks 判断是否有待处理任务，使用 LIMIT 1 避免扫描整张表。
func HasPendingAsyncRelayTasks() bool {
	var id int64
	err := DB.Model(&AsyncRelayTask{}).
		Where("status = ?", AsyncRelayTaskStatusPending).
		Limit(1).Pluck("id", &id).Error
	return err == nil && id != 0
}

// RecoverStaleAsyncRelayTasks 将长时间没有更新的处理中任务重新放回队列。
// timeoutSeconds 省略或传入非正数时使用默认的十分钟超时值，返回本次回收数量。
func RecoverStaleAsyncRelayTasks(timeoutSeconds ...int64) (int64, error) {
	timeout := AsyncRelayTaskDefaultProcessingTimeoutSeconds
	if len(timeoutSeconds) > 0 && timeoutSeconds[0] > 0 {
		timeout = timeoutSeconds[0]
	}
	cutoff := common.GetTimestamp() - timeout
	var tasks []*AsyncRelayTask
	if err := DB.Where("status = ? AND updated_at < ?", AsyncRelayTaskStatusProcessing, cutoff).Limit(100).Find(&tasks).Error; err != nil {
		return 0, err
	}
	var recovered int64
	for _, task := range tasks {
		previousWorker := task.WorkerID
		task.Status, task.WorkerID = AsyncRelayTaskStatusPending, ""
		if task.LinkedTaskID != "" || task.ResultFilePath != "" || task.ResponseFilePath != "" {
			task.Status = AsyncRelayTaskStatusWaiting
		} else if task.DispatchStartedAt > 0 {
			// 已提交但结果未知时不自动重发，避免重复生成和重复扣费。
			task.Status = AsyncRelayTaskStatusFailed
			task.Error = "执行进程中断，上游结果待核对，任务未重复提交"
			task.FinishedAt = common.GetTimestamp()
		} else {
			task.StartedAt = 0
		}
		task.UpdatedAt = common.GetTimestamp()
		err := DB.Transaction(func(tx *gorm.DB) error {
			// 心跳时间也参与比较，查询后刚续租的任务不会被误回收。
			result := tx.Model(&AsyncRelayTask{}).Where("id = ? AND status = ? AND worker_id = ? AND updated_at < ?", task.ID, AsyncRelayTaskStatusProcessing, previousWorker, cutoff).Select("*").Updates(task)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return nil
			}
			if err := task.SyncLog(tx); err != nil {
				return err
			}
			recovered++
			return nil
		})
		if err != nil {
			return recovered, err
		}
	}
	return recovered, nil
}

// ClaimAsyncRelayTask 将待处理任务原子地领取为处理中。
// 条件更新是 CAS，多个 worker 同时领取时只有一个会得到 true。
func ClaimAsyncRelayTask(id int64, workerID string) (*AsyncRelayTask, bool, error) {
	return claimAsyncRelayTask(id, AsyncRelayTaskStatusPending, AsyncRelayTaskStatusProcessing, workerID)
}

// ClaimAsyncRelayTaskByTaskID 按对外任务编号原子领取待处理任务。
func ClaimAsyncRelayTaskByTaskID(taskID string, workerID string) (*AsyncRelayTask, bool, error) {
	if taskID == "" {
		return nil, false, nil
	}
	var task AsyncRelayTask
	if err := DB.Where("task_id = ? AND status = ?", taskID, AsyncRelayTaskStatusPending).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return claimAsyncRelayTask(task.ID, AsyncRelayTaskStatusPending, AsyncRelayTaskStatusProcessing, workerID)
}

// ClaimAsyncRelayTaskWithStatus 支持后台 worker 自定义前后状态的 CAS 领取。
func ClaimAsyncRelayTaskWithStatus(id int64, fromStatus AsyncRelayTaskStatus, toStatus AsyncRelayTaskStatus, workerID string) (*AsyncRelayTask, bool, error) {
	return claimAsyncRelayTask(id, fromStatus, toStatus, workerID)
}

func claimAsyncRelayTask(id int64, fromStatus AsyncRelayTaskStatus, toStatus AsyncRelayTaskStatus, workerID string) (*AsyncRelayTask, bool, error) {
	if id <= 0 || fromStatus == "" || toStatus == "" {
		return nil, false, nil
	}
	now := common.GetTimestamp()
	updates := map[string]any{
		"status":     toStatus,
		"worker_id":  workerID,
		"updated_at": now,
	}
	if toStatus == AsyncRelayTaskStatusProcessing {
		updates["started_at"] = now
	}
	result := DB.Model(&AsyncRelayTask{}).
		Where("id = ? AND status = ?", id, fromStatus).
		Updates(updates)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, false, nil
	}
	var task AsyncRelayTask
	if err := DB.Where("id = ?", id).First(&task).Error; err != nil {
		return nil, false, err
	}
	return &task, true, nil
}

// UpdateWithStatus 使用状态条件更新任务，适用于成功、失败等并发状态转换。
func (task *AsyncRelayTask) UpdateWithStatus(fromStatus AsyncRelayTaskStatus) (bool, error) {
	if task == nil || task.ID <= 0 {
		return false, errors.New("异步任务编号无效")
	}
	task.UpdatedAt = common.GetTimestamp()
	if task.Status.IsTerminal() && task.FinishedAt == 0 {
		task.FinishedAt = task.UpdatedAt
	}
	won := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&AsyncRelayTask{}).Where("id = ? AND status = ?", task.ID, fromStatus)
		// 执行租约也参与比较，过期执行者的结果不会覆盖后来领取的任务。
		if fromStatus == AsyncRelayTaskStatusProcessing && task.WorkerID != "" {
			query = query.Where("worker_id = ?", task.WorkerID)
		}
		result := query.Select("*").Updates(task)
		if result.Error != nil {
			return result.Error
		}
		won = result.RowsAffected > 0
		if !won {
			return nil
		}
		return task.SyncLog(tx)
	})
	return won && err == nil, err
}

// Update 保存任务的最新请求处理结果。
func (task *AsyncRelayTask) Update() error {
	if task == nil || task.ID <= 0 {
		return errors.New("异步任务编号无效")
	}
	task.UpdatedAt = common.GetTimestamp()
	if task.Status.IsTerminal() && task.FinishedAt == 0 {
		task.FinishedAt = task.UpdatedAt
	}
	return DB.Model(&AsyncRelayTask{}).Where("id = ?", task.ID).Select("*").Updates(task).Error
}

// UpdateAsyncRelayTaskStatus 按任务编号执行一次状态 CAS 更新。
func UpdateAsyncRelayTaskStatus(taskID string, fromStatus AsyncRelayTaskStatus, toStatus AsyncRelayTaskStatus) (bool, error) {
	if taskID == "" || fromStatus == "" || toStatus == "" {
		return false, nil
	}
	now := common.GetTimestamp()
	updates := map[string]any{"status": toStatus, "updated_at": now}
	if toStatus.IsTerminal() {
		updates["finished_at"] = now
	}
	result := DB.Model(&AsyncRelayTask{}).
		Where("task_id = ? AND status = ?", taskID, fromStatus).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
