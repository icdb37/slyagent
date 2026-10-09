package repl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

type ToolShellParam struct {
	Cmd string   `json:"cmd"`
	Arg []string `json:"arg,omitempty"`
}

func (t *ToolShellParam) Format() string {
	return fmt.Sprintf("%s %s", t.Cmd, strings.Join(t.Arg, " "))
}

// ToolShellResult 终端命令执行结果：合并 stdout/stderr，附带退出码
// Code=0 表示正常退出；非零表示异常退出（输出仍会返回，方便模型诊断）
type ToolShellResult struct {
	Output string `json:"output"`
	Code   int    `json:"code"`
}

// shellWriter 是 io.Writer，限制累积字节数；超出后丢弃剩余输入并通过 onOverflow 通知调用方
// 用于在命令输出过大时及时 kill 子进程，避免无谓的 IO/内存消耗
type shellWriter struct {
	mu   sync.Mutex
	cap  int
	buf  strings.Builder
	over bool
}

func init() {
	BuiltIns["ruh_shell"] = &UnitTool{
		Schema: &XllmTool{
			Name:        "ruh_shell",
			Description: "在 shell 执行一条命令，返回 stdout+stderr 合并输出与退出码。\n\n参数：cmd 是可执行文件名或绝对路径，arg 是参数数组。示例：cmd=[\"bash\"]、arg=[\"-c\", \"ls\"]。\n\n返回：output 是合并输出（超长末尾追加截断标记）；code 是退出码——0=成功；非 0=异常退出，结合 output 判断原因；-1=工具超时强制 kill。\n\n约束：超时时长与输出上限由配置决定；输出超限时建议在命令侧用 grep/head/tail 预过滤。\n\n用户交互：每次执行前会向用户发起确认 [y/N]，只有用户回答 y/yes 才会真正运行；任何其他输入（包括直接回车、no、ctrl-c 等）都视为取消。\n\n取消语义：当用户取消时，工具会返回错误\"用户取消运行命令\"。收到该错误后必须立即停止当前任务——不要再调用本工具，也不要尝试用其它变体命令绕过（例如改写路径、改用 sudo/powershell 之类）；不要再调用任何其它工具；以一句简短话告知用户命令已被取消，然后结束当前问答轮，不要再生成后续动作。",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"cmd":{"type":"string","description":"可执行文件路径或可被系统解析的命令名"},
					"arg":{"type":"array","items":{"type":"string"},"description":"命令行参数列表"}
				},
				"required":["cmd"],
				"additionalProperties":false
			}`),
		},
		Param: &ToolShellParam{},
		Process: func(c *Client, a any) (any, error) {
			tp, ok := a.(*ToolShellParam)
			if !ok {
				return nil, fmt.Errorf("ruh_shell param type invalid，请检查代码之后重新运行")
			}
			if !c.AskYesNo("请求执行命令: " + tp.Format()) {
				return nil, fmt.Errorf("用户取消运行命令")
			}
			return run_cmd(tp.Cmd, tp.Arg...)
		},
	}
}

func (w *shellWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.over {
		// 已溢出：静默丢弃，避免子进程写满 pipe 后阻塞
		return len(p), nil
	}
	w.buf.Write(p)
	w.over = len([]rune(w.buf.String())) > w.cap
	return len(p), nil
}

func (w *shellWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (w *shellWriter) Over() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.over
}

func run_cmd(pcmd string, pargs ...string) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), shellRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, pcmd, pargs...)
	sw := &shellWriter{cap: shellOutputSize}
	cmd.Stdout = sw
	cmd.Stderr = sw
	err := cmd.Run()
	result := &ToolShellResult{Output: sw.String()}
	if sw.Over() {
		result.Output += fmt.Sprintf("...太长截断(> %d 字符)", shellOutputSize)
	}
	if ctx.Err() == context.DeadlineExceeded {
		result.Code = -1
		return result, fmt.Errorf("command timeout after %s", shellRunTimeout)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.Code = exitErr.ExitCode()
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}
