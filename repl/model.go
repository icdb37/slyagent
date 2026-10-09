package repl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode"

	"slyagent/enum"
)

type ReqContent string
type ResContent struct {
	Type enum.ResContentType `json:"type"`

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

type MessageAction string

const (
	MessageActionUser       MessageAction = "user"
	MessageActionToolCall   MessageAction = "tool_call"
	MessageActionToolResult MessageAction = "tool_result"
	MessageActionAssistant  MessageAction = "assistant"
)

// MessageContent 是 Message.Content 的封闭接口：只接受 TextContent / BlocksContent。
// 静态保证 Content 字段不会被装入任意值，type switch 配合 isMessageContent
// 哨兵方法形成编译期可枚举的"和类型"。
//
// 两个变体各自实现 MarshalJSON，使 json.Marshal(Message) 出的 wire 形态与
// Anthropic 协议一致：
//   - TextContent  → "content":"<Text>"            （system / user 纯文本）
//   - BlocksContent → "content":[{...},{...}]      （assistant / user 工具结果）
type MessageContent interface {
	isMessageContent()
	GetTexts() []string
}

type TextContent struct{ Text string }

func (TextContent) isMessageContent() {}

// GetTexts 返回此 content 形态包含的纯文本片段。
// TextContent 自身是一段文本；BlocksContent 汇总各 text 块。
// 统一接口便于压缩、显示、history 大小估算等场景无需关心具体形态。
func (t TextContent) GetTexts() []string {
	return []string{t.Text}
}

// MarshalJSON 把 TextContent 序列化为 JSON 字符串，与 wire 形态对齐。
func (t TextContent) MarshalJSON() ([]byte, error) { return json.Marshal(t.Text) }

type BlocksContent struct{ Blocks []*ResContent }

func (BlocksContent) isMessageContent() {}

func (b BlocksContent) GetTexts() []string {
	texts := make([]string, 0, len(b.Blocks))
	for _, c := range b.Blocks {
		if c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	return texts
}

// MarshalJSON 把 BlocksContent 序列化为 JSON 数组。
func (b BlocksContent) MarshalJSON() ([]byte, error) { return json.Marshal(b.Blocks) }

type Message struct {
	Role    enum.Role         `json:"role"`
	Content MessageContent    `json:"content"`
	size    int                // 消息大小
	action  MessageAction      // 操作
}

// UnmarshalJSON 从 wire 形态恢复 Message：以 role 为主判别决定 Content 形态。
//
// 协议事实：
//   - system / agent  → TextContent
//   - assistant       → BlocksContent
//   - user            → 二义（纯文本 或 tool_result 块数组），sniff content 首字节
//
// 用 role 决定 content 类型而不是纯 sniff，是因为大多数 role 的 content 形态
// 是确定的；只有 user 兼容两种 wire 形态。
func (m *Message) UnmarshalJSON(data []byte) error {
	var probe struct {
		Role    enum.Role          `json:"role"`
		Content json.RawMessage   `json:"content"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	m.Role = probe.Role
	switch probe.Role {
	case enum.RoleSystem, enum.RoleAgent:
		var t TextContent
		if err := json.Unmarshal(probe.Content, &t.Text); err != nil {
			return err
		}
		m.Content = t
	case enum.RoleAssistant:
		var b BlocksContent
		if err := json.Unmarshal(probe.Content, &b.Blocks); err != nil {
			return err
		}
		m.Content = b
	case enum.RoleUser:
		// user 二义：文本（首字节 "）或块数组（首字节 [）
		head := bytes.TrimLeftFunc(probe.Content, unicode.IsSpace)
		if len(head) > 0 && head[0] == '"' {
			var t TextContent
			if err := json.Unmarshal(probe.Content, &t.Text); err != nil {
				return err
			}
			m.Content = t
		} else {
			var b BlocksContent
			if err := json.Unmarshal(probe.Content, &b.Blocks); err != nil {
				return err
			}
			m.Content = b
		}
	default:
		return fmt.Errorf("Message: 未知 role %q", probe.Role)
	}
	return nil
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
	Role    enum.Role      `json:"role"`
	Model   string         `json:"model"`
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
		Type enum.ResContentType `json:"type,omitempty"`
		ID   string         `json:"id,omitempty"`
		Name string         `json:"name,omitempty"`
	} `json:"content_block"`
}

type ResContentDelta struct {
	Type  ResEventType `json:"type,omitempty"`
	Index int          `json:"index"`
	Delta struct {
		Type        enum.ResContentDeltaType `json:"type,omitempty"`
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

// 流式响应中 message_delta 事件：刷新 output_tokens、stop_reason
type ResMessageDelta struct {
	Type  string `json:"type,omitempty"`
	Delta struct {
		StopReason   string `json:"stop_reason,omitempty"`
		StopSequence string `json:"stop_sequence,omitempty"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}
