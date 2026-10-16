package system_setting

import "strings"

var ServerAddress = "http://localhost:3000"
var TaskPublicAddress = ""

// PublicServerURL 拼接站点公开链接，统一处理末尾斜杠并保留配置的路径前缀。
func PublicServerURL(path string) string {
	return strings.TrimRight(strings.TrimSpace(ServerAddress), "/") + path
}

// PublicTaskBaseAddress 任务对外地址优先于站点地址，每次读取当前配置，历史任务也随设置更新。
func PublicTaskBaseAddress() string {
	if address := strings.TrimSpace(TaskPublicAddress); address != "" {
		return strings.TrimRight(address, "/")
	}
	return strings.TrimRight(strings.TrimSpace(ServerAddress), "/")
}

// PublicTaskURL 保留管理员配置的端口与反向代理路径前缀，不采用客户端 Host。
func PublicTaskURL(path string) string {
	return PublicTaskBaseAddress() + path
}

var WorkerUrl = ""
var WorkerValidKey = ""
var WorkerAllowHttpImageRequestEnabled = false

func EnableWorker() bool {
	return WorkerUrl != ""
}
