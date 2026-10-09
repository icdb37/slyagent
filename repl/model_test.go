package repl

import (
	"encoding/json"
	"strings"
	"testing"

	"slyagent/enum"
)

// TestMessage_RoundTrip 验证 Message 在 TextContent / BlocksContent 两种
// content 形态下，Marshal → Unmarshal 后能拿回等价的 Go 值。
// 顺带验证 role-based UnmarshalJSON：system/assistant/agent 走确定性分支，
// user 走"sniff content 首字节"分支。
func TestMessage_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   *Message
		// wire 形态里的 content 字段：应等于 marshal 出的对应片段
		wantText   string                         // 当 Content 是 TextContent 时，期望的 JSON content 值（带引号）
		wantArray  string                         // 当 Content 是 BlocksContent 时，期望的 JSON content 片段
		verifyBack func(t *testing.T, m *Message) // unmarshal 后断言
	}{
		{
			name: "system_text",
			in: &Message{
				Role:    enum.RoleSystem,
				Content: TextContent{Text: "you are a helper"},
			},
			wantText: `"you are a helper"`,
			verifyBack: func(t *testing.T, m *Message) {
				tc, ok := m.Content.(TextContent)
				if !ok {
					t.Fatalf("Content type = %T, want TextContent", m.Content)
				}
				if tc.Text != "you are a helper" {
					t.Fatalf("Text = %q, want %q", tc.Text, "you are a helper")
				}
			},
		},
		{
			name: "user_text",
			in: &Message{
				Role:    enum.RoleUser,
				Content: TextContent{Text: "hi"},
			},
			wantText: `"hi"`,
			verifyBack: func(t *testing.T, m *Message) {
				tc, ok := m.Content.(TextContent)
				if !ok {
					t.Fatalf("Content type = %T, want TextContent", m.Content)
				}
				if tc.Text != "hi" {
					t.Fatalf("Text = %q", tc.Text)
				}
			},
		},
		{
			name: "user_toolresult_blocks",
			in: &Message{
				Role:    enum.RoleUser,
				Content: BlocksContent{Blocks: []*ResContent{{Type: enum.ResContentTypeToolResult, ToolUseID: "t1", Result: "out"}}},
			},
			wantArray: `"type":"tool_result"`,
			verifyBack: func(t *testing.T, m *Message) {
				bc, ok := m.Content.(BlocksContent)
				if !ok {
					t.Fatalf("Content type = %T, want BlocksContent", m.Content)
				}
				if len(bc.Blocks) != 1 || bc.Blocks[0].ToolUseID != "t1" || bc.Blocks[0].Result != "out" {
					t.Fatalf("Blocks = %+v", bc.Blocks)
				}
			},
		},
		{
			name: "assistant_blocks",
			in: &Message{
				Role:    enum.RoleAssistant,
				Content: BlocksContent{Blocks: []*ResContent{{Type: enum.ResContentTypeText, Text: "hi"}, {Type: enum.ResContentTypeToolUse, ID: "u1", Name: "shell"}}},
			},
			wantArray: `"type":"text"`,
			verifyBack: func(t *testing.T, m *Message) {
				bc, ok := m.Content.(BlocksContent)
				if !ok {
					t.Fatalf("Content type = %T, want BlocksContent", m.Content)
				}
				if len(bc.Blocks) != 2 {
					t.Fatalf("Blocks length = %d, want 2", len(bc.Blocks))
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := json.Marshal(c.in)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			// wire 形态断言
			if c.wantText != "" && !strings.Contains(string(data), `"content":`+c.wantText) {
				t.Fatalf("wire 缺 content 字段或值不匹配：%s", data)
			}
			if c.wantArray != "" && !strings.Contains(string(data), c.wantArray) {
				t.Fatalf("wire 缺 array 片段：%s", data)
			}

			// 反向：unmarshal 拿回 Message，断言 Content 还原
			var out Message
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if out.Role != c.in.Role {
				t.Fatalf("Role = %q, want %q", out.Role, c.in.Role)
			}
			c.verifyBack(t, &out)
		})
	}
}

// TestMessage_UnmarshalUnknownRole 验证未知 role 触发 error，
// 而不是默默把 Content 留 nil。
func TestMessage_UnmarshalUnknownRole(t *testing.T) {
	data := []byte(`{"role":"bogus","content":"hi"}`)
	var m Message
	if err := json.Unmarshal(data, &m); err == nil {
		t.Fatal("未知 role 应返回 error")
	}
}

// TestMessageContent_Seal 验证 TextContent / BlocksContent 实现
// MessageContent；外部类型不能 implement 进来。
func TestMessageContent_Seal(t *testing.T) {
	var _ MessageContent = TextContent{Text: "x"}
	var _ MessageContent = BlocksContent{Blocks: nil}
	// 这条编译失败（如果取消注释）说明密封性破坏：
	// var _ MessageContent = "raw string"
}
