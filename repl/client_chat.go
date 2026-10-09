package repl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slyagent/enum"
)

// ChatFull 问答，全量回答。
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
		if c.Type == enum.ResContentTypeText {
			text = c.Text
		}
	}
	c.processAssistantMessage(param, res)
	return text, nil
}

// IncrChat 流式响应
func (c *Client) IncrChat(msg string) error {
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
				case enum.ResContentDeltaTypeThinking:
					rc.Thinking += evtVal.Delta.Thinking
				case enum.ResContentDeltaTypeText:
					rc.Text += evtVal.Delta.Text
					c.evtDeltaText(evtVal.Delta.Text)
				case enum.ResContentDeltaTypeSignature:
					rc.Signature = evtVal.Delta.Signature
				case enum.ResContentDeltaTypeInputJson:
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

func (c *Client) evtDeltaText(delta string) {
	c.out <- DecodeEvtRepl(&EvtPayloadAssistantChunk{Delta: delta}, AssistantChunk)
}
