package helper

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// localToolReference 只保存配对所需标识，不保存工具参数、输出或思考签名。
type localToolReference struct {
	id, name, path string
}

type localToolTurn struct {
	path             string
	calls, responses []localToolReference
}

// validateLocalToolPairing 先归一化消息轮次，再按 ID 或无 ID 的工具名匹配，不修改原请求。
func validateLocalToolPairing(root gjson.Result, gemini bool) error {
	var turns []localToolTurn
	if gemini {
		for i, content := range root.Get("contents").Array() {
			turn := localToolTurn{path: fmt.Sprintf("contents[%d].parts", i)}
			for j, part := range content.Get("parts").Array() {
				for _, key := range []string{"functionCall", "function_call", "functionResponse", "function_response"} {
					value := part.Get(key)
					if !value.Exists() || value.Type == gjson.Null {
						continue
					}
					path := fmt.Sprintf("%s[%d].%s", turn.path, j, key)
					if !value.IsObject() || value.Get("name").Type != gjson.String || strings.TrimSpace(value.Get("name").Str) == "" {
						return localToolPairingError(path, "工具调用或结果缺少有效名称")
					}
					if id := value.Get("id"); id.Exists() && id.Type != gjson.Null && id.Type != gjson.String {
						return localToolPairingError(path, "工具调用 ID 必须是字符串")
					}
					ref := localToolReference{id: value.Get("id").Str, name: value.Get("name").Str, path: path}
					if key == "functionCall" || key == "function_call" {
						turn.calls = append(turn.calls, ref)
					} else {
						turn.responses = append(turn.responses, ref)
					}
				}
			}
			turns = append(turns, turn)
		}
	} else {
		previousTool := false
		for i, message := range root.Get("messages").Array() {
			role := message.Get("role").Str
			// 转换层将系统指令单独收集，不将其作为工具轮次边界。
			if role == "system" || role == "developer" {
				continue
			}
			path := fmt.Sprintf("messages[%d]", i)
			if role == "tool" || role == "function" {
				id := message.Get("tool_call_id")
				if role == "tool" && (id.Type != gjson.String || strings.TrimSpace(id.Str) == "") {
					return localToolPairingError(path, "工具结果缺少有效的 tool_call_id")
				}
				// 与 OpenAI 转 Gemini 现有逻辑保持一致，连续 tool 结果组成一个返回轮次。
				if !previousTool {
					turns = append(turns, localToolTurn{path: path})
				}
				turn := &turns[len(turns)-1]
				turn.responses = append(turn.responses, localToolReference{id: id.Str, name: message.Get("name").Str, path: path})
				previousTool = true
				continue
			}
			previousTool = false
			turn := localToolTurn{path: path}
			if calls := message.Get("tool_calls"); calls.Exists() && calls.Type != gjson.Null && !calls.IsArray() {
				return localToolPairingError(path, "tool_calls 必须是数组")
			}
			for j, call := range message.Get("tool_calls").Array() {
				// 非 function 扩展工具交由其原协议处理，不套用函数配对约束。
				if kind := call.Get("type").Str; kind != "" && kind != "function" {
					return nil
				}
				field := fmt.Sprintf("%s.tool_calls[%d]", path, j)
				if role != "assistant" && role != "model" {
					return localToolPairingError(field, "工具调用必须属于模型消息")
				}
				id, name := call.Get("id"), call.Get("function.name")
				if id.Type != gjson.String || strings.TrimSpace(id.Str) == "" || name.Type != gjson.String || strings.TrimSpace(name.Str) == "" {
					return localToolPairingError(field, "工具调用缺少有效的 ID 或名称")
				}
				turn.calls = append(turn.calls, localToolReference{id: id.Str, name: name.Str, path: field})
			}
			if call := message.Get("function_call"); call.Exists() && call.Type != gjson.Null {
				if len(turn.calls) > 0 || (role != "assistant" && role != "model") || strings.TrimSpace(call.Get("name").Str) == "" {
					return localToolPairingError(path, "旧式 function_call 结构无效或与 tool_calls 混用")
				}
				turn.calls = append(turn.calls, localToolReference{name: call.Get("name").Str, path: path + ".function_call"})
			}
			turns = append(turns, turn)
		}
	}
	var pending *localToolTurn
	for i := range turns {
		turn := &turns[i]
		if len(turn.calls) > 0 {
			if pending != nil {
				return localToolPairingError(pending.path, fmt.Sprintf("本轮调用 %d 个工具，尚未返回结果就开始下一轮调用", len(pending.calls)))
			}
			if len(turn.responses) > 0 {
				return localToolPairingError(turn.path, "同一消息不能混合工具调用和工具结果")
			}
			ids := make(map[string]bool)
			for _, call := range turn.calls {
				if call.id != "" && ids[call.id] {
					return localToolPairingError(call.path, "本轮存在重复的工具调用 ID")
				}
				ids[call.id] = true
			}
			pending = turn
			continue
		}
		if len(turn.responses) == 0 {
			if pending != nil {
				return localToolPairingError(pending.path, fmt.Sprintf("本轮调用 %d 个工具，缺少结果便进入普通消息", len(pending.calls)))
			}
			continue
		}
		if pending == nil {
			return localToolPairingError(turn.path, "工具结果没有对应的调用轮次")
		}
		if len(pending.calls) != len(turn.responses) {
			return localToolPairingError(turn.path, fmt.Sprintf("对应 %s 的 %d 个工具调用，实际返回 %d 个结果", pending.path, len(pending.calls), len(turn.responses)))
		}
		byID := make(map[string]localToolReference)
		byName := make(map[string]int)
		for _, call := range pending.calls {
			if call.id != "" {
				byID[call.id] = call
			} else {
				byName[call.name]++
			}
		}
		responseIDs := make(map[string]bool)
		for _, response := range turn.responses {
			if response.id != "" {
				if responseIDs[response.id] {
					return localToolPairingError(response.path, "本轮存在重复的工具结果 ID")
				}
				responseIDs[response.id] = true
			}
			if call, ok := byID[response.id]; response.id != "" && ok {
				if response.name != "" && response.name != call.name {
					return localToolPairingError(response.path, "结果名称与对应调用的工具名称不一致")
				}
				delete(byID, response.id)
				continue
			}
			// 无 ID 的旧式调用按名称和次数匹配，同名并行调用不会互相覆盖。
			if response.name != "" && byName[response.name] > 0 {
				byName[response.name]--
				continue
			}
			return localToolPairingError(response.path, "工具结果重复、ID 不匹配或找不到对应工具调用")
		}
		pending = nil
	}
	if pending != nil {
		return localToolPairingError(pending.path, fmt.Sprintf("本轮调用 %d 个工具，请求结束时仍缺少结果", len(pending.calls)))
	}
	return nil
}

// 错误只包含位置与数量，不回显可能含敏感信息的工具标识、参数或执行结果。
func localToolPairingError(path, detail string) error {
	return fmt.Errorf("Please ensure that the number of function response parts is equal to the number of function call parts of the function call turn. (%s：%s)", path, detail)
}
