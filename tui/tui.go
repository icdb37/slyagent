// Package tui 实现基于 bubbletea 的终端 UI：交互历史记录、输入框、状态展示。
//
// 入口为 Run：创建 repl.Client、把 inp.AskYesNo 替换为 TUI 弹窗实现、启动 forwardEvents
// drain 通道、启动 REPL goroutine。所有对话推进由 REPL 自主完成；TUI 只负责：
//   - 按键 → 包装成 repl.Cmd 投递
//   - repl.Event  → 渲染或更新状态
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"slyagent/repl"
)

// Run 启动 TUI，阻塞直到用户退出或 ctx 被取消。
func Run(ctx context.Context, cfg *repl.Config) error {
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	m.program = p

	installTUIAuth(p)
	m.client.Start()
	go m.forwardEvents()

	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	_, err := p.Run()
	m.client.Stop()
	return err
}

// authRequest 暂存一次授权请求，等待用户按 y/n。
type authRequest struct {
	prompt  string
	replyCh chan bool
}

type model struct {
	cmd     chan<- repl.Cmd
	cfg     *repl.Config
	client  *repl.Client
	program *tea.Program

	width  int
	height int
	ready  bool

	viewport viewport.Model
	items    []historyItem
	atBottom bool

	textInput textinput.Model

	busy    bool
	authReq *authRequest

	status string
}

func newModel(cfg *repl.Config) *model {
	ti := textinput.New()
	ti.Placeholder = "输入消息，回车发送 (Ctrl+C 退出)"
	ti.Prompt = ""
	ti.Focus()
	ti.CharLimit = 0

	vp := viewport.New(80, 20)

	client, cmdCh := repl.New(cfg)
	return &model{
		cmd:       cmdCh,
		cfg:       cfg,
		client:    client,
		items:     []historyItem{},
		textInput: ti,
		viewport:  vp,
		status:    "就绪",
	}
}

func (m *model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.handleResize()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	// 鼠标事件：滚轮上下直接驱动 viewport，不依赖 focus 状态。
	// 终端终端环境鼠标 hover 仍会触发，但只有 wheel 才走 ScrollUp/ScrollDown。
	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			m.viewport.ScrollUp(3)
			return m, nil
		case tea.MouseWheelDown:
			m.viewport.ScrollDown(3)
			return m, nil
		}
		return m, nil

	// REPL 事件：所有事件统一为 *repl.EvtRepl，按 Kind 分发并用 EncodeEvent[T] 解出 payload。
	case *repl.EvtRepl:
		return m.handleEvt(msg)

	// 授权请求
	case authRequestMsg:
		m.authReq = &authRequest{prompt: msg.prompt, replyCh: msg.replyCh}
		m.textInput.Blur()
		m.status = "等待授权"
		m.refreshViewport()
		return m, nil
	}

	return m, nil
}

