package enum

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleAgent     Role = "agent"
)

type ResContentType string

const (
	ResContentTypeText       ResContentType = "text"
	ResContentTypeThinking   ResContentType = "thinking"
	ResContentTypeToolUse    ResContentType = "tool_use"
	ResContentTypeToolResult ResContentType = "tool_result"
)

type ResContentDeltaType string

const (
	ResContentDeltaTypeText      ResContentDeltaType = "text_delta"
	ResContentDeltaTypeSignature ResContentDeltaType = "signature_delta"
	ResContentDeltaTypeThinking  ResContentDeltaType = "thinking_delta"
	ResContentDeltaTypeInputJson ResContentDeltaType = "input_json_delta"
)
