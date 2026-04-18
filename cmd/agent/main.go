package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"ai-agent/internal/llm"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	jsonMode := flag.Bool("json", false, "JSON modeで応答を取得する")
	flag.Parse()

	prompt := strings.Join(flag.Args(), " ")
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: agent [-json] <question>")
		os.Exit(1)
	}

	cfg := parseEnv()

	client := llm.NewClient(
		llm.WithEndpoint(cfg.endpoint),
		llm.WithModel(cfg.model),
		llm.WithAPIKey(cfg.apiKey),
	)

	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: llm.StringPtr(prompt)},
		},
	}
	if *jsonMode {
		req.ResponseFormat = &llm.ResponseFormat{Type: "json_object"}
	}

	resp, err := client.ChatCompletion(ctx, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if len(resp.Choices) > 0 {
		fmt.Println(resp.Choices[0].Message.ContentString())
	}
}

type envConfig struct {
	endpoint string
	model    string
	apiKey   string
}

func parseEnv() envConfig {
	cfg := envConfig{
		endpoint: os.Getenv("SLLM_ENDPOINT"),
		model:    os.Getenv("SLLM_MODEL"),
		apiKey:   os.Getenv("SLLM_API_KEY"),
	}
	if cfg.endpoint == "" {
		cfg.endpoint = "http://localhost:8000/v1/chat/completions"
	}
	if cfg.model == "" {
		cfg.model = "gemma-4-E2B-it-Q4_K_M"
	}
	return cfg
}
