package repl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/go-resty/resty/v2"
)

// Client 自驱的 REPL 对话客户端。
//
// 用法：
//
//	c, cmd := repl.New(cfg)
//	c.Start()
//	for ev := range c.Events() { ... }
//	c.Stop()  // 关闭 cmd、等待 run 退出、关闭 c.out，drain 自然结束
//
// 所有对话驱动（命令解析、压缩、流式回答、工具调用循环）由内部 run goroutine 完成。
// UI 只负责把按键包装成 Cmd 投递（向返回的 cmd 通道写），以及把 Event 渲染成视图。
type Client struct {
	cfg *Config
	hc  *resty.Client

	cmd chan Cmd // 由 New 创建并归 Client 所有；Stop 时关闭
	out chan any // run() 退出时关闭

	history []*Message // 单 goroutine 访问（run），无需锁
	latest  []*ResContent

	startMu  sync.Mutex
	started  bool
	stopOnce sync.Once
	done     chan struct{}
	sess     *Session
	id       string
}

// New 构造客户端并创建其 cmd 通道（缓冲 32）。
// 返回的 chan Cmd 由 Client 拥有；调用方不要直接 close，应使用 Stop。
func New(cfg *Config) (*Client, chan Cmd) {
	cmdCh := make(chan Cmd, 32)
	c := &Client{
		cfg:  cfg,
		hc:   resty.New(),
		cmd:  cmdCh,
		out:  make(chan any, 256),
		done: make(chan struct{}),
		sess: NewSession(),
		id:   "default",
	}
	return c, cmdCh
}

// Start 启动 run goroutine。重复调用安全。
func (c *Client) Start() {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	if c.started {
		return
	}
	c.started = true
	go c.run()
}

// Stop 关闭 cmd 通道，等待 run 退出（同时关闭 c.out 让 drain 端结束）。重复调用安全。
func (c *Client) Stop() {
	c.stopOnce.Do(func() {
		close(c.cmd)
	})
	<-c.done
}

func (c *Client) SaveSession() error {
	return c.sess.Save(&MsgStore{ID: c.id, History: c.history})
}
func (c *Client) LoadSession(id string) error {
	msg, err := c.sess.Load(id)
	if err != nil {
		return err
	}
	c.history = msg.History
	c.id = msg.ID
	return nil
}
func (c *Client) NewSession(id string) error {
	if err := c.SaveSession(); err != nil {
		return err
	}
	c.id = id
	clear(c.history)
	return nil
}

// Events 返回事件通道。run() 退出时会关闭它。
func (c *Client) Events() <-chan any {
	return c.out
}

// Reset 同步清空历史并通知 UI 清屏。仅供内部 run goroutine 在处理 CmdClear 时调用。
// 若 c.out 已无 drain，会阻塞——前提是调用方在 Events() 上持续消费。
func (c *Client) Reset() {
	c.history = c.history[:0]
	c.out <- DecodeEvtRepl(&EvtPayloadData{}, Clear)
}

// ---------- 公共：完整回答（用于 runCompress）----------

// ChatFull 问答，全量回答。保留公开供 client_test 使用。
func (c *Client) ChatFull(msg string) (string, error) {
	param := c.buildReqXllm(msg)
	hreq := c.hc.NewRequest()
	res := &ResXllm{}
	hreq.SetBody(param).
		SetResult(res).
		SetAuthToken(c.cfg.APIKey)
	hres, err := hreq.Post(c.cfg.BaseURL)
	if err != nil {
		return "", err
	}
	if !hres.IsSuccess() {
		return "", errors.New(hres.String())
	}
	text := ""
	for _, c := range res.Content {
		if c.Type == ResContentTypeText {
			text = c.Text
		}
	}
	c.processAssistantMessage(param, res)
	return text, nil
}

// ---------- 内部：run goroutine ----------

// run 是单一 cmd 消费循环，按 Kind 分发。退出前关闭 c.out 让 drain 端结束。
func (c *Client) run() {
	defer close(c.done)
	defer close(c.out)
	for cmd := range c.cmd {
		c.handleCmd(cmd)
	}
}

func (c *Client) handleCmd(cmd Cmd) {
	switch cmd.Kind {
	case CmdAsk:
		c.runChat(cmd.Msg)
	case CmdQuit:
		c.out <- DecodeEvtRepl(&EvtPayloadData{}, Quit)
	case CmdClear:
		c.Reset()
	case CmdCompress:
		c.runCompress()
	case CmdSession:
		c.runSession(cmd.Msg)
	default:
		c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "未知命令: " + string(cmd.Kind)}, System)
	}
}

