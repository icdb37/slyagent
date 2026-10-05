package repl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slyagent/inp"
	"slyagent/outp"
	"strings"

	"github.com/go-resty/resty/v2"
)

type Client struct {
	cfg     *Config
	hc      *resty.Client
	history []*Message
	latest  []*ResContent
}

func New(cfg *Config) *Client {
	c := &Client{
		cfg: cfg,
		hc:  resty.New(),
	}
	return c
}

// msg 为 nil 时不追加用户消息，直接用 history 请求（用于工具调用循环续接）
func (c *Client) buildReqXllm(msg string) *ReqXllm {
	msgs := append([]*Message{}, c.history...)
	if msg != "" {
		msgs = append(msgs,
			&Message{
				Role:    RoleUser,
				Content: msg,
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

// ChatFull 问答，全量回答
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

const (
	prefixEvtType = "event: "
	prefixEvtData = "data: "
)

// ChatIncr 问答，增量回答。msg 为 nil 时不追加用户消息（工具循环续接场景）
func (c *Client) ChatIncr(msg string, w io.Writer) error {
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
		// fmt.Println(string(line))
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
			case "content_block_delta": // thinking 或者 text
				evtVal := &ResContentDelta{}
				if err := json.Unmarshal(evtData, evtVal); err != nil {
					return err
				}
				switch evtVal.Delta.Type {
				case ResContentDeltaTypeThinking: // 增量思考
					rc.Thinking += evtVal.Delta.Thinking
				case ResContentDeltaTypeText: // 增量文本
					rc.Text += evtVal.Delta.Text
					w.Write([]byte(evtVal.Delta.Text))
				case ResContentDeltaTypeSignature: // 增量前面
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

func (c *Client) Loop(ctx context.Context, mode int) {
	chqa := make(chan error, 1)
	if mode == 0 {
		go c.doqa_full(chqa)
	} else {
		go c.doqa_incr(chqa)
	}
	for {
		select {
		case <-ctx.Done():
			fmt.Println(ctx.Err())
			return
		case <-chqa:
			fmt.Println("问答退出")
			return
		}
	}
}

func (c *Client) doqa_full(ch chan error) {
	var (
		cmd []byte
		err error
		res string
	)
	defer func() {
		ch <- err
	}()
	r := bufio.NewReader(os.Stdin)
	for {
		os.Stdout.Write([]byte("> "))
		cmd, _, err = r.ReadLine()
		if err != nil {
			return
		}
		cmd = bytes.TrimSpace(cmd)
		if len(cmd) == 0 {
			continue
		}
		if cmd[0] != '/' {
			res, err = c.ChatFull(string(cmd))
			if err != nil {
				return
			}
			print := outp.NewStdout(outp.ColorBlue)
			print.Write([]byte(res))
			print.Close()
			continue
		}
		switch strings.ToLower(string(cmd)) {
		case "/q", "/quite", "/exit":
			return
		case "/clear", "/reset":
			c.reset()
		default:
			print := outp.NewStdout(outp.ColorRed)
			print.Write([]byte("未知命令: " + string(cmd)))
			print.Close()
		}
	}
}

func (c *Client) doqa_incr(ch chan error) {
	var (
		cmd string
		err error
	)
	defer func() {
		ch <- err
	}()
	for {
		outp.Prompt(outp.ColorWhite)
		cmd = inp.ReadLine()
		if len(cmd) == 0 {
			continue
		}
		if cmd[0] != '/' {
			if err := c.doCompress(); err != nil {
				outp.Print(err, outp.ColorRed)
				continue
			}
			print := outp.NewStdout(outp.ColorBlue)
			msg := string(cmd)
		tool_next:
			err = c.ChatIncr(msg, print)
			if err != nil {
				outp.Print(err, outp.ColorRed)
				return
			}
			if c.execTool() {
				msg = ""
				goto tool_next
			}
			print.Close()
			continue
		}
		switch strings.ToLower(cmd) {
		case "/q", "/quite", "/exit":
			return
		case "/clear", "/reset":
			c.reset()
		case "/compress":
			if err := c.doCompress(); err != nil {
				outp.Print(err, outp.ColorRed)
				continue
			}
		default:
			outp.Print("未知命令: "+cmd, outp.ColorYellow)
		}
	}
}

func (c *Client) reset() {
	if len(c.history) == 0 {
		return
	}
	c.history = c.history[:0]
}

func (c *Client) execTool() bool {
	if len(c.latest) == 0 {
		return false
	}
	toolResults := []*ResContent{}
	toolUsed := false
	for _, rc := range c.latest {
		if rc.Type != ResContentTypeToolUse {
			continue
		}
		c.history[len(c.history)-1].action = MessageActionToolCall
		toolUsed = true
		toolResult := &ResContent{
			Type:      ResContentTypeToolResult,
			ToolUseID: rc.ID,
		}
		toolResults = append(toolResults, toolResult)
		ec, ok := BuiltIns[rc.Name]
		if !ok {
			toolResult.Result = fmt.Sprintf(`{"error":"unknown tool %q"}`, rc.Name)
			continue
		}
		if err := json.Unmarshal(rc.Input, ec.Param); err != nil {
			toolResult.Result = fmt.Sprintf(`{"error":%q}`, err.Error())
			continue
		}
		result, err := ec.Process(ec.Param)
		if err != nil {
			toolResult.Result = fmt.Sprintf(`{"error":%q}`, err.Error())
			continue
		}
		data, err := json.Marshal(result)
		if err != nil {
			toolResult.Result = fmt.Sprintf(`{"error":%q}`, err.Error())
			continue
		}
		toolResult.Result = truncate(string(data), MessageToolResultMax)
	}
	if toolUsed {
		c.history = append(c.history,
			&Message{
				Role:    RoleUser,
				Content: toolResults,
				action:  MessageActionToolResult,
			})
	}
	c.latest = c.latest[:0]
	return toolUsed
}

func (c *Client) compressIndex() int {
	hs := len(c.history)
	if hs < MessageRecentNum*MessageRecentMul1 {
		return -1
	}

	// 缓存 message 字节大小
	for _, h := range c.history {
		if h.size != 0 {
			continue
		}
		data, _ := json.Marshal(h)
		h.size = len(data)
	}

	// 中间区：[N*MessageRecentMul1, N*MessageRecentMu2] 按总大小判断
	if hs <= MessageRecentNum*MessageRecentMu2 {
		totalSize := 0
		for _, h := range c.history {
			totalSize += h.size
		}
		if totalSize <= MessageHistorySize {
			return -1
		}
	}

	// 保留最近 MessageRecentNum 条；向前调整到合法切点
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
	// 下一条消息为用户消息，之前消息为完整消息
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
			Content: `你是对话压缩器。把以下对话历史压缩成简洁的中文要点，必须保留：

【意图】用户的目标、需求、已做的决定与偏好
【事实】涉及的文件路径、shell 命令、工具调用及关键结论
【状态】已完成、进行中、未完成的事项

只输出要点；不解释、不寒暄、不保留原话。`,
		},
		{
			Role:    RoleUser,
			Content: formatHistory(history[:pos]),
		},
	}
	c.history = msgs
	if _, err := c.ChatFull(""); err != nil {
		c.history = history // 失败恢复历史记录
		return err
	}
	for s := len(c.latest) - 1; s >= 0; s-- {
		if c.latest[s].Type == ResContentTypeText {
			c.latest[s].Text = "【此前对话摘要】\n" + c.latest[s].Text
			break
		}
	}
	outp.Print(fmt.Sprintf("[历史压缩：%d 条旧消息合并为 1 条摘要]\n", pos), outp.ColorYellow)
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

func (c *Client) processAssistantMessage(req *ReqXllm, res *ResXllm) {
	if !SaveThinking {
		// 不保留 思考过程
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
	c.history = append(c.history,
		req.Messages[len(req.Messages)-1],
		&Message{
			Role:    RoleAssistant,
			Content: res.Content,
			action:  MessageActionAssistant,
		})
}
