package tui

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"slyagent/repl"
)

// newTestProgram 创建一个无 TTY、无渲染器的 tea.Program，仅用于在测试中跑通 eventLoop。
// 用 strings.Reader 模拟 stdin，让事件循环能正常 select 到键事件。
func newTestProgram(m *model, input string) *tea.Program {
	return tea.NewProgram(
		m,
		tea.WithoutRenderer(),
		tea.WithInput(strings.NewReader(input)),
		tea.WithOutput(io.Discard),
		tea.WithoutSignalHandler(),
	)
}

// startProgram 在 goroutine 中跑 p.Run，等 eventLoop 起来。
// WithoutRenderer 不会自动发 WindowSizeMsg（handleResize 跳过非 ttyOutput 的情况），
// 所以我们主动 Send 一个 WindowSizeMsg 来触发 m.ready=true，作为 eventLoop 启动信号。
func startProgram(t *testing.T, p *tea.Program, m *model, input string) {
	t.Helper()
	go func() {
		_, _ = p.Run()
	}()
	// 给 eventLoop 一点时间进入 select
	time.Sleep(20 * time.Millisecond)
	// 主动塞一个 WindowSizeMsg，触发 m.ready
	p.Send(tea.WindowSizeMsg{Width: 80, Height: 24})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.ready {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("eventLoop 未在 2s 内 ready")
}

// stopProgram 优雅关闭 program。
func stopProgram(p *tea.Program) {
	p.Quit()
}

// TestAuthFlow_RealProgram 端到端验证：forwardEvents 把 *EvtUserAuthq 投到
// eventLoop，eventLoop 调 Update 设置 m.authReq；用户按 y 后 handleKey 写回 Reply。
// 这条路径走的是真实的 tea.Program + p.Send，模拟生产环境的 transport。
func TestAuthFlow_RealProgram(t *testing.T) {
	cfg := &repl.Config{ModelName: "test"}
	m := newModel(cfg)
	m.busy = true

	p := newTestProgram(m, "")
	m.program = p
	startProgram(t, p, m, "")

	// 模拟 forwardEvents 投递一次授权请求（生产代码里这里就是 m.program.Send(req)）
	req := &repl.EvtUserAuthq{Prompt: "执行命令: ls", Reply: make(chan bool, 1)}
	p.Send(req)

	// 等 Update 把它落到 m.authReq
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.authReq != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if m.authReq == nil {
		t.Fatal("2s 内 m.authReq 仍未设置（p.Send 没被 eventLoop 收到）")
	}

	// 模拟用户按 y。直接 Send KeyMsg 走 eventLoop 而非 stdin 读取，因为本测试 stdin 已被 WithInput 占住。
	p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// 等 reply
	select {
	case got := <-req.Reply:
		if got != true {
			t.Fatalf("期望 true，得到 %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("2s 内未收到 reply（handleKey 没把 y 写回 Reply）")
	}

	stopProgram(p)
}

// TestForwardEvents_AuthNotBlocks 验证 c.Stop() 关闭 c.done 后，AskYesNo
// 能在 select 中收到 c.done 信号并立即返回 false（不会卡住 REPL 退出）。
//
// 关键路径：run goroutine 的 defer close(c.done) 必须先于 AskYesNo 监听者生效，
// 工具线程才能在 REPL 退出时立即解除阻塞。
func TestForwardEvents_AuthNotBlocks(t *testing.T) {
	cfg := &repl.Config{ModelName: "test"}
	c, _ := repl.New(cfg)
	c.Start() // 启 run goroutine，否则 c.Stop 会卡在 <-c.done

	// 在另一 goroutine 跑 AskYesNo：req 会进 c.out（缓冲 256），
	// 然后阻塞在 <-req.Reply 或 <-c.done。
	done := make(chan bool, 1)
	go func() {
		ok := c.AskYesNo("test prompt")
		done <- ok
	}()

	// 等 c.AskYesNo 把 req 投到 c.out
	time.Sleep(50 * time.Millisecond)

	// c.Stop 会 close(c.cmd) → run goroutine 退出 → defer close(c.done)
	// → AskYesNo 在 select 收到 c.done 信号 → 返回 false
	c.Stop()

	select {
	case got := <-done:
		if got != false {
			t.Fatalf("c.done 关闭时 AskYesNo 应返回 false，得到 %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("c.Stop 后 AskYesNo 没在 2s 内返回")
	}
}

// TestConcurrentSends_NoDeadlock 验证大量并发 Send + 真实 eventLoop 不会卡死。
// 这模拟 LLM 流式回答期间的 chunk 风暴。
func TestConcurrentSends_NoDeadlock(t *testing.T) {
	cfg := &repl.Config{ModelName: "test"}
	m := newModel(cfg)
	m.busy = true

	p := newTestProgram(m, "")
	m.program = p
	startProgram(t, p, m, "")

	// 并发 200 个 AssistantChunk 投递
	var wg sync.WaitGroup
	const N = 200
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ev := repl.DecodeEvtRepl(&repl.EvtPayloadAssistantChunk{Delta: "x"}, repl.AssistantChunk)
			p.Send(ev)
		}()
	}

	// 等所有 Send 完
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatalf("200 个 Send 中部分卡住：p.msgs 在 eventLoop 忙时被压住")
	}

	// 投一个 auth 请求，验证 200 chunk 之后 eventLoop 仍能处理新事件
	req := &repl.EvtUserAuthq{Prompt: "after storm", Reply: make(chan bool, 1)}
	p.Send(req)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.authReq != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if m.authReq == nil {
		t.Fatal("chunk 风暴后 auth 请求未送达 eventLoop")
	}

	stopProgram(p)
}
