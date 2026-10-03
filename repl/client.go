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
		msgs = append(msgs, &Message{Role: RoleUser, Content: msg})
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
	c.history = append(c.history, &Message{Role: RoleAssistant, Content: res.Content})
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
			}
		}
	}
	c.latest = rcs
	res.Content = rcs
	c.history = append(c.history, &Message{Role: RoleAssistant, Content: res.Content})
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
		cmd []byte
		err error
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
			print := outp.NewStdout(outp.ColorBlue)
			msg := string(cmd)
		tool_next:
			err = c.ChatIncr(msg, print)
			if err != nil {
				print.ResetColor(outp.ColorRed)
				print.Write([]byte(err.Error()))
				print.Close()
				return
			}
			if c.execTool() {
				msg = ""
				goto tool_next
			}
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
		toolUsed = true
		ec, ok := BuiltIns[rc.Name]
		if !ok {
			toolResults = append(toolResults, &ResContent{
				Type:      ResContentTypeToolResult,
				ToolUseID: rc.ID,
				Result:    json.RawMessage(fmt.Sprintf(`{"error":"unknown tool %q"}`, rc.Name)),
			})
			continue
		}
		if err := json.Unmarshal(rc.Input, ec.Param); err != nil {
			toolResults = append(toolResults, &ResContent{
				Type:      ResContentTypeToolResult,
				ToolUseID: rc.ID,
				Result:    json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error())),
			})
			continue
		}
		result, err := ec.Process(ec.Param)
		if err != nil {
			toolResults = append(toolResults, &ResContent{
				Type:      ResContentTypeToolResult,
				ToolUseID: rc.ID,
				Result:    json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error())),
			})
			continue
		}
		data, err := json.Marshal(result)
		if err != nil {
			toolResults = append(toolResults, &ResContent{
				Type:      ResContentTypeToolResult,
				ToolUseID: rc.ID,
				Result:    json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error())),
			})
			continue
		}
		toolResults = append(toolResults, &ResContent{
			Type:      ResContentTypeToolResult,
			ToolUseID: rc.ID,
			Result:    data,
		})
	}
	if toolUsed {
		c.history = append(c.history, &Message{Role: RoleUser, Content: toolResults})
	}
	c.latest = c.latest[:0]
	return toolUsed
}
