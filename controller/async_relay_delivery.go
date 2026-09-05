package controller

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// asyncRelayDeliveries 只连接本次 HTTP 请求与后台执行者，任务本身仍由持久队列管理。
var asyncRelayDeliveries sync.Map

type asyncRelayDeliveryState struct {
	path     string
	headers  http.Header
	status   int
	size     int64
	finished bool
	failure  string
}

// asyncRelayDelivery 只通知已写入文件的字节位置，不把大图片缓存进通知队列。
type asyncRelayDelivery struct {
	mu      sync.Mutex
	changed chan struct{}
	state   asyncRelayDeliveryState
}

func (delivery *asyncRelayDelivery) publish(path string, headers http.Header, status int, size int64) {
	if delivery == nil {
		return
	}
	delivery.mu.Lock()
	if delivery.state.status == 0 {
		delivery.state.path = path
		delivery.state.headers = headers.Clone()
		delivery.state.status = status
	}
	delivery.state.size = size
	delivery.mu.Unlock()
	select {
	case delivery.changed <- struct{}{}:
	default:
	}
}

// finish 结束的是原接口回执，而不是后台媒体保存或原生视频任务的整个生命周期。
func (delivery *asyncRelayDelivery) finish(failure string) {
	if delivery == nil {
		return
	}
	delivery.mu.Lock()
	if !delivery.state.finished {
		delivery.state.finished = true
		delivery.state.failure = failure
	}
	delivery.mu.Unlock()
	select {
	case delivery.changed <- struct{}{}:
	default:
	}
}

// serve 沿用上游状态、响应结构和流式片段；客户端断开只停止转发，不取消后台生成。
func (delivery *asyncRelayDelivery) serve(c *gin.Context, taskID string) error {
	// 同一节点运行多个进程时，另一执行者完成的响应也能从持久记录恢复。
	if err := delivery.restore(taskID); err != nil {
		return err
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	var file *os.File
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	var offset int64
	var buffer []byte
	started := false
	for {
		if c.Request.Context().Err() != nil {
			return nil
		}
		delivery.mu.Lock()
		state := delivery.state
		delivery.mu.Unlock()
		if state.status != 0 {
			if !started {
				var err error
				file, err = os.Open(state.path)
				if err != nil {
					return fmt.Errorf("读取后台接口响应失败")
				}
				buffer = make([]byte, 32*1024)
				// 传输分段由当前连接管理，业务响应头则保持原接口含义。
				excluded := map[string]bool{"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true, "Proxy-Authorization": true, "Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true, "Content-Length": true}
				for _, value := range state.headers.Values("Connection") {
					for _, name := range strings.Split(value, ",") {
						excluded[http.CanonicalHeaderKey(strings.TrimSpace(name))] = true
					}
				}
				for name, values := range state.headers {
					if excluded[http.CanonicalHeaderKey(name)] {
						continue
					}
					c.Writer.Header()[name] = append([]string(nil), values...)
				}
				c.Writer.WriteHeader(state.status)
				c.Writer.WriteHeaderNow()
				started = true
			}
			if state.size > offset {
				copied, err := io.CopyBuffer(c.Writer, io.NewSectionReader(file, offset, state.size-offset), buffer)
				offset += copied
				if err != nil {
					if c.Request.Context().Err() == nil {
						common.SysError("转发后台接口响应失败: " + err.Error())
					}
					return nil
				}
			}
			if strings.Contains(strings.ToLower(state.headers.Get("Content-Type")), "text/event-stream") {
				c.Writer.Flush()
			}
		}
		if state.finished {
			if started {
				return nil
			}
			if state.failure != "" {
				return fmt.Errorf("%s", state.failure)
			}
			return fmt.Errorf("后台执行未返回接口响应")
		}
		select {
		case <-c.Request.Context().Done():
			return nil
		case <-delivery.changed:
		case <-ticker.C:
			if state.status == 0 {
				if err := delivery.restore(taskID); err != nil {
					return err
				}
			}
		}
	}
}

// restore 恢复已持久化的完整响应；未结束的本进程流式请求仍使用实时通知。
func (delivery *asyncRelayDelivery) restore(taskID string) error {
	task, err := model.GetAsyncRelayTaskByTaskID(taskID)
	if err != nil {
		return fmt.Errorf("读取后台任务状态失败")
	}
	if task == nil {
		return fmt.Errorf("后台任务记录已不存在")
	}
	if task.ResponseFilePath != "" {
		info, err := os.Stat(task.ResponseFilePath)
		if err != nil {
			return fmt.Errorf("读取已保存的接口响应失败")
		}
		headers := make(http.Header)
		if task.ResponseContentType != "" {
			headers.Set("Content-Type", task.ResponseContentType)
		}
		delivery.publish(task.ResponseFilePath, headers, task.ResponseStatusCode, info.Size())
		delivery.finish("")
	} else if task.Status.IsTerminal() {
		delivery.finish(task.Error)
	}
	return nil
}
