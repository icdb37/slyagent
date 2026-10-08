package repl

import (
	"encoding/json"
	"strings"
)

// Cmd 与 EvtRepl 是 REPL 与外部（TUI/CLI/HTTP 前端）之间的契约。
//
// Cmd 由调用方通过 chan Cmd 投递，Client 消费后按状态机推进。
//
// EvtRepl 是统一的输出 envelope：所有"事件"都是某种 T 通过 DecodeEvtRepl
// 包装成的 *EvtRepl；接收方按 Kind 分发，用 EncodeEvent[T] 解出原值。
// 把"事件类型"从域类型下沉为调用方自决，避免 sum-type 接口的耦合。
//
// 示例：
//
//	env := DecodeEvtRepl(&EvtPayloadData{Data: "hi"}, UserEcho)
//	p, _ := EncodeEvent[EvtPayloadData](env)
//
// wire 字节流（Payload 是 JSON 字符串，内层再次转义）：
//
//	{"kind":"ASSISTANT_CHUNK","payload":"{\"Delta\":\"hello\"}"}

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
	SessionNew     EvtKind = "SESSION_NEW"
	SessionLoad    EvtKind = "SESSION_LOAD"
)

// ---------- Payload 类型 ----------
//
// 每个 Kind 对应一个具体 payload 类型，由调用方在 DecodeEvtRepl / EncodeEvent 处
// 显式标注。Kind 是协议标签，Payload 是载荷 schema；二者一一对应。
//
// 多个 Kind 可共用同一 Payload 类型（结构相同时），由 Kind 区分语义：
//   - EvtPayloadData{Data}：UserEcho / Compress / Status / Error / System / SessionNew / SessionLoad / AssistantStart / Clear / Quit / AssistantEnd
//   - EvtPayloadBusy / EvtPayloadAssistantChunk / EvtPayloadToolCall / EvtPayloadToolResult / EvtPayloadSessionList / EvtPayloadSessionLoad：各自专属

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

// SessionItemKind 是 SessionItem 的 wire 分类标签。
// 用 string 而非内部 enum，让 wire 与 UI 内部 itemKind 解耦；
// UI 端按 Kind 字面量映射到自己的渲染项。
type SessionItemKind string

const (
	SessionItemUser       SessionItemKind = "user"
	SessionItemAssistant  SessionItemKind = "assistant"
	SessionItemThinking   SessionItemKind = "thinking"
	SessionItemToolCall   SessionItemKind = "tool_call"
	SessionItemToolResult SessionItemKind = "tool_result"
	SessionItemSystem     SessionItemKind = "system"
)

// SessionItem 是 session 回放中的最小渲染单元。
// 一条 Message 会被拆成多条 Item（assistant 含 text + thinking + tool_use 时
// 各成一条）；TUI 端按 Kind 直接渲染，无需再做 role-based 解析。
type SessionItem struct {
	Kind   SessionItemKind `json:"kind"`
	Text   string          `json:"text,omitempty"`   // user / assistant / thinking / system 的内容
	Name   string          `json:"name,omitempty"`   // tool_call 工具名
	Input  string          `json:"input,omitempty"`  // tool_call 参数（已格式化）
	Result string          `json:"result,omitempty"` // tool_result 输出
	Err    string          `json:"error,omitempty"`  // tool_result 错误
}

// ToSessionItems 把 history []*Message 转换为 UI 回放用的 []SessionItem。
// 转换规则：
//   - RoleSystem / RoleAgent → 一条 SessionItem{Kind: SessionItemSystem, Text}
//   - RoleUser, TextContent   → 一条 SessionItem{Kind: SessionItemUser}
//   - RoleUser, BlocksContent（tool_result bundle）→ 每个 tool_result 块一条
//   - RoleAssistant, BlocksContent → 按块类型拆：text / thinking / tool_use 各成一条
func ToSessionItems(msgs []*Message) []*SessionItem {
	var items []*SessionItem
	for _, m := range msgs {
		switch m.Role {
		case RoleSystem, RoleAgent:
			items = append(items, &SessionItem{
				Kind: SessionItemSystem,
				Text: strings.Join(m.Content.GetTexts(), "\n"),
			})
		case RoleUser:
			switch mc := m.Content.(type) {
			case TextContent:
				items = append(items, &SessionItem{Kind: SessionItemUser, Text: mc.Text})
			case BlocksContent:
				for _, b := range mc.Blocks {
					if b.Type != ResContentTypeToolResult {
						continue
					}
					items = append(items, &SessionItem{
						Kind:   SessionItemToolResult,
						Result: b.Result,
					})
				}
			}
		case RoleAssistant:
			bc, ok := m.Content.(BlocksContent)
			if !ok {
				continue
			}
			for _, b := range bc.Blocks {
				switch b.Type {
				case ResContentTypeText:
					items = append(items, &SessionItem{Kind: SessionItemAssistant, Text: b.Text})
				case ResContentTypeThinking:
					items = append(items, &SessionItem{Kind: SessionItemThinking, Text: b.Thinking})
				case ResContentTypeToolUse:
					items = append(items, &SessionItem{
						Kind:  SessionItemToolCall,
						Name:  b.Name,
						Input: string(b.Input),
					})
				}
			}
		}
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
// json.Marshal 失败时 Payload 留空（罕见：T 含有不可序列化字段）。
// 入参取 *T：避免结构体拷贝、保证调用方传地址语义统一（与 EncodeEvent 出 *T 对称）。
func DecodeEvtRepl[T any](param *T, kind EvtKind) *EvtRepl {
	e := &EvtRepl{Kind: kind}
	data, _ := json.Marshal(param)
	e.Payload = string(data)
	return e
}

// errString 把 error 折叠为 string：nil → ""，否则 err.Error()。
// 当某 Kind 改用 EvtPayloadData{Data} 表达"可空错误"时（如 AssistantEnd），
// emit 端用本函数做转换；接收端以 Data == "" 区分成功 / 失败。
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
