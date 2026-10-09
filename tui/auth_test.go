package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"slyagent/repl"
)

// TestAuthFlow 验证 *repl.EvtUserAuthq 通过 c.out 投递能被 Update 接收、
// m.authReq 被设置；handleKey 写回 Reply 后工具侧能解除阻塞。
func TestAuthFlow(t *testing.T) {
	m := &model{
		busy: true,
	}

	// 模拟 forwardEvents 把 *repl.EvtUserAuthq 投递到 model 的 Update
	replyCh := make(chan bool, 1)
	req := &repl.EvtUserAuthq{Prompt: "test", Reply: replyCh}

	updatedModel, _ := m.Update(req)
	m = updatedModel.(*model)

	if m.authReq == nil {
		t.Fatal("Update 收到 *repl.EvtUserAuthq 后 m.authReq 应非 nil")
	}
	if m.authReq.Prompt != "test" {
		t.Fatalf("Prompt 丢失: %q", m.authReq.Prompt)
	}

	// 模拟用户按 y：handleKey 应写回 true
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}
	updatedModel, _ = m.Update(keyMsg)
	m = updatedModel.(*model)

	select {
	case got := <-replyCh:
		if got != true {
			t.Fatalf("期望 true，得到 %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("handleKey 没在 1s 内写回 Reply")
	}

	if m.authReq != nil {
		t.Fatalf("写回后 m.authReq 应清空，得到 %+v", m.authReq)
	}
}

// TestAuthFlowNoAuth 验证 m.authReq 为 nil 时按 y/n 不会被当作授权回复。
func TestAuthFlowNoAuth(t *testing.T) {
	m := &model{busy: true}
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}
	updatedModel, _ := m.Update(keyMsg)
	m = updatedModel.(*model)
	// busy=true 时 y 会被吃掉（m.authReq == nil 直接落到 busy 分支）
	// 但 m.authReq 应保持 nil
	if m.authReq != nil {
		t.Fatal("m.authReq 仍为 nil 时不该被设置")
	}
}