// handleEvt 把单一 EvtRepl 按 Kind 分发并解码 payload；payload 解码失败时静默丢弃。
func (m *model) handleEvt(env *repl.EvtRepl) (tea.Model, tea.Cmd) {
	switch env.Kind {
	case repl.UserEcho:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return m, nil
		}
		m.items = append(m.items, historyItem{kind: itemUser, text: p.Data})
		m.refreshViewport()

	case repl.Busy:
		p, err := repl.EncodeEvent[repl.EvtPayloadBusy](env)
		if err != nil || p == nil {
			return m, nil
		}
		m.busy = p.Busy
		if m.busy {
			m.textInput.Blur()
			m.status = "思考中..."
		} else {
			m.textInput.Focus()
			m.status = "就绪"
		}

	case repl.AssistantStart:
		m.items = append(m.items, historyItem{kind: itemAssistant, streaming: true})
		m.refreshViewport()

	case repl.AssistantChunk:
		p, err := repl.EncodeEvent[repl.EvtPayloadAssistantChunk](env)
		if err != nil || p == nil {
			return m, nil
		}
		if n := len(m.items); n > 0 {
			m.items[n-1].text += p.Delta
		}
		m.refreshViewport()

	case repl.AssistantEnd:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return m, nil
		}
		if n := len(m.items); n > 0 {
			m.items[n-1].streaming = false
			if p.Data != "" {
				m.items[n-1].text += "\n[错误: " + p.Data + "]"
			}
		}
		m.refreshViewport()

	case repl.ToolCall:
		p, err := repl.EncodeEvent[repl.EvtPayloadToolCall](env)
		if err != nil || p == nil {
			return m, nil
		}
		m.items = append(m.items, historyItem{kind: itemToolCall, toolName: p.Name, toolInput: p.Input})
		m.refreshViewport()

	case repl.ToolResult:
		p, err := repl.EncodeEvent[repl.EvtPayloadToolResult](env)
		if err != nil || p == nil {
			return m, nil
		}
		m.items = append(m.items, historyItem{
			kind:       itemToolResult,
			toolName:   p.Name,
			toolResult: p.Result,
			toolErr:    p.Err,
		})
		m.refreshViewport()

	case repl.Status:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return m, nil
		}
		m.status = p.Data

	case repl.Compress:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return m, nil
		}
		m.items = append(m.items, historyItem{
			kind: itemSystem,
			text: fmt.Sprintf("[历史压缩：%s 条旧消息合并为 1 条摘要]", p.Data),
		})
		m.refreshViewport()

	case repl.Error:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return m, nil
		}
		m.items = append(m.items, historyItem{kind: itemError, text: p.Data})
		m.refreshViewport()

	case repl.System:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return m, nil
		}
		m.items = append(m.items, historyItem{kind: itemSystem, text: p.Data})
		m.refreshViewport()

	case repl.Clear:
		m.items = nil
		m.refreshViewport()

	case repl.Quit:
		return m, tea.Quit

	case repl.SessionNew:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return m, nil
		}
		m.items = append(m.items, historyItem{kind: itemSystem, text: p.Data})
		m.refreshViewport()

	case repl.SessionList:
		p, err := repl.EncodeEvent[repl.EvtPayloadSessionList](env)
		if err != nil || p == nil {
			return m, nil
		}
		m.items = append(m.items, historyItem{kind: itemSystem, text: strings.Join(p.IDs, "\n")})
		m.refreshViewport()

	case repl.SessionLoad:
		p, err := repl.EncodeEvent[repl.EvtPayloadSessionLoad](env)
		if err != nil || p == nil {
			return m, nil
		}
		m.items = make([]historyItem, 0, len(p.Items))
		if p.ID != "" {
			// 前置一条系统消息表明加载来源
			m.items = append(m.items, historyItem{kind: itemSystem, text: "[已加载: " + p.ID + "]"})
		}
		for _, it := range p.Items {
			m.items = append(m.items, sessionItemToHistoryItem(it))
		}
		m.refreshViewport()
	}

	return m, nil
}

// sessionItemToHistoryItem 把 wire SessionItem 映射为内部 historyItem。
// SessionItem 是 repl 在 emit 时按 Message 拆好的渲染单元；这里只做 wire →
// 内部 itemKind 的字面量转换，不再二次解析 role / content blocks。
func sessionItemToHistoryItem(it *repl.SessionItem) historyItem {
	switch it.Kind {
	case repl.SessionItemUser:
		return historyItem{kind: itemUser, text: it.Text}
	case repl.SessionItemAssistant:
		return historyItem{kind: itemAssistant, text: it.Text}
	case repl.SessionItemThinking:
		return historyItem{kind: itemThinking, text: it.Text}
	case repl.SessionItemToolCall:
		return historyItem{kind: itemToolCall, toolName: it.Name, toolInput: it.Input}
	case repl.SessionItemToolResult:
		return historyItem{kind: itemToolResult, toolResult: it.Result, toolErr: it.Err}
	case repl.SessionItemSystem:
		return historyItem{kind: itemSystem, text: it.Text}
	}
	return historyItem{}
}

