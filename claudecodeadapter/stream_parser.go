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
	toolMetadata := map[string]claudeToolMetadata{}
	return commandbridge.NewJSONLStreamParser(func(event map[string]any) (commandbridge.JSONLMapping, error) {
		mapping := mapClaudeStreamEvent(event, seenAssistantText)
		for _, streamEvent := range mapping.Events {
			if streamEvent.Update["sessionUpdate"] == "agent_message_chunk" {
				seenAssistantText = true
			}
			trackClaudeToolMetadata(toolMetadata, streamEvent.Update)
		}
		return mapping, nil
	})
}

type claudeToolMetadata struct {
	title string
	kind  string
}

func trackClaudeToolMetadata(known map[string]claudeToolMetadata, update map[string]any) {
	if len(update) == 0 {
		return
	}
	id := firstString(update, "toolCallId")
	if id == "" {
		return
	}
	switch firstString(update, "sessionUpdate") {
	case "tool_call":
		known[id] = claudeToolMetadata{
			title: firstString(update, "title"),
			kind:  firstString(update, "kind"),
		}
	case "tool_call_update":
		if metadata, ok := known[id]; ok {
			if firstString(update, "title") == "" && metadata.title != "" {
				update["title"] = metadata.title
			}
			if firstString(update, "kind") == "" && metadata.kind != "" {
				update["kind"] = metadata.kind
			}
			switch firstString(update, "status") {
			case "completed", "failed", "cancelled":
				delete(known, id)
			}
		}
	}
}

func mapClaudeStreamEvent(event map[string]any, seenAssistantText bool) commandbridge.JSONLMapping {
	eventType := firstString(event, "type")
	switch eventType {
	case "assistant":
		return mapClaudeAssistantMessage(mapValue(event["message"]))
	case "user":
		return mapClaudeUserMessage(mapValue(event["message"]))
	case "result":
		return mapClaudeResult(event, seenAssistantText)
	default:
		if isClaudePermissionRequest(eventType, event) {
			return mapClaudePermissionRequest(event)
		}
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
		out = append(out, commandbridge.ToolCallFinish(id, "", "", claudeToolResultStatus(block), claudeToolResultRawOutput(block)))
	}
	return commandbridge.JSONLMapping{Events: out}
}

func claudeToolResultStatus(block map[string]any) string {
	if boolValue(block["is_error"]) || boolValue(block["error"]) {
		return "failed"
	}
	status := strings.ToLower(firstString(block, "status", "state", "outcome", "result_status", "resultStatus"))
	if claudeToolStatusFailed(status) {
		return "failed"
	}
	if code := firstInt(block, "exit_code", "exitCode"); code != 0 {
		return "failed"
	}
	if content := mapValue(block["content"]); len(content) > 0 {
		if status := strings.ToLower(firstString(content, "status", "state", "outcome", "result_status", "resultStatus")); claudeToolStatusFailed(status) {
			return "failed"
		}
		if code := firstInt(content, "exit_code", "exitCode"); code != 0 {
			return "failed"
		}
	}
	return "completed"
}

func claudeToolStatusFailed(status string) bool {
	switch {
	case status == "":
		return false
	case strings.Contains(status, "fail"),
		strings.Contains(status, "error"),
		strings.Contains(status, "cancel"),
		strings.Contains(status, "reject"),
		strings.Contains(status, "deni"),
		strings.Contains(status, "block"),
		strings.Contains(status, "timeout"),
		strings.Contains(status, "timed_out"),
		strings.Contains(status, "interrupt"),
		strings.Contains(status, "abort"):
		return true
	default:
		return false
	}
}

func claudeToolResultRawOutput(block map[string]any) any {
	if output := claudeStructuredToolOutput(block); len(output) > 0 {
		return output
	}
	content := block["content"]
	if output := claudeStructuredToolOutput(mapValue(content)); len(output) > 0 {
		return output
	}
	if text := textValue(content); text != "" {
		return text
	}
	return firstValue(block, "rawOutput", "raw_output", "result")
}

func claudeStructuredToolOutput(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, key := range []string{"stdout", "stderr", "exit_code", "exitCode"} {
		if value, ok := values[key]; ok {
			out[key] = value
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

func isClaudePermissionRequest(eventType string, event map[string]any) bool {
	eventType = strings.ToLower(eventType)
	if !strings.Contains(eventType, "permission") &&
		!strings.Contains(eventType, "approval") &&
		!strings.Contains(eventType, "can_use_tool") {
		return false
	}
	if strings.Contains(eventType, "response") ||
		strings.Contains(eventType, "result") ||
		strings.Contains(eventType, "resolved") ||
		strings.Contains(eventType, "decision") {
		return false
	}
	return len(claudePermissionToolCall(event)) > 0
}

func mapClaudePermissionRequest(event map[string]any) commandbridge.JSONLMapping {
	toolCall := claudePermissionToolCall(event)
	id := firstString(toolCall, "tool_use_id", "toolUseId", "tool_call_id", "toolCallId", "id")
	title := claudePermissionToolTitle(toolCall)
	kind := firstString(toolCall, "kind")
	if kind == "" {
		kind = claudeToolKind(firstString(toolCall, "type", "tool_type", "toolType"))
		if kind == "other" {
			kind = claudeToolKind(title)
		}
	}
	rawInput := firstValue(toolCall, "rawInput", "raw_input", "input", "arguments")
	if rawInput == nil {
		rawInput = firstValue(event, "rawInput", "raw_input", "input", "arguments")
	}
	return commandbridge.JSONLMapping{Events: []commandbridge.StreamEvent{
		commandbridge.ToolCallPermissionRequest(id, title, kind, rawInput, claudePermissionOptions(event)),
	}}
}

func claudePermissionToolCall(event map[string]any) map[string]any {
	for _, key := range []string{"toolCall", "tool_call", "toolUse", "tool_use", "tool"} {
		if value := mapValue(event[key]); len(value) > 0 {
			return value
		}
	}
	return event
}

func claudePermissionToolTitle(toolCall map[string]any) string {
	explicitServer := firstString(toolCall, "server", "server_name", "serverName")
	if explicitServer == "" && !strings.Contains(strings.ToLower(firstString(toolCall, "type", "tool_type", "toolType")), "mcp") {
		return firstString(toolCall, "title", "name", "tool_name", "toolName")
	}
	server := explicitServer
	if server == "" {
		server = firstString(toolCall, "namespace")
	}
	tool := firstString(toolCall, "tool", "tool_name", "toolName")
	if server != "" && tool != "" {
		return server + "/" + tool
	}
	return firstString(toolCall, "title", "name", "tool_name", "toolName")
}

func claudePermissionOptions(event map[string]any) []commandbridge.PermissionOption {
	for _, key := range []string{"options", "permission_options", "permissionOptions", "choices"} {
		raw := sliceValue(event[key])
		if len(raw) == 0 {
			continue
		}
		options := make([]commandbridge.PermissionOption, 0, len(raw))
		for _, value := range raw {
			option := mapValue(value)
			if len(option) == 0 {
				continue
			}
			options = append(options, commandbridge.PermissionOption{
				OptionID: firstString(option, "optionId", "option_id", "id", "value"),
				Name:     firstString(option, "name", "label", "title"),
				Kind:     firstString(option, "kind", "type"),
			})
		}
		if len(options) > 0 {
			return options
		}
	}
	return nil
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
	case strings.Contains(name, "mcp"):
		return "mcp"
	case strings.Contains(name, "todo"):
		return "todo"
	case strings.Contains(name, "plan"):
		return "plan"
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
	case strings.Contains(name, "think"):
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

func sliceValue(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func firstValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}
