package repl

import (
	"fmt"
	"os"
	"testing"
)

func TestReplClient(t *testing.T) {
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		t.Fatal("<LLM_API_KEY> not found")
	}
	cfg := &Config{
		ModelName: "MiniMax-M2.7",
		BaseURL:   "https://api.minimax.cn/anthropic/v1/messages",
		APIKey:    apiKey,
	}
	c := New(cfg)
	msg, err := c.ChatFull("数学鸡兔同笼问题，头共10个，腿共30只，求几只鸡几个兔")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(msg)
}
