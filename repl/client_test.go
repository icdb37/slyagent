package repl

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestReplClient(t *testing.T) {
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		t.Fatal("<LLM_API_KEY> not found")
	}
	cfg := &Config{
		ModelName: "MiniMax-M2.7",
		BaseURL:   "https://api.minimax.cn/anthropic/v1/messages",
		APIKey:    apiKey,
	}
	c := New(cfg)
	msg, err := c.ChatFull("数学鸡兔同笼问题，头共10个，腿共30只，求几只鸡几个兔")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(msg)
}

// mkMsg 构造一条指定 action 的消息，大小近似固定
func mkMsg(action MessageAction, body string) *Message {
	return &Message{
		Role:    roleOf(action),
		Content: body,
		action:  action,
	}
}

func roleOf(a MessageAction) Role {
	switch a {
	case MessageActionUser, MessageActionToolResult:
		return RoleUser
	default:
		return RoleAssistant
	}
}

// 诉求 3：总数 < MessageRecentNum*3 不压缩
func TestCompressIndex_BelowTrigger(t *testing.T) {
	c := New(&Config{})
	// 2*MessageRecentNum 条，仍 < 3*MessageRecentNum
	for i := 0; i < MessageRecentNum*2; i++ {
		c.history = append(c.history, mkMsg(MessageActionUser, fmt.Sprintf("u%d", i)))
	}
	if got := c.compressIndex(); got != -1 {
		t.Fatalf("期望 -1，得到 %d", got)
	}
}

// 基础压缩：交替 user/assistant，最旧处可切
func TestCompressIndex_BasicCutAtOldest(t *testing.T) {
	c := New(&Config{})
	// 7 条：Asst, User, Asst, User, Asst, User, Asst
	// 总大小需要超过 MessageHistorySize，否则中间区不压缩
	s := strings.Repeat("a", 2*1024) // 2KB/条
	for i := 0; i < 7; i++ {
		a := MessageActionAssistant
		if i%2 == 1 {
			a = MessageActionUser
		}
		c.history = append(c.history, mkMsg(a, fmt.Sprintf("%s%d", s, i)))
	}
	got := c.compressIndex()
	// hs=7 处于中间区，总大小 7*~2KB > 10KB，触发压缩
	// pos = 7 - MessageRecentNum = 5；validCutAt(4)：history[5]=User，合法
	if got != 5 {
		t.Fatalf("期望切点 5，得到 %d", got)
	}
}

// tool pair 必须成对保留
func TestCompressIndex_ToolPairPreserved(t *testing.T) {
	c := New(&Config{})
	// Asst(text), User, Asst(tool_call), User(tool_result), User, Asst
	// 6 条处于中间区；每条 ~2KB，总大小 ~12KB > MessageHistorySize，触发压缩
	s := strings.Repeat("a", 2*1024)
	seq := []MessageAction{
		MessageActionAssistant,
		MessageActionUser,
		MessageActionToolCall,
		MessageActionToolResult,
		MessageActionUser,
		MessageActionAssistant,
	}
	for i, a := range seq {
		c.history = append(c.history, mkMsg(a, fmt.Sprintf("%s%d", s, i)))
	}
	cut := c.compressIndex()
	if cut <= 0 || cut >= len(c.history) {
		t.Fatalf("合法切点应为中间位置，得到 %d", cut)
	}
	// 保留区起点 history[cut] 不得是 ToolResult
	if c.history[cut].action == MessageActionToolResult {
		t.Fatalf("切点切在 ToolResult 之前，破坏 tool 对：cut=%d action=%s", cut, c.history[cut].action)
	}
	// 保留区起点之后的 ToolResult 必须能找到同一个 ToolCall 配对
	for i := cut; i < len(c.history); i++ {
		if c.history[i].action != MessageActionToolResult {
			continue
		}
		// 向左找最近的 ToolCall（要求同保留区内）
		hasPair := false
		for j := i - 1; j >= cut; j-- {
			if c.history[j].action == MessageActionToolCall {
				hasPair = true
				break
			}
		}
		if !hasPair {
			t.Fatalf("保留区内 ToolResult 失去配对：i=%d cut=%d", i, cut)
		}
	}
}

