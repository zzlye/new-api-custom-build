package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func GetAllTask(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	// 解析其他查询参数
	queryParams := model.SyncTaskQueryParams{
		ModelName:      strings.TrimSpace(c.Query("model_name")),
		Platform:       constant.TaskPlatform(c.Query("platform")),
		TaskID:         c.Query("task_id"),
		Status:         c.Query("status"),
		Action:         c.Query("action"),
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		ChannelID:      c.Query("channel_id"),
	}

	items := model.TaskGetAllTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.TaskCountAllTasks(queryParams)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tasksToDto(items, true))
	common.ApiSuccess(c, pageInfo)
}

func GetUserTask(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	userId := c.GetInt("id")

	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	queryParams := model.SyncTaskQueryParams{
		ModelName:      strings.TrimSpace(c.Query("model_name")),
		Platform:       constant.TaskPlatform(c.Query("platform")),
		TaskID:         c.Query("task_id"),
		Status:         c.Query("status"),
		Action:         c.Query("action"),
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}

	items := model.TaskGetAllUserTask(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.TaskCountAllUserTask(userId, queryParams)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tasksToDto(items, false))
	common.ApiSuccess(c, pageInfo)
}

func tasksToDto(tasks []*model.Task, fillUser bool) []*dto.TaskDto {
	var userIdMap map[int]*model.UserBase
	if fillUser {
		userIdMap = make(map[int]*model.UserBase)
		userIds := types.NewSet[int]()
		for _, task := range tasks {
			userIds.Add(task.UserId)
		}
		for _, userId := range userIds.Items() {
			cacheUser, err := model.GetUserCache(userId)
			if err == nil {
				userIdMap[userId] = cacheUser
			}
		}
	}
	// 一次加载本页后台任务，避免每行单独访问数据库。
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if task.IsAsync {
			ids = append(ids, task.TaskID)
		}
	}
	asyncTasks := make(map[string]*model.AsyncRelayTask)
	if len(ids) > 0 {
		var rows []*model.AsyncRelayTask
		// 列表仅加载摘要，完整提示词与参考图清单由详情接口按需读取。
		if err := model.DB.Select("task_id", "request_method", "request_path", "model_name", "request_format", "status", "result_file_path", "finished_at", "result_expired_at", "result_files", "response_completed_at").Where("task_id IN ?", ids).Find(&rows).Error; err == nil {
			for _, row := range rows {
				asyncTasks[row.TaskID] = row
			}
		}
	}
	result := make([]*dto.TaskDto, len(tasks))
	for i, task := range tasks {
		if fillUser {
			if user, ok := userIdMap[task.UserId]; ok {
				task.Username = user.Username
			}
		}
		result[i] = relay.TaskModel2Dto(task)
		result[i].ModelName = task.Properties.OriginModelName
		result[i].IsAsync = task.IsAsync
		result[i].DurationFinishTime = task.FinishTime
		if asyncTask := asyncTasks[task.TaskID]; asyncTask != nil {
			result[i].RequestMethod, result[i].RequestPath, result[i].ModelName = asyncTask.RequestMethod, asyncTask.RequestPath, asyncTask.ModelName
			result[i].MediaSaving = asyncTask.ResultFilePath != "" && !asyncTask.Status.IsTerminal()
			// 普通生图的耗时以接口返回为准，后台文件补存不会虚增生成耗时。
			if asyncTask.ResponseCompletedAt > 0 && asyncTask.RequestFormat != "task" && asyncTask.RequestFormat != "mj_proxy" {
				result[i].DurationFinishTime = asyncTask.ResponseCompletedAt
			}
			result[i].MediaExpired = model.AsyncRelayTaskExpired(asyncTask, common.GetTimestamp())
			if asyncTask.FinishedAt > 0 {
				result[i].ExpiresAt = asyncTask.FinishedAt + common.AsyncMediaRetentionSeconds()
			}
			if asyncTask.Status == model.AsyncRelayTaskStatusSucceeded {
				result[i].Media = asyncRelayMediaLinks(asyncTask, "/api/task/")
			}
		}
	}
	return result
}
