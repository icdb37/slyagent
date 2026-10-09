package repl

import (
	"encoding/json"

	"slyagent/enum"
)

// Cmd 由调用方通过 chan Cmd 投递，Client 消费后推进对话。
//
// EvtRepl 是统一的输出 envelope：所有"事件"都是某种 T 通过 DecodeEvtRepl
// 包装成的 *EvtRepl；接收方按 Kind 分发，用 EncodeEvent[T] 解出原值。
//
// 事件类型由调用方自决（Kind 是协议标签，Payload 是载荷 schema），避免
// sum-type 接口的耦合。

// ---------- 命令（调用方 → REPL）----------

type CmdKind string

const (
	CmdAsk      CmdKind = "ask"      // 普通问答，附 Msg
	CmdQuit     CmdKind = "quit"     // 退出
	CmdClear    CmdKind = "clear"    // 清空历史
	CmdCompress CmdKind = "compress" // 手动压缩
	CmdSession  CmdKind = "session"  // 会话
)

type Cmd struct {
	Kind CmdKind
	Msg  string // 仅 CmdAsk 使用
}

// ---------- 事件 envelope ----------

type EvtKind string

const (
	UserEcho       EvtKind = "USER_ECHO"
	Busy           EvtKind = "BUSY"
	AssistantStart EvtKind = "ASSISTANT_START"
	AssistantChunk EvtKind = "ASSISTANT_CHUNK"
	AssistantEnd   EvtKind = "ASSISTANT_END"
	ToolCall       EvtKind = "TOOL_CALL"
	ToolResult     EvtKind = "TOOL_RESULT"
	Status         EvtKind = "STATUS"
	Compress       EvtKind = "COMPRESS"
	Error          EvtKind = "ERROR"
	System         EvtKind = "SYSTEM"
	Clear          EvtKind = "CLEAR"
	Quit           EvtKind = "QUIT"
	SessionList    EvtKind = "SESSION_LIST"
	SessionLoad    EvtKind = "SESSION_LOAD"
)

// ---------- Payload 类型 ----------
//
// Kind → Payload 一一对应；多个 Kind 可共用同一 Payload 类型，由 Kind 区分语义。
// EvtPayloadData 是通用 string payload；其余类型各对应专属 Kind。

type EvtPayloadData struct {
	Data string `json:"data,omitempty"`
}
type EvtPayloadBusy struct {
	Busy bool `json:"busy,omitempty"`
}
type EvtPayloadAssistantChunk struct {
	Delta string `json:"delta,omitempty"`
}
type EvtPayloadToolCall struct {
	Name  string `json:"name,omitempty"`
	Input string `json:"input,omitempty"`
}
type EvtPayloadToolResult struct {
	Name   string `json:"name,omitempty"`
	Result string `json:"result,omitempty"`
	Err    string `json:"error,omitempty"`
}
type EvtPayloadSessionList struct {
	IDs []string `json:"ids,omitempty"`
}

// SessionItem 是 session 回放的最小渲染单元：一条 Message 映射到一个
// SessionItem，由 TUI 按 Role + Contents 自行渲染。
type SessionItem struct {
	Role     enum.Role     `json:"role"`
	Contents []*ResContent `json:"contents"`
}

// ToSessionItems 把 history []*Message 转换为 UI 回放用的 []SessionItem：
// TextContent 包装为单 text 块，BlocksContent 直接复用其 Blocks。
// msgs 里若有 nil 元素（磁盘数据损坏或极端竞态），跳过以避免 nil 解引用崩溃。
func ToSessionItems(msgs []*Message) []*SessionItem {
	items := make([]*SessionItem, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		item := &SessionItem{Role: m.Role}
		switch mc := m.Content.(type) {
		case TextContent:
			item.Contents = []*ResContent{{Type: enum.ResContentTypeText, Text: mc.Text}}
		case BlocksContent:
			item.Contents = mc.Blocks
		}
		items = append(items, item)
	}
	return items
}

type EvtPayloadSessionLoad struct {
	ID    string         `json:"id,omitempty"`
	Items []*SessionItem `json:"items,omitempty"`
}

// EvtRepl 是统一的传输 envelope。Payload 是 JSON 字符串（任意 T 的 JSON 序列化结果）。
type EvtRepl struct {
	Kind    EvtKind `json:"kind"`
	Payload string  `json:"payload,omitempty"`
}

// EncodeEvent 把 EvtRepl.Payload（JSON 字符串）反序列化为类型 T，返回 *T。
func EncodeEvent[T any](e *EvtRepl) (*T, error) {
	param := new(T)
	err := json.Unmarshal([]byte(e.Payload), param)
	return param, err
}

// DecodeEvtRepl 把任意类型 param 序列化为 JSON 字符串，包成 EvtRepl。
// 入参取 *T 与 EncodeEvent 出 *T 对称，避免结构体拷贝。
func DecodeEvtRepl[T any](param *T, kind EvtKind) *EvtRepl {
	e := &EvtRepl{Kind: kind}
	data, _ := json.Marshal(param)
	e.Payload = string(data)
	return e
}

// errString 把 error 折叠为 string：nil → ""，否则 err.Error()。
// 用于把可空错误装进 EvtPayloadData{Data}。
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// EvtUserAuthq 是工具授权请求：Prompt 描述动作，Reply 是单次回复通道。
type EvtUserAuthq struct {
	Prompt string
	Reply  chan bool
}
