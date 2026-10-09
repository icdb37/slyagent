package tui

// forwardEvents 持续把 REPL 事件转为 tea.Msg 推给 model。c.out 关闭时返回。
// 这是修复 32-frame 死锁的关键——之前 c.out 在 TUI 模式下根本没人消费。
//
// c.out 上既可能有 *repl.EvtRepl（普通事件），也可能有 *repl.EvtUserAuthq（授权请求，
// Client.AskYesNo 投递）；Update 的 type switch 各自处理。
func (m *model) forwardEvents() {
	for ev := range m.client.Events() {
		m.program.Send(ev)
	}
}
