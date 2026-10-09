package repl

import (
	"encoding/json"
	"strings"
	"testing"

	"slyagent/enum"
)

// decodeTest 把 any 形态的 param 分派到对应的 DecodeEvtRepl 重载。
func decodeTest(param any, kind EvtKind) *EvtRepl {
	switch v := param.(type) {
	case *EvtPayloadData:
		return DecodeEvtRepl(v, kind)
	case *EvtPayloadBusy:
		return DecodeEvtRepl(v, kind)
	case *EvtPayloadAssistantChunk:
		return DecodeEvtRepl(v, kind)
	case *EvtPayloadToolCall:
		return DecodeEvtRepl(v, kind)
	case *EvtPayloadToolResult:
		return DecodeEvtRepl(v, kind)
	case *EvtPayloadSessionList:
		return DecodeEvtRepl(v, kind)
	}
	return nil
}

// TestDecodeEvtRepl_AllKinds 给每种 Kind 配一个示例 payload，验证 DecodeEvtRepl
// 产出合法 envelope：Kind 不空、Payload 是合法 JSON。
func TestDecodeEvtRepl_AllKinds(t *testing.T) {
	cases := []struct {
		name   string
		kind   EvtKind
		param  any // *EvtPayloadXxx 之一
		hasPld bool // 期望 Payload 非空
	}{
		{"user_echo", UserEcho, &EvtPayloadData{Data: "hi"}, true},
		{"busy", Busy, &EvtPayloadBusy{Busy: true}, true},
		{"assistant_start", AssistantStart, &EvtPayloadData{}, true},
		{"assistant_chunk", AssistantChunk, &EvtPayloadAssistantChunk{Delta: "tok"}, true},
		{"assistant_end", AssistantEnd, &EvtPayloadData{}, true},
		{"tool_call", ToolCall, &EvtPayloadToolCall{Name: "shell", Input: `{"cmd":"ls"}`}, true},
		{"tool_result", ToolResult, &EvtPayloadToolResult{Name: "shell", Result: "out"}, true},
		{"status", Status, &EvtPayloadData{Data: "ok"}, true},
		{"compress", Compress, &EvtPayloadData{Data: "3"}, true},
		{"error", Error, &EvtPayloadData{Data: "boom"}, true},
		{"system", System, &EvtPayloadData{Data: "info"}, true},
		{"clear", Clear, &EvtPayloadData{}, true},
		{"quit", Quit, &EvtPayloadData{}, true},
		{"session_list", SessionList, &EvtPayloadSessionList{IDs: []string{"a", "b"}}, true},
		{"session_load", SessionLoad, &EvtPayloadData{Data: "[已加载: x]"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := decodeTest(c.param, c.kind)
			if env == nil {
				t.Fatal("nil envelope")
			}
			if env.Kind != c.kind {
				t.Fatalf("Kind = %q, want %q", env.Kind, c.kind)
			}
			if c.hasPld && env.Payload == "" {
				t.Fatalf("Payload is empty")
			}
			// payload 必须是合法 JSON
			var probe any
			if err := json.Unmarshal([]byte(env.Payload), &probe); err != nil {
				t.Fatalf("Payload not valid JSON: %v (raw=%q)", err, env.Payload)
			}
		})
	}
}