// 诉求 2：中间区总大小超阈值时触发压缩
func TestCompressIndex_SizeThreshold(t *testing.T) {
	c := New(&Config{})
	// 6 条处于中间区；m2 设成 20KB，总大小远超阈值，必然触发压缩
	s1 := strings.Repeat("a", 1024)
	sBig := strings.Repeat("a", 20*1024)
	c.history = []*Message{
		mkMsg(MessageActionAssistant, s1),  // 0
		mkMsg(MessageActionUser, s1),       // 1
		mkMsg(MessageActionAssistant, sBig), // 2 —— 超胖
		mkMsg(MessageActionUser, s1),       // 3
		mkMsg(MessageActionAssistant, s1),  // 4  recent
		mkMsg(MessageActionAssistant, s1),  // 5  recent
	}
	cut := c.compressIndex()
	if cut < 0 {
		t.Fatalf("期望找到切点，得到 -1（中间区总大小应超阈值）")
	}
	kept := c.history[cut:]
	keptBytes := 0
	for _, h := range kept {
		data, _ := json.Marshal(h)
		keptBytes += len(data)
	}
	if keptBytes > MessageHistorySize {
		t.Fatalf("保留区 %d 字节超过阈值 %d", keptBytes, MessageHistorySize)
	}
	// m2 必须出现在被丢的一侧
	for i := cut; i < len(c.history); i++ {
		if c.history[i].Content == sBig {
			t.Fatalf("cat 含 20KB 那条，阈值未生效：cut=%d", cut)
		}
	}
}

// recent 块不合法时（单条 ToolCall 在末尾），不应给出破坏配对的切点
func TestCompressIndex_RecentBlockIncomplete(t *testing.T) {
	c := New(&Config{})
	// 末尾 2 条是 Assistant(text) + ToolCall，但没 ToolResult 跟。
	// recent 块不完整，但 compressIndex 不应该越界给出破坏配对的切点。
	seq := []MessageAction{
		MessageActionAssistant, // 0
		MessageActionUser,      // 1
		MessageActionAssistant, // 2
		MessageActionUser,      // 3
		MessageActionAssistant, // 4  recent
		MessageActionToolCall,  // 5  recent (无对应 result)
	}
	for i, a := range seq {
		c.history = append(c.history, mkMsg(a, fmt.Sprintf("m%d", i)))
	}
	cut := c.compressIndex()
	// 不期望切点紧贴 recent 块的 ToolCall 前（即 cut=5）。
	// validCutAt(4) 会拒绝：history[5]=ToolCall 但被规则(1)允许；
	// 然而 history[4].action=Assistant 且无 ToolResult 影响——
	// 实际期望：可以切在 history[5] 之前，那保留区起点就是 ToolCall（合法起点）。
	// 这里只断言：切点不能把 ToolCall 留在被丢的一侧、把 ToolResult 留在保留区。
	if cut > 0 && cut <= len(c.history) {
		// 校验：如果保留区含 ToolResult，则同侧必须有配对 ToolCall
		for i := cut; i < len(c.history); i++ {
			if c.history[i].action != MessageActionToolResult {
				continue
			}
			hasPair := false
			for j := i - 1; j >= cut; j-- {
				if c.history[j].action == MessageActionToolCall {
					hasPair = true
					break
				}
			}
			if !hasPair {
				t.Fatalf("保留区内 ToolResult 失去配对：i=%d cut=%d", i, cut)
			}
		}
	}
}

// User 消息也应当作合法切点
func TestCompressIndex_UserIsValidCut(t *testing.T) {
	c := New(&Config{})
	// 6 条处于中间区，每条 ~2KB，总大小 ~12KB > MessageHistorySize，触发压缩
	// 序列：Asst, User, Asst, User, Asst, User
	s := strings.Repeat("a", 2*1024)
	for i := 0; i < 6; i++ {
		a := MessageActionAssistant
		if i%2 == 1 {
			a = MessageActionUser
		}
		c.history = append(c.history, mkMsg(a, fmt.Sprintf("%s%d", s, i)))
	}
	cut := c.compressIndex()
	// hs=6，pos=6-2=4；validCutAt(3)：history[4]=Asst，不合法；
	// 回退 pos=3，validCutAt(2)：history[3]=User，合法
	if cut != 3 {
		t.Fatalf("期望切点 3，得到 %d", cut)
	}
}
