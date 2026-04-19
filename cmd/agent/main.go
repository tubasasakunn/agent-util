package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"ai-agent/internal/engine"
	"ai-agent/internal/llm"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg := parseFlags()
	envCfg := parseEnv()

	client := llm.NewClient(
		llm.WithEndpoint(envCfg.endpoint),
		llm.WithModel(envCfg.model),
		llm.WithAPIKey(envCfg.apiKey),
	)
	eng := engine.New(client, engine.WithMaxTurns(cfg.maxTurns))

	// 引数ありならワンショットモード
	if cfg.prompt != "" {
		result, err := eng.Run(ctx, cfg.prompt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(result.Response)
		return
	}

	// 引数なしならREPLモード
	if err := runREPL(ctx, eng); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runREPL(ctx context.Context, eng *engine.Engine) error {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, "> ")
		if !scanner.Scan() {
			break // EOF
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			break
		}

		reqCtx, reqCancel := context.WithCancel(ctx)
		result, err := eng.Run(reqCtx, line)
		reqCancel()

		if err != nil {
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "\n(interrupted)")
				return nil
			}
			return fmt.Errorf("run: %w", err)
		}
		fmt.Println(result.Response)
		fmt.Println()
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	return nil
}

type flagConfig struct {
	prompt   string
	maxTurns int
}

func parseFlags() flagConfig {
	maxTurns := flag.Int("max-turns", 10, "1回のRunで許可する最大ターン数")
	flag.Parse()

	return flagConfig{
		prompt:   strings.Join(flag.Args(), " "),
		maxTurns: *maxTurns,
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