func (c *Client) runSession(msg string) {
	parts := strings.Fields(msg)
	if len(parts) == 0 {
		return
	}
	cmd := strings.ToLower(parts[0])
	switch cmd {
	case "new":
		sid, ok := c.sessIDArg(parts)
		if !ok {
			return
		}
		if err := c.NewSession(sid); err != nil {
			c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "保存当前会话失败: " + err.Error()}, Error)
			return
		}
		c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "==== " + sid + " ===="}, SessionNew)
	case "list":
		c.out <- DecodeEvtRepl(&EvtPayloadSessionList{IDs: c.sess.GetIDs()}, SessionList)
	case "load":
		sid, ok := c.sessIDArg(parts)
		if !ok {
			return
		}
		if err := c.LoadSession(sid); err != nil {
			c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "加载会话失败: " + err.Error()}, Error)
			return
		}
		c.out <- DecodeEvtRepl(&EvtPayloadSessionLoad{ID: sid, Items: ToSessionItems(c.history)}, SessionLoad)
	case "save":
		if err := c.SaveSession(); err != nil {
			c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "保存失败: " + err.Error()}, Error)
			return
		}
		c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "[已保存: " + c.id + "]"}, System)
	case "rm", "remove", "del", "delete":
		sid, ok := c.sessIDArg(parts)
		if !ok {
			return
		}
		if err := c.sess.Remove(sid); err != nil {
			c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "删除失败: " + err.Error()}, Error)
			return
		}
		c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "[已删除: " + sid + "]"}, System)
	default:
		c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "未知会话命令: " + cmd}, System)
	}
}

// sessIDArg 取出 subcmd 后的会话 id；缺失或为空时发 System 提示并返回 ok=false。
func (c *Client) sessIDArg(parts []string) (string, bool) {
	if len(parts) < 2 {
		c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "需要会话 id"}, System)
		return "", false
	}
	sid := strings.TrimSpace(parts[1])
	if sid == "" {
		c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "需要会话 id"}, System)
		return "", false
	}
	return sid, true
}

// runChat 完整跑一轮：压缩（如需）→ 流式回答 → 工具循环。
// 末尾 defer 把 busy 置 false；recover 在 busy 之前注册，避免 panic 永远卡 busy。
func (c *Client) runChat(userMsg string) {
	defer func() {
		if r := recover(); r != nil {
			c.out <- DecodeEvtRepl(&EvtPayloadData{Data: fmt.Sprintf("panic: %v", r)}, Error)
		}
	}()
	defer func() {
		c.out <- DecodeEvtRepl(&EvtPayloadBusy{Busy: false}, Busy)
	}()

	c.out <- DecodeEvtRepl(&EvtPayloadData{Data: userMsg}, UserEcho)
	c.out <- DecodeEvtRepl(&EvtPayloadBusy{Busy: true}, Busy)

	// 1) 触发压缩
	if pos := c.compressIndex(); pos > 0 {
		c.out <- DecodeEvtRepl(&EvtPayloadData{Data: fmt.Sprintf("[正在压缩 %d 条旧消息...]", pos)}, System)
		if err := c.doCompress(); err != nil {
			c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "压缩失败: " + err.Error()}, Error)
			return
		}
		c.out <- DecodeEvtRepl(&EvtPayloadData{Data: fmt.Sprintf("%d", pos)}, Compress)
	}

	// 2) 问答循环
	msg := userMsg
	for {
		c.out <- DecodeEvtRepl(&EvtPayloadData{}, AssistantStart)
		err := c.chatIncrStream(msg, func(delta string) {
			c.out <- DecodeEvtRepl(&EvtPayloadAssistantChunk{Delta: delta}, AssistantChunk)
		})
		c.out <- DecodeEvtRepl(&EvtPayloadData{Data: errString(err)}, AssistantEnd)
		if err != nil {
			return
		}
		if !c.execToolsAndEmit() {
			return
		}
		msg = "" // 工具循环续接：不再追加用户消息
	}
}

