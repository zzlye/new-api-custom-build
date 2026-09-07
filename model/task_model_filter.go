package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// filterTaskModel 按用户提交的模型精确匹配，兼容内部后台任务及旧版原生任务的模型记录。
func filterTaskModel(query *gorm.DB, name string) *gorm.DB {
	name = strings.TrimSpace(name)
	if name == "" {
		return query
	}
	// 三种数据库使用各自的 JSON 取值方式，参数始终单独绑定，不拼接用户输入。
	modelField := "CASE WHEN json_valid(properties) THEN json_extract(properties, '$.origin_model_name') END"
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		modelField = "JSON_UNQUOTE(JSON_EXTRACT(properties, '$.origin_model_name'))"
	} else if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelField = "properties ->> 'origin_model_name'"
	}
	asyncIDs := DB.Model(&AsyncRelayTask{}).Select("task_id").Where("model_name = ?", name)
	return query.Where("task_id IN (?) OR "+modelField+" = ?", asyncIDs, name)
}
