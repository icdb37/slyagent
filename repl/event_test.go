package repl

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeTest 是 type-switch 版的 DecodeEvtRepl：表驱动测试里 param 是 any，
// 而 DecodeEvtRepl 需要具体 *T，故通过此函数分派。该函数只用于测试。
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
// 正确产生 envelope（Kind 不空、Payload 是合法 JSON）。Kind → payload schema 由
// 调用方自定，测试只保证"wire 形态合法"。
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
		{"session_new", SessionNew, &EvtPayloadData{Data: "new"}, true},
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

// TestEncodeEvent_BadPayload Payload 不是合法 JSON 时返回 error。
// 实现里用 new(T) 预分配，所以返回的 *T 仍非 nil（带零值），错误只走 err。
func TestEncodeEvent_BadPayload(t *testing.T) {
	env := &EvtRepl{Kind: Busy, Payload: "not json"}
	p, err := EncodeEvent[EvtPayloadBusy](env)
	if err == nil {
		t.Fatal("expected error for bad payload")
	}
	if p == nil {
		t.Fatal("EncodeEvent 总是返回 *T（new(T) 预分配），错误时仅靠 err 表达")
	}
	if p.Busy != false {
		t.Fatalf("error path 应是零值，得到 Busy=%v", p.Busy)
	}
}

// TestEncodeEvent_EmptyPayload 空 payload 让 json.Unmarshal 报错——这是
// json.Unmarshal 本身的行为，本 API 不做特殊处理。
func TestEncodeEvent_EmptyPayload(t *testing.T) {
	env := &EvtRepl{Kind: Clear, Payload: ""}
	_, err := EncodeEvent[EvtPayloadData](env)
	if err == nil {
		t.Fatal("空 payload 应让 json.Unmarshal 报错")
	}
}

// TestDecodeEvtRepl_NonNilError 验证含 error 字段的 payload 至少能 marshal 成 JSON。
// encoding/json 当前把 error 接口按底层 struct 序列化（errors.errorString → {"s":"..."}）。
// 这里只保证"不 panic、不报错"；若要 wire 形态可读，需为含 error 的类型加 MarshalJSON。
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

// TestEvtRepl_JSONTags 保证 wire 字段名稳定：kind / payload（不是大写）。
// 改 tag 是协议级破坏，必须显式 review。
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
	// 反过来：wire 字符串值必须是大写（EvtKind 自身 string 化时仍保留大写）
	if !strings.Contains(string(data), `"USER_ECHO"`) {
		t.Fatalf("wire 应含 Kind 字符串值 USER_ECHO：%s", data)
	}
}

// TestToSessionItems 验证 []*Message → []SessionItem 的拆分。
//
// 关键 case：assistant 含 text + thinking + tool_use 三种块时，应分别生成
// SessionItemAssistant / SessionItemThinking / SessionItemToolCall 三条；
// user 的 tool_result 块数组也应拆成多条 SessionItemToolResult。
func TestToSessionItems(t *testing.T) {
	toolInput := json.RawMessage(`{"cmd":"ls"}`)
	msgs := []*Message{
		{Role: RoleUser, Content: TextContent{Text: "hi"}},
		{Role: RoleAssistant, Content: BlocksContent{Blocks: []*ResContent{
			{Type: ResContentTypeThinking, Thinking: "思考中..."},
			{Type: ResContentTypeText, Text: "让我查一下"},
			{Type: ResContentTypeToolUse, ID: "u1", Name: "shell", Input: toolInput},
		}}},
		{Role: RoleUser, Content: BlocksContent{Blocks: []*ResContent{
			{Type: ResContentTypeToolResult, ToolUseID: "u1", Result: "file.txt\nfile2.txt"},
		}}},
		{Role: RoleSystem, Content: TextContent{Text: "you are helpful"}},
	}
	items := ToSessionItems(msgs)

	// 期望：user → assistant(thinking + text + tool_call) → user(tool_result) → system
	if len(items) != 6 {
		t.Fatalf("len(items) = %d, want 6 (got %+v)", len(items), items)
	}
	want := []SessionItem{
		{Kind: SessionItemUser, Text: "hi"},
		{Kind: SessionItemThinking, Text: "思考中..."},
		{Kind: SessionItemAssistant, Text: "让我查一下"},
		{Kind: SessionItemToolCall, Name: "shell", Input: `{"cmd":"ls"}`},
		{Kind: SessionItemToolResult, Result: "file.txt\nfile2.txt"},
		{Kind: SessionItemSystem, Text: "you are helpful"},
	}
	for i, w := range want {
		if items[i].Kind != w.Kind {
			t.Errorf("items[%d].Kind = %q, want %q", i, items[i].Kind, w.Kind)
		}
		if items[i].Text != w.Text {
			t.Errorf("items[%d].Text = %q, want %q", i, items[i].Text, w.Text)
		}
		if items[i].Name != w.Name {
			t.Errorf("items[%d].Name = %q, want %q", i, items[i].Name, w.Name)
		}
		if items[i].Input != w.Input {
			t.Errorf("items[%d].Input = %q, want %q", i, items[i].Input, w.Input)
		}
		if items[i].Result != w.Result {
			t.Errorf("items[%d].Result = %q, want %q", i, items[i].Result, w.Result)
		}
	}
}