// chatIncrStream 是 ChatIncr 的内部版：text delta 走 onDelta 回调而不是 print。
// 末尾仍调 processAssistantMessage 把本轮助手消息写入 history/latest。
func (c *Client) chatIncrStream(msg string, onDelta func(string)) error {
	const (
		prefixEvtType = "event: "
		prefixEvtData = "data: "
	)
	param := c.buildReqXllm(msg)
	param.Stream = true
	hreq := c.hc.NewRequest()
	hreq.SetBody(param).
		SetAuthToken(c.cfg.APIKey).
		SetDoNotParseResponse(true)
	hres, err := hreq.Post(c.cfg.BaseURL)
	if err != nil {
		return err
	}
	if !hres.IsSuccess() {
		body, _ := io.ReadAll(hres.RawResponse.Body)
		hres.RawResponse.Body.Close()
		return errors.New(string(body))
	}
	res := &ResXllm{}
	defer hres.RawResponse.Body.Close()
	r := bufio.NewReader(hres.RawResponse.Body)
	evtType, evtData := "", []byte{}
	rcs := []*ResContent{}
	var rc *ResContent
	for {
		line, _, err := r.ReadLine()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if bytes.HasPrefix(line, []byte(prefixEvtType)) {
			evtType = string(line[len(prefixEvtType):])
			continue
		}
		if bytes.HasPrefix(line, []byte(prefixEvtData)) {
			evtData = line[len(prefixEvtData):]
			switch evtType {
			case "message_start":
				evtVal := &ResEventMessage{}
				if err := json.Unmarshal(evtData, evtVal); err != nil {
					return err
				}
				res = evtVal.Message
				evtVal.Message = nil
			case "ping":
			case "content_block_start":
				evtVal := &ResContentBlock{}
				if err := json.Unmarshal(evtData, evtVal); err != nil {
					return err
				}
				rc = &ResContent{
					Type: evtVal.ContentBlock.Type,
					ID:   evtVal.ContentBlock.ID,
					Name: evtVal.ContentBlock.Name,
				}
				rcs = append(rcs, rc)
			case "content_block_stop":
			case "content_block_delta":
				evtVal := &ResContentDelta{}
				if err := json.Unmarshal(evtData, evtVal); err != nil {
					return err
				}
				switch evtVal.Delta.Type {
				case ResContentDeltaTypeThinking:
					rc.Thinking += evtVal.Delta.Thinking
				case ResContentDeltaTypeText:
					rc.Text += evtVal.Delta.Text
					onDelta(evtVal.Delta.Text)
				case ResContentDeltaTypeSignature:
					rc.Signature = evtVal.Delta.Signature
				case ResContentDeltaTypeInputJson:
					rc.Input = append(rc.Input, json.RawMessage(evtVal.Delta.PartialJson)...)
				}
			case "message_delta":
				evtVal := &ResMessageDelta{}
				if err := json.Unmarshal(evtData, evtVal); err != nil {
					return err
				}
				res.Usage.OutputTokens = evtVal.Usage.OutputTokens
				res.StopReason = evtVal.Delta.StopReason
			case "message_stop":
			}
		}
	}
	res.Content = rcs
	c.processAssistantMessage(param, res)
	return nil
}

// execToolsAndEmit 遍历 latest 中的 tool_use，逐个 emit 并执行。
// 在 runOneTool 之前 emit EvToolCall，让 UI 在授权确认前显示意图。
func (c *Client) execToolsAndEmit() bool {
	if len(c.latest) == 0 {
		return false
	}
	toolResults := []*ResContent{}
	toolUsed := false
	for _, rc := range c.latest {
		if rc.Type != ResContentTypeToolUse {
			continue
		}
		if len(c.history) == 0 {
			// 防御：没有上一条助手消息则跳过（理论上 chatIncr 刚写完）
			continue
		}
		c.history[len(c.history)-1].action = MessageActionToolCall
		toolUsed = true
		toolResult := &ResContent{
			Type:      ResContentTypeToolResult,
			ToolUseID: rc.ID,
		}
		toolResults = append(toolResults, toolResult)

		// 先 emit 意图，再执行（执行期间会触发 AskYesNo）
		c.out <- DecodeEvtRepl(&EvtPayloadToolCall{Name: rc.Name, Input: shortJSON(rc.Input)}, ToolCall)

		ec, _ := BuiltIns[rc.Name]
		resultStr, toolErr := runOneTool(rc.Name, rc.Input, ec)
		toolResult.Result = resultStr

		c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "工具: " + rc.Name}, Status)
		c.out <- DecodeEvtRepl(&EvtPayloadToolResult{Name: rc.Name, Result: resultStr, Err: errString(toolErr)}, ToolResult)
	}
	if toolUsed {
		c.history = append(c.history,
			&Message{
				Role:    RoleUser,
				Content: &BlocksContent{Blocks: toolResults},
				action:  MessageActionToolResult,
			})
	}
	c.latest = c.latest[:0]
	return toolUsed
}

// runCompress 触发压缩并在末尾 emit EvCompress。
// 注意：doCompress 自身不再 print；压缩的"系统通知"由 runConversation 开头 + 此函数末尾各自发出。
func (c *Client) runCompress() {
	pos := c.compressIndex()
	if pos < 0 {
		c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "[无需压缩]"}, System)
		return
	}
	c.out <- DecodeEvtRepl(&EvtPayloadData{Data: fmt.Sprintf("[正在压缩 %d 条旧消息...]", pos)}, System)
	if err := c.doCompress(); err != nil {
		c.out <- DecodeEvtRepl(&EvtPayloadData{Data: "压缩失败: " + err.Error()}, Error)
		return
	}
	c.out <- DecodeEvtRepl(&EvtPayloadData{Data: fmt.Sprintf("%d", pos)}, Compress)
}