func (m *model) handleResize() {
	const (
		topH    = 1
		inputH  = 1
		statusH = 1
		sepH    = 3
	)
	vpH := m.height - topH - inputH - statusH - sepH
	if vpH < 3 {
		vpH = 3
	}
	if m.width < 20 {
		m.width = 20
	}
	m.viewport.Width = m.width
	m.viewport.Height = vpH
	m.textInput.Width = m.width
	m.refreshViewport()
}

func (m *model) refreshViewport() {
	m.atBottom = m.viewport.AtBottom()
	m.viewport.SetContent(renderItems(m.items))
	if m.busy || m.atBottom {
		m.viewport.GotoBottom()
	}
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 1) 授权请求最高优先级：y/n 立即回复
	if m.authReq != nil {
		switch msg.String() {
		case "y", "Y":
			m.authReq.replyCh <- true
			m.authReq = nil
			// 不 Focus：REPL 还在 busy 状态，由 EvBusy{false} 触发 Focus
			m.status = "运行中..."
			m.refreshViewport()
		case "n", "N", "esc":
			m.authReq.replyCh <- false
			m.authReq = nil
			m.status = "已取消"
			m.refreshViewport()
		}
		return m, nil
	}

	// 2) 全局键
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.busy {
			return m, nil
		}
		m.textInput.SetValue("")
		return m, nil
	case "up":
		// 单行 textinput 对 up/down 无副作用；这里直接拿来滚 viewport，
		// 让"非忙时"也能向上翻看历史。
		m.viewport.ScrollUp(1)
		return m, nil
	case "down":
		m.viewport.ScrollDown(1)
		return m, nil
	case "pgup":
		m.viewport.HalfPageUp()
		return m, nil
	case "pgdown":
		m.viewport.HalfPageDown()
		return m, nil
	}

	// 3) 忙时其它键吃掉（仅滚动 + 退出可用）
	if m.busy {
		return m, nil
	}

	// 4) Enter 发送
	if msg.String() == "enter" {
		text := strings.TrimSpace(m.textInput.Value())
		if text == "" {
			return m, nil
		}
		m.cmd <- cmdFromText(text)
		m.textInput.SetValue("")
		return m, nil
	}

	// 5) 其它键交给 textinput
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// cmdFromText 把用户输入的文本翻译为对应的 Cmd。
// 以 "/" 开头为命令；其余作为 CmdAsk。
func cmdFromText(text string) repl.Cmd {
	low := strings.ToLower(text)
	cmd := low
	if pos := strings.Index(low, " "); pos != -1 {
		cmd = low[:pos]
	}
	switch cmd {
	case "/q", "/quit", "/exit":
		return repl.Cmd{Kind: repl.CmdQuit}
	case "/clear", "/reset":
		return repl.Cmd{Kind: repl.CmdClear}
	case "/compress":
		return repl.Cmd{Kind: repl.CmdCompress}
	case "/session":
		return repl.Cmd{Kind: repl.CmdSession, Msg: strings.TrimSpace(text[len("/session"):])}
	}
	return repl.Cmd{Kind: repl.CmdAsk, Msg: text}
}

func (m *model) View() string {
	if !m.ready {
		return "初始化中..."
	}
	top := topBarStyle.Render(fmt.Sprintf(" slyagent  %s", m.cfg.ModelName))
	sep := systemStyle.Render(strings.Repeat("─", m.width))

	input := inputPromptStyle.Render("> ") + m.textInput.View()
	status := m.renderStatus()

	if m.authReq != nil {
		prompt := m.authReq.prompt
		if prompt == "" {
			prompt = "确认执行？"
		}
		auth := authStyle.Render("⚠ "+prompt) + " " +
			lipgloss.NewStyle().Render("[y]是 [n]否 [esc]取消")
		input = input + "\n" + auth
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		top,
		sep,
		m.viewport.View(),
		sep,
		input,
		sep,
		status,
	)
}

func (m *model) renderStatus() string {
	icon := "✓"
	if m.busy {
		icon = "◐"
	}
	parts := []string{
		fmt.Sprintf("%s %s", icon, m.status),
		fmt.Sprintf("历史: %d", len(m.items)),
	}
	return statusBarStyle.Render(strings.Join(parts, "  │  "))
}
