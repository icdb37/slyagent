package repl

import "encoding/json"

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

type ReqContent string
type ResContent struct {
	Type ResContentType `json:"type"`

	// text 块
	Text string `json:"text,omitempty"`
	// thinking 块
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	// tool_use 块：模型请求调用工具
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result 块：工具执行结果（Anthropic 协议挂在 user 消息里）
	// content 字段必须是字符串或内容块数组；不能是裸 JSON 对象
	ToolUseID string `json:"tool_use_id,omitempty"`
	Result    string `json:"content,omitempty"`
}

type Message struct {
	Role    Role `json:"role"`
	Content any  `json:"content"`
}

type XllmThinking struct {
	// disabled（MiniMax-M3 可关闭）、adaptive
	Type string `json:"type"`
}
type XllmOutput struct {
	// low、medium、high、xhigh、max；省略时默认为 max。
	Effort string `json:"effort"`
}
type XllmTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *struct {
		Type string `json:"type"`
	} `json:"cache_control,omitempty"`
}

// 请求模型参数
type ReqXllm struct {
	Messages     []*Message    `json:"messages"`
	Model        string        `json:"model"`
	Stream       bool          `json:"stream,omitempty"`
	MaxTokens    int           `json:"max_tokens"`
	Temperature  float64       `json:"temperature,omitempty"`
	Thinking     *XllmThinking `json:"thinking,omitempty"`
	OutputConfig *XllmOutput   `json:"output_config,omitempty"`
	Tools        []*XllmTool   `json:"tools,omitempty"`
}

// 请求模型应答
type ResXllm struct {
	ID      string        `json:"id"`
	Type    string        `json:"type"`
	Role    Role          `json:"role"`
	Model   string        `json:"model"`
	Content []*ResContent `json:"content"`
	Usage   struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
	StopReason string `json:"stop_reason"`
}

type ResEventType string

const (
	ResEventTypeMessageStart      = "message_start"
	ResEventTypePing              = "ping"
	ResEventTypeContentBlockStart = "content_block_start"
	ResEventTypeContentBlockDelta = "content_block_delta"
	ResEventTypeContentBlockStop  = "content_block_stop"
	ResEventTypeMessageStop       = "message_stop"
)

type ResContentBlock struct {
	Type         ResEventType `json:"type,omitempty"`
	Index        int          `json:"index"`
	ContentBlock struct {
		Type ResContentType `json:"type,omitempty"`
		ID   string         `json:"id,omitempty"`
		Name string         `json:"name,omitempty"`
	} `json:"content_block"`
}

type ResContentDelta struct {
	Type  ResEventType `json:"type,omitempty"`
	Index int          `json:"index"`
	Delta struct {
		Type        ResContentDeltaType `json:"type,omitempty"`
		Thinking    string              `json:"thinking,omitempty"`
		Text        string              `json:"text,omitempty"`
		Signature   string              `json:"signature,omitempty"`
		PartialJson string              `json:"partial_json,omitempty"`
	} `json:"delta"`
}

type ResEventMessage struct {
	Type    string   `json:"type,omitempty"`
	Message *ResXllm `json:"message,omitempty"`
}