// TestEventWire_RoundTrip 验证 wire 字节流可往返：
//
//	DecodeEvtRepl → Marshal → Unmarshal EvtRepl → EncodeEvent[T] → 拿回原值
func TestEventWire_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		kind EvtKind
		env  any // *EvtPayloadXxx 之一
	}{
		{"busy_true", Busy, &EvtPayloadBusy{Busy: true}},
		{"busy_false", Busy, &EvtPayloadBusy{Busy: false}},
		{"chunk", AssistantChunk, &EvtPayloadAssistantChunk{Delta: "hello"}},
		{"session_list", SessionList, &EvtPayloadSessionList{IDs: []string{"x", "y"}}},
		{"message_user", UserEcho, &EvtPayloadData{Data: "hi"}},
		{"message_error", Error, &EvtPayloadData{Data: "boom"}},
		{"empty_clear", Clear, &EvtPayloadData{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env1 := decodeTest(c.env, c.kind)
			data, err := json.Marshal(env1)
			if err != nil {
				t.Fatalf("Marshal EvtRepl: %v", err)
			}

			var env2 EvtRepl
			if err := json.Unmarshal(data, &env2); err != nil {
				t.Fatalf("Unmarshal EvtRepl: %v", err)
			}
			if env2.Kind != env1.Kind {
				t.Fatalf("Kind round-trip: %q vs %q", env2.Kind, env1.Kind)
			}
			if env2.Payload != env1.Payload {
				t.Fatalf("Payload round-trip: %q vs %q", env2.Payload, env1.Payload)
			}

			// 解码到原类型
			switch v := c.env.(type) {
			case *EvtPayloadBusy:
				p, err := EncodeEvent[EvtPayloadBusy](&env2)
				if err != nil {
					t.Fatalf("EncodeEvent[EvtPayloadBusy]: %v", err)
				}
				if p.Busy != v.Busy {
					t.Fatalf("Busy = %+v, want %+v", p, v)
				}
			case *EvtPayloadAssistantChunk:
				p, err := EncodeEvent[EvtPayloadAssistantChunk](&env2)
				if err != nil {
					t.Fatalf("EncodeEvent[EvtPayloadAssistantChunk]: %v", err)
				}
				if p.Delta != v.Delta {
					t.Fatalf("Delta = %+v, want %+v", p, v)
				}
			case *EvtPayloadSessionList:
				p, err := EncodeEvent[EvtPayloadSessionList](&env2)
				if err != nil {
					t.Fatalf("EncodeEvent[EvtPayloadSessionList]: %v", err)
				}
				if len(p.IDs) != len(v.IDs) {
					t.Fatalf("IDs length: %d vs %d", len(p.IDs), len(v.IDs))
				}
			case *EvtPayloadData:
				p, err := EncodeEvent[EvtPayloadData](&env2)
				if err != nil {
					t.Fatalf("EncodeEvent[EvtPayloadData]: %v", err)
				}
				if p.Data != v.Data {
					t.Fatalf("Data = %+v, want %+v", p, v)
				}
			}
		})
	}
}

// TestEncodeEvent_BadPayload Payload 不是合法 JSON 时返回 error；返回的 *T 非 nil，
// 错误仅靠 err 表达。
func TestEncodeEvent_BadPayload(t *testing.T) {
	env := &EvtRepl{Kind: Busy, Payload: "not json"}
	p, err := EncodeEvent[EvtPayloadBusy](env)
	if err == nil {
		t.Fatal("expected error for bad payload")
	}
	if p == nil {
		t.Fatal("EncodeEvent 总是返回 *T，错误时仅靠 err 表达")
	}
	if p.Busy != false {
		t.Fatalf("error path 应是零值，得到 Busy=%v", p.Busy)
	}
}

// TestEncodeEvent_EmptyPayload 空 payload 让 json.Unmarshal 报错。
func TestEncodeEvent_EmptyPayload(t *testing.T) {
	env := &EvtRepl{Kind: Clear, Payload: ""}
	_, err := EncodeEvent[EvtPayloadData](env)
	if err == nil {
		t.Fatal("空 payload 应让 json.Unmarshal 报错")
	}
}

// TestDecodeEvtRepl_NonNilError 含 error 字段的 payload 至少能 marshal 成 JSON。
func TestDecodeEvtRepl_NonNilError(t *testing.T) {
	ev := &EvtPayloadData{Data: "boom"}
	env := DecodeEvtRepl(ev, AssistantEnd)
	if env.Kind != AssistantEnd {
		t.Fatalf("Kind = %q, want %q", env.Kind, AssistantEnd)
	}
	if env.Payload == "" {
		t.Fatal("Payload should be non-empty")
	}
}

