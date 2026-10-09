// Package tui 是基于 bubbletea 的终端 UI：渲染 REPL 事件、把按键转 Cmd。
// 工具授权由 Client.AskYesNo 通过 *repl.EvtUserAuthq 投到 TUI 后用 y/n 回复。
package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"slyagent/enum"
	"slyagent/repl"
)

// Run 启动 TUI，阻塞直到用户退出或 ctx 被取消。
func Run(ctx context.Context, cfg *repl.Config) error {
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	m.program = p

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
	authReq *repl.EvtUserAuthq

	// mouse 表示鼠标上报是否开启：开启时滚轮可用但终端拖选复制被禁用。
	mouse  bool
	status string

	// sessID 当前会话 id：初始化时来自 cfg.SessID，会话切换时由 SessionLoad 事件更新。
	sessID string
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
		mouse:     true,
		status:    "就绪",
		sessID:    cfg.SessID,
	}
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink}
	// 启动时按 cfg.SessID 加载会话，通过 cmd channel 与 /session load 走同一条路径
	if m.cfg.SessID != "" {
		sid := m.cfg.SessID
		cmds = append(cmds, func() tea.Msg {
			m.cmd <- repl.Cmd{Kind: repl.CmdSession, Msg: "load " + sid}
			return nil
		})
	}
	return tea.Batch(cmds...)
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

	// 鼠标滚轮直接驱动 viewport，不依赖 focus 状态
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

	// REPL 事件：按 Kind 分发并用 EncodeEvent[T] 解出 payload
	case *repl.EvtRepl:
		return m.handleEvt(msg)

	// 授权请求：Client 投到 c.out 的 *repl.EvtUserAuthq；y → true，n/esc → false
	case *repl.EvtUserAuthq:
		m.authReq = msg
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
			m.sessID = p.ID
			m.items = append(m.items, historyItem{kind: itemSystem, text: "[已加载: " + p.ID + "]"})
		}
		for _, it := range p.Items {
			m.items = append(m.items, sessionItemToHistoryItems(it)...)
		}
		m.refreshViewport()
	}

	return m, nil
}

// sessionItemToHistoryItems 把 SessionItem 拆成 TUI 的 historyItem 列表：
// 每条 Contents 按 Type 渲染，text 块的 UI kind 由 Role 决定。
func sessionItemToHistoryItems(it *repl.SessionItem) []historyItem {
	if it == nil {
		return nil
	}
	var items []historyItem
	for _, c := range it.Contents {
		switch c.Type {
		case enum.ResContentTypeText:
			kind := itemAssistant
			switch it.Role {
			case enum.RoleUser:
				kind = itemUser
			case enum.RoleSystem, enum.RoleAgent:
				kind = itemSystem
			}
			items = append(items, historyItem{kind: kind, text: c.Text})
		case enum.ResContentTypeThinking:
			items = append(items, historyItem{kind: itemThinking, text: c.Thinking})
		case enum.ResContentTypeToolUse:
			items = append(items, historyItem{kind: itemToolCall, toolName: c.Name, toolInput: string(c.Input)})
		case enum.ResContentTypeToolResult:
			items = append(items, historyItem{kind: itemToolResult, toolResult: c.Result})
		}
	}
	return items
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
	m.viewport.SetContent(renderItems(m.items, m.width))
	if m.busy || m.atBottom {
		m.viewport.GotoBottom()
	}
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 1) 授权请求最高优先级：y/n 立即回复
	if m.authReq != nil {
		switch msg.String() {
		case "y", "Y":
			m.authReq.Reply <- true
			m.authReq = nil
			m.status = "运行中..."
			m.refreshViewport()
		case "n", "N", "esc":
			m.authReq.Reply <- false
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
	case "ctrl+y":
		// 复制最后一条助手回复，忙碌时也可用
		return m.copyLastAssistant()
	case "alt+m":
		// 切换鼠标上报：关闭后原生拖选复制恢复，但滚轮失效
		m.toggleMouse()
		return m, nil
	case "esc":
		if m.busy {
			return m, nil
		}
		m.textInput.SetValue("")
		return m, nil
	case "up":
		// 非忙时 up/down 滚 viewport
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
	top := topBarStyle.Render(fmt.Sprintf(" slyagent [%s] %s", m.sessID, m.cfg.ModelName))
	sep := systemStyle.Render(strings.Repeat("─", m.width))

	input := inputPromptStyle.Render("> ") + m.textInput.View()
	status := m.renderStatus()

	if m.authReq != nil {
		prompt := m.authReq.Prompt
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
	// 状态栏够宽时附上快捷键提示；窄屏不挤。
	if m.width >= 64 {
		mouseLabel := "开"
		if !m.mouse {
			mouseLabel = "关"
		}
		parts = append(parts, "Ctrl+Y 复制", "Alt+M 鼠标:"+mouseLabel)
	}
	return statusBarStyle.Render(strings.Join(parts, "  │  "))
}

// copyLastAssistant 把最后一条助手回复写入剪贴板：先尝试系统剪贴板，
// 再追加 OSC 52 写入终端剪贴板。失败时把原因写到状态栏。
func (m *model) copyLastAssistant() (tea.Model, tea.Cmd) {
	text := lastAssistantText(m.items)
	if text == "" {
		m.status = "没有可复制的助手回复"
		return m, nil
	}
	if err := clipboard.WriteAll(text); err != nil {
		m.status = "复制失败: " + err.Error()
	} else {
		m.status = "已复制最后一条助手回复（" + fmt.Sprintf("%d", ansi.StringWidth(text)) + " 列）"
	}
	return m, osc52Copy(text)
}

// osc52Copy 返回一个 tea.Cmd，把 text 通过 OSC 52 写入终端剪贴板。
func osc52Copy(text string) tea.Cmd {
	enc := base64.StdEncoding.EncodeToString([]byte(text))
	return tea.Printf("\x1b]52;c;%s\x07", enc)
}

// toggleMouse 切换鼠标上报。关掉时滚轮不再触发 app，终端原生选区
// 复制恢复；开回后滚轮可用但拖选会被 app 捕获。
func (m *model) toggleMouse() {
	if m.mouse {
		m.program.DisableMouseCellMotion()
		m.mouse = false
		m.status = "鼠标已关闭：可拖选复制（↑↓ 翻页），Alt+M 重启"
	} else {
		m.program.EnableMouseCellMotion()
		m.mouse = true
		m.status = "鼠标已开启：滚轮可用，Alt+M 关闭后拖选复制"
	}
}
