package claudecodeadapter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hecatehq/acp-adapter-kit/commandbridge"
	"github.com/hecatehq/acp-adapter-kit/runtimeacp"
)

func NewStreamParser(commandbridge.Session, runtimeacp.PromptParams) commandbridge.StreamParser {
	seenAssistantText := false
	return commandbridge.NewJSONLStreamParser(func(event map[string]any) (commandbridge.JSONLMapping, error) {
		mapping := mapClaudeStreamEvent(event, seenAssistantText)
		for _, streamEvent := range mapping.Events {
			if streamEvent.Update["sessionUpdate"] == "agent_message_chunk" {
				seenAssistantText = true
				break
			}
		}
		return mapping, nil
	})
}

func mapClaudeStreamEvent(event map[string]any, seenAssistantText bool) commandbridge.JSONLMapping {
	switch firstString(event, "type") {
	case "assistant":
		return mapClaudeAssistantMessage(mapValue(event["message"]))
	case "user":
		return mapClaudeUserMessage(mapValue(event["message"]))
	case "result":
		return mapClaudeResult(event, seenAssistantText)
	default:
		return commandbridge.JSONLMapping{}
	}
}

func mapClaudeAssistantMessage(message map[string]any) commandbridge.JSONLMapping {
	var out []commandbridge.StreamEvent
	var transcript strings.Builder
	for _, block := range contentBlocks(message["content"]) {
		switch firstString(block, "type") {
		case "text":
			text := textValue(block["text"])
			if text == "" {
				continue
			}
			out = append(out, commandbridge.AgentMessageChunk(text))
			transcript.WriteString(text)
		case "thinking":
			if text := textValue(block["thinking"]); text != "" {
				out = append(out, commandbridge.AgentThoughtChunk(firstString(block, "id"), text))
			}
		case "tool_use":
			id := firstString(block, "id")
			if id == "" {
				continue
			}
			name := firstString(block, "name")
			out = append(out, commandbridge.ToolCallStart(id, name, claudeToolKind(name), "in_progress", block["input"]))
		}
	}
	return commandbridge.JSONLMapping{Events: out, TranscriptText: transcript.String()}
}

func mapClaudeUserMessage(message map[string]any) commandbridge.JSONLMapping {
	var out []commandbridge.StreamEvent
	for _, block := range contentBlocks(message["content"]) {
		if firstString(block, "type") != "tool_result" {
			continue
		}
		id := firstString(block, "tool_use_id", "toolUseId", "id")
		if id == "" {
			continue
		}
		status := "completed"
		if boolValue(block["is_error"]) {
			status = "failed"
		}
		out = append(out, commandbridge.ToolCallFinish(id, "", "", status, textValue(block["content"])))
	}
	return commandbridge.JSONLMapping{Events: out}
}

func mapClaudeResult(event map[string]any, seenAssistantText bool) commandbridge.JSONLMapping {
	mapping := commandbridge.JSONLMapping{StopReason: claudeStopReason(event)}
	if !seenAssistantText {
		if text := firstText(event, "result", "message", "text"); text != "" {
			mapping.Events = append(mapping.Events, commandbridge.AgentMessageChunk(text))
			mapping.TranscriptText = text
		}
	}
	usage := mapValue(event["usage"])
	if len(usage) == 0 {
		usage = event
	}
	used := sumInts(usage, "input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens", "output_tokens", "thinking_tokens", "total_tokens")
	size := firstInt(usage, "context_window", "context_window_tokens", "size")
	if used > 0 && size > 0 {
		mapping.Events = append(mapping.Events, commandbridge.UsageUpdate(used, size))
	}
	return mapping
}

func claudeToolKind(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(name, "bash"):
		return "execute"
	case strings.Contains(name, "read"):
		return "read"
	case strings.Contains(name, "write"), strings.Contains(name, "edit"), strings.Contains(name, "patch"):
		return "edit"
	case strings.Contains(name, "web"):
		return "fetch"
	case strings.Contains(name, "grep"), strings.Contains(name, "glob"), strings.Contains(name, "search"):
		return "search"
	case strings.Contains(name, "task"):
		return "task"
	case strings.Contains(name, "memory"):
		return "memory"
	case strings.Contains(name, "todo"), strings.Contains(name, "plan"), strings.Contains(name, "think"):
		return "think"
	default:
		return "other"
	}
}

func claudeStopReason(values map[string]any) runtimeacp.StopReason {
	reason := strings.ToLower(firstString(values, "stop_reason", "stopReason", "finish_reason", "finishReason", "reason", "subtype", "status"))
	switch {
	case reason == "":
		return ""
	case strings.Contains(reason, "max_turn"):
		return runtimeacp.StopReasonMaxTurnRequests
	case strings.Contains(reason, "max_token"), strings.Contains(reason, "token_limit"), strings.Contains(reason, "length"):
		return runtimeacp.StopReasonMaxTokens
	case strings.Contains(reason, "refusal"), strings.Contains(reason, "refused"), strings.Contains(reason, "safety"):
		return runtimeacp.StopReasonRefusal
	case strings.Contains(reason, "cancel"):
		return runtimeacp.StopReasonCancelled
	case strings.Contains(reason, "success"), strings.Contains(reason, "end"), strings.Contains(reason, "complete"), strings.Contains(reason, "done"):
		return runtimeacp.StopReasonEndTurn
	default:
		return ""
	}
}

func contentBlocks(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if block := mapValue(item); len(block) > 0 {
				out = append(out, block)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{typed}
	case string:
		return []map[string]any{{"type": "text", "text": typed}}
	default:
		return nil
	}
}

func textValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		var parts []string
		for _, item := range typed {
			if text := textValue(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		for _, key := range []string{"text", "content", "message", "result"} {
			if text := textValue(typed[key]); text != "" {
				return text
			}
		}
	case fmt.Stringer:
		return typed.String()
	}
	return ""
}

func firstText(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := textValue(values[key]); text != "" {
			return text
		}
	}
	return ""
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if text := strings.TrimSpace(textValue(value)); text != "" {
				return text
			}
		}
	}
	return ""
}

func firstInt(values map[string]any, keys ...string) int {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if out := intValue(value); out != 0 {
				return out
			}
		}
	}
	return 0
}

func sumInts(values map[string]any, keys ...string) int {
	total := 0
	for _, key := range keys {
		total += intValue(values[key])
	}
	return total
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(typed))
		return n
	default:
		return 0
	}
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return false
	}
}

func mapValue(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}
