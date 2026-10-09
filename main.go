package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"slyagent/repl"
	"slyagent/tui"
)

func main() {
	cfg := &repl.Config{
		ModelName: "MiniMax-M2.7",
		BaseURL:   "https://api.minimax.cn/anthropic/v1/messages",
		APIKey:    os.Getenv("LLM_API_KEY"),
		MaxTokens: 1024 * 10,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Kill, os.Interrupt)
	defer cancel()

	if len(os.Args) > 1 {
		cfg.SessID = strings.TrimSpace(os.Args[1])
	}
	if cfg.SessID == "" {
		cfg.SessID = "default"
	}
	if err := tui.Run(ctx, cfg); err != nil {
		os.Stderr.WriteString("tui error: " + err.Error() + "\n")
		os.Exit(1)
	}
}

// runCLI 是 REPL 的非交互前端：把事件写到 stdout、读 stdin 转 Cmd。
func runCLI(ctx context.Context, cfg *repl.Config) {
	c, cmdCh := repl.New(cfg)
	c.Start()

	go func() {
		r := bufio.NewReader(os.Stdin)
		for {
			line, _, err := r.ReadLine()
			if err != nil { // EOF / Ctrl+D / 管道关闭
				c.Stop()
				return
			}
			s := strings.TrimSpace(string(line))
			if s == "" {
				continue
			}
			cmdCh <- cliCmd(s)
		}
	}()

	go func() {
		r := bufio.NewReader(os.Stdin)
		for ev := range c.Events() {
			switch v := ev.(type) {
			case *repl.EvtUserAuthq:
				fmt.Fprintf(os.Stdout, "%s[y/N]: ", v.Prompt)
				line, _, _ := r.ReadLine()
				ans := strings.ToLower(strings.TrimSpace(string(line)))
				v.Reply <- (ans == "y" || ans == "yes")
			default:
				printEvent(ev)
			}
		}
	}()

	<-ctx.Done()
	c.Stop()
}

// cliCmd 把 CLI 文本翻译为 Cmd。
func cliCmd(text string) repl.Cmd {
	low := strings.ToLower(text)
	switch low {
	case "/q", "/quit", "/exit":
		return repl.Cmd{Kind: repl.CmdQuit}
	case "/clear", "/reset":
		return repl.Cmd{Kind: repl.CmdClear}
	case "/compress":
		return repl.Cmd{Kind: repl.CmdCompress}
	}
	return repl.Cmd{Kind: repl.CmdAsk, Msg: text}
}

// printEvent 把单个 REPL 事件格式化为 stdout 文本。
func printEvent(ev any) {
	env, ok := ev.(*repl.EvtRepl)
	if !ok {
		return
	}
	switch env.Kind {
	case repl.UserEcho:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return
		}
		fmt.Fprintf(os.Stdout, "> %s\n", p.Data)
	case repl.Busy:
		// busy 状态由流式文本本身表达，不单独打印
	case repl.AssistantStart:
		// 标记流式开始；chunk 由 AssistantChunk 累积
	case repl.AssistantChunk:
		p, err := repl.EncodeEvent[repl.EvtPayloadAssistantChunk](env)
		if err != nil || p == nil {
			return
		}
		fmt.Fprint(os.Stdout, p.Delta)
	case repl.AssistantEnd:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return
		}
		if p.Data != "" {
			fmt.Fprintf(os.Stderr, "\n[错误: %s]\n", p.Data)
		} else {
			fmt.Fprintln(os.Stdout)
		}
	case repl.ToolCall:
		p, err := repl.EncodeEvent[repl.EvtPayloadToolCall](env)
		if err != nil || p == nil {
			return
		}
		fmt.Fprintf(os.Stdout, "\n[工具调用: %s]\n  参数: %s\n", p.Name, p.Input)
	case repl.ToolResult:
		p, err := repl.EncodeEvent[repl.EvtPayloadToolResult](env)
		if err != nil || p == nil {
			return
		}
		if p.Err != "" {
			fmt.Fprintf(os.Stderr, "[工具失败: %s] %s\n", p.Err, p.Result)
		} else {
			fmt.Fprintf(os.Stdout, "[工具结果]\n%s\n", p.Result)
		}
	case repl.Status:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return
		}
		fmt.Fprintf(os.Stdout, "[状态: %s]\n", p.Data)
	case repl.Compress:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return
		}
		fmt.Fprintf(os.Stdout, "[历史压缩：%s 条旧消息合并为 1 条摘要]\n", p.Data)
	case repl.Error:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return
		}
		fmt.Fprintf(os.Stderr, "[错误] %s\n", p.Data)
	case repl.System:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return
		}
		fmt.Fprintf(os.Stdout, "%s\n", p.Data)
	case repl.SessionLoad:
		p, err := repl.EncodeEvent[repl.EvtPayloadData](env)
		if err != nil || p == nil {
			return
		}
		fmt.Fprintf(os.Stdout, "%s\n", p.Data)
	case repl.SessionList:
		p, err := repl.EncodeEvent[repl.EvtPayloadSessionList](env)
		if err != nil || p == nil {
			return
		}
		fmt.Fprintf(os.Stdout, "%s\n", strings.Join(p.IDs, "\n"))
	case repl.Clear:
		fmt.Fprintln(os.Stdout, "\033[2J\033[H") // 清屏
	case repl.Quit:
		// 收到 quit 后 c.out 会被关闭，循环自然退出
	}
}