// TestEvtRepl_JSONTags 锁定 wire 字段名（kind / payload）与 Kind 字符串值大写。
func TestEvtRepl_JSONTags(t *testing.T) {
	env := DecodeEvtRepl(&EvtPayloadData{Data: "hi"}, UserEcho)
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"kind":`) {
		t.Fatalf("wire 应含 \"kind\" 字段：%s", data)
	}
	if !strings.Contains(string(data), `"payload":`) {
		t.Fatalf("wire 应含 \"payload\" 字段：%s", data)
	}
	// EvtKind 序列化为大写字符串值
	if !strings.Contains(string(data), `"USER_ECHO"`) {
		t.Fatalf("wire 应含 Kind 字符串值 USER_ECHO：%s", data)
	}
}

// TestToSessionItems 验证 []*Message → []SessionItem 的映射规则：
// TextContent 包装为单元素 Contents，BlocksContent 复用 Blocks，Role 字段保留。
func TestToSessionItems(t *testing.T) {
	toolInput := json.RawMessage(`{"cmd":"ls"}`)
	msgs := []*Message{
		{Role: enum.RoleUser, Content: TextContent{Text: "hi"}},
		{Role: enum.RoleAssistant, Content: BlocksContent{Blocks: []*ResContent{
			{Type: enum.ResContentTypeThinking, Thinking: "思考中..."},
			{Type: enum.ResContentTypeText, Text: "让我查一下"},
			{Type: enum.ResContentTypeToolUse, ID: "u1", Name: "shell", Input: toolInput},
		}}},
		{Role: enum.RoleUser, Content: BlocksContent{Blocks: []*ResContent{
			{Type: enum.ResContentTypeToolResult, ToolUseID: "u1", Result: "file.txt\nfile2.txt"},
		}}},
		{Role: enum.RoleSystem, Content: TextContent{Text: "you are helpful"}},
	}
	items := ToSessionItems(msgs)

	// 期望：4 条消息 → 4 条 SessionItem（一对一）
	if len(items) != 4 {
		t.Fatalf("len(items) = %d, want 4 (got %+v)", len(items), items)
	}

	// item 0: user 文本包装为单 text 块
	if items[0].Role != enum.RoleUser {
		t.Errorf("items[0].Role = %q, want %q", items[0].Role, enum.RoleUser)
	}
	if got := items[0].Contents; len(got) != 1 ||
		got[0].Type != enum.ResContentTypeText || got[0].Text != "hi" {
		t.Errorf("items[0].Contents = %+v", got)
	}

	// item 1: assistant 三个块按原顺序保留
	if items[1].Role != enum.RoleAssistant {
		t.Errorf("items[1].Role = %q, want %q", items[1].Role, enum.RoleAssistant)
	}
	if got := items[1].Contents; len(got) != 3 ||
		got[0].Type != enum.ResContentTypeThinking || got[0].Thinking != "思考中..." ||
		got[1].Type != enum.ResContentTypeText || got[1].Text != "让我查一下" ||
		got[2].Type != enum.ResContentTypeToolUse || got[2].Name != "shell" || string(got[2].Input) != `{"cmd":"ls"}` {
		t.Errorf("items[1].Contents = %+v", got)
	}

	// item 2: user tool_result 块数组
	if items[2].Role != enum.RoleUser {
		t.Errorf("items[2].Role = %q, want %q", items[2].Role, enum.RoleUser)
	}
	if got := items[2].Contents; len(got) != 1 ||
		got[0].Type != enum.ResContentTypeToolResult || got[0].Result != "file.txt\nfile2.txt" {
		t.Errorf("items[2].Contents = %+v", got)
	}

	// item 3: system 文本包装
	if items[3].Role != enum.RoleSystem {
		t.Errorf("items[3].Role = %q, want %q", items[3].Role, enum.RoleSystem)
	}
	if got := items[3].Contents; len(got) != 1 ||
		got[0].Type != enum.ResContentTypeText || got[0].Text != "you are helpful" {
		t.Errorf("items[3].Contents = %+v", got)
	}
}

// TestToSessionItems_NilElement 回归保护：msgs 里出现 nil 元素（磁盘数据
// 损坏或 null 反序列化）时，ToSessionItems 必须跳过而不是 nil 解引用崩溃。
func TestToSessionItems_NilElement(t *testing.T) {
	msgs := []*Message{
		nil,
		{Role: enum.RoleUser, Content: TextContent{Text: "hi"}},
		nil,
	}
	items := ToSessionItems(msgs)
	if len(items) != 1 {
		t.Fatalf("期望跳过 nil 后剩 1 条，得到 %d", len(items))
	}
	if items[0].Role != enum.RoleUser {
		t.Errorf("items[0].Role = %q, want %q", items[0].Role, enum.RoleUser)
	}
}
