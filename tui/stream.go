package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"slyagent/inp"
)

// installTUIAuth 把 inp.AskYesNo 替换为基于 tea.Program 的实现。
// 工具线程在需要授权时通过 program.Send 把请求投递给 UI，并阻塞等待回复。
func installTUIAuth(p *tea.Program) {
	inp.AskYesNo = func(prompt string) bool {
		replyCh := make(chan bool, 1)
		p.Send(authRequestMsg{prompt: prompt, replyCh: replyCh})
		return <-replyCh
	}
}

// forwardEvents 持续把 REPL 事件转为 tea.Msg 推给 model。c.out 关闭时返回。
// 这是修复 32-frame 死锁的关键——之前 c.out 在 TUI 模式下根本没人消费。
func (m *model) forwardEvents() {
	for ev := range m.client.Events() {
		m.program.Send(ev)
	}
}
