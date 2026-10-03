package repl

import (
	"bufio"
	"bytes"
	"context"
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
}

func New(cfg *Config) *Client {
	c := &Client{
		cfg: cfg,
		hc:  resty.New(),
	}
	return c
}

// ChatFull 问答，全量回答
func (c *Client) ChatFull(msg string) (string, error) {
	msgs := append(c.history, &Message{Role: RoleUser, Content: msg})
	param := &ReqXllm{
		Messages:    msgs,
		Model:       c.cfg.ModelName,
		MaxTokens:   c.cfg.MaxTokens,
		Temperature: c.cfg.Temperature,
		Stream:      false,
	}
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

// ChatIncr 问答，增量回答
func (c *Client) ChatIncr(msg string, w io.Writer) error {
	return nil
}

func (c *Client) Loop(ctx context.Context) {
	chqa := make(chan error, 1)
	go c.doqa(chqa)
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
func (c *Client) doqa(ch chan error) {
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

func (c *Client) reset() {
	if len(c.history) == 0 {
		return
	}
	c.history = c.history[:0]
}
