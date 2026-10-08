package tui

// authRequestMsg 工具执行前的用户授权请求；TUI 须在 replyCh 上回应 bool。
// 通过 inp.AskYesNo 钩子由 REPL 工具线程投递给 TUI。
type authRequestMsg struct {
	prompt  string
	replyCh chan bool
}