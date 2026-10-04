package main

import (
	"context"
	"os"
	"os/signal"
	"slyagent/repl"
)

func main() {
	cfg := &repl.Config{
		ModelName: "MiniMax-M2.7",
		BaseURL:   "https://api.minimax.cn/anthropic/v1/messages",
		APIKey:    os.Getenv("LLM_API_KEY"),
		MaxTokens: 1024 * 10,
	}
	c := repl.New(cfg)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Kill, os.Interrupt)
	defer cancel()
	c.Loop(ctx, 1)
}