// ---------- 内部：history / compress / 工具 ----------

// processAssistantMessage 把本轮助手消息写入 history/latest（chatIncr / chatFull 都用）。
func (c *Client) processAssistantMessage(req *ReqXllm, res *ResXllm) {
	if !SaveThinking {
		out := make([]*ResContent, 0, len(res.Content))
		for _, rc := range res.Content {
			if rc.Type == ResContentTypeThinking {
				continue
			}
			out = append(out, rc)
		}
		res.Content = out
	}
	c.latest = res.Content
	if len(req.Messages) == 0 {
		return
	}
	c.history = append(c.history,
		req.Messages[len(req.Messages)-1],
		&Message{
			Role:    RoleAssistant,
			Content: &BlocksContent{Blocks: res.Content},
			action:  MessageActionAssistant,
		})
}

// buildReqXllm 构造一次模型调用所需的请求体。msg 为空时不追加用户消息。
func (c *Client) buildReqXllm(msg string) *ReqXllm {
	msgs := append([]*Message{}, c.history...)
	if msg != "" {
		msgs = append(msgs,
			&Message{
				Role:    RoleUser,
				Content: &TextContent{Text: msg},
				action:  MessageActionUser,
			})
	}
	param := &ReqXllm{
		Messages:    msgs,
		Model:       c.cfg.ModelName,
		MaxTokens:   c.cfg.MaxTokens,
		Temperature: c.cfg.Temperature,
	}
	for _, v := range BuiltIns {
		param.Tools = append(param.Tools, v.Schema)
	}
	return param
}

// runOneTool 执行单个工具并返回其结果字符串和错误。
func runOneTool(name string, input json.RawMessage, ec *UnitTool) (string, error) {
	if ec == nil {
		return fmt.Sprintf(`{"error":"unknown tool %q"}`, name), nil
	}
	if err := json.Unmarshal(input, ec.Param); err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), nil
	}
	result, err := ec.Process(ec.Param)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), err
	}
	return truncate(string(data), MessageToolResultMax), nil
}

func (c *Client) compressIndex() int {
	hs := len(c.history)
	if hs < MessageRecentNum*MessageRecentMul1 {
		return -1
	}
	for _, h := range c.history {
		if h.size != 0 {
			continue
		}
		data, _ := json.Marshal(h)
		h.size = len(data)
	}
	if hs <= MessageRecentNum*MessageRecentMu2 {
		totalSize := 0
		for _, h := range c.history {
			totalSize += h.size
		}
		if totalSize <= MessageHistorySize {
			return -1
		}
	}
	pos := hs - MessageRecentNum
	for pos > 0 && !validCutAt(c.history, pos-1) {
		pos--
	}
	if pos <= 0 {
		return -1
	}
	return pos
}

func validCutAt(history []*Message, i int) bool {
	if i+1 >= len(history) {
		return false
	}
	return history[i+1].action == MessageActionUser
}

func (c *Client) doCompress() error {
	pos := c.compressIndex()
	if pos < 0 {
		return nil
	}
	history := c.history
	hs := len(c.history)
	msgs := []*Message{
		{
			Role: RoleSystem,
			Content: &TextContent{Text: `你是对话压缩器。把以下对话历史压缩成简洁的中文要点，必须保留：

【意图】用户的目标、需求、已做的决定与偏好
【事实】涉及的文件路径、shell 命令、工具调用及关键结论
【状态】已完成、进行中、未完成的事项

只输出要点；不解释、不寒暄、不保留原话。`},
		},
		{
			Role:    RoleUser,
			Content: &TextContent{Text: formatHistory(history[:pos])},
		},
	}
	c.history = msgs
	if _, err := c.ChatFull(""); err != nil {
		c.history = history
		return err
	}
	for s := len(c.latest) - 1; s >= 0; s-- {
		if c.latest[s].Type == ResContentTypeText {
			c.latest[s].Text = "【此前对话摘要】\n" + c.latest[s].Text
			break
		}
	}
	c.history[0] = c.history[len(c.history)-1]
	c.history = c.history[:1]
	for ; pos < hs; pos++ {
		c.history = append(c.history, history[pos])
	}
	return nil
}

func formatHistory(msgs []*Message) string {
	dat, _ := json.Marshal(msgs)
	return string(dat)
}

// shortJSON 把工具参数格式化为单行字符串，超长截断。供 runOneTool 之前 emit
// EvToolCall.Input 用。上限 200 字符（display concern）。
func shortJSON(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return truncate(string(raw), 200)
	}
	out, _ := json.Marshal(v)
	return truncate(string(out), 200)
}
