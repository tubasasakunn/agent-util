package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"ai-agent/internal/engine"
	"ai-agent/internal/llm"
	"ai-agent/internal/tools/readfile"
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
		llm.WithLogWriter(os.Stderr),
	)
	// デフォルトパーミッションポリシー: ReadOnly は自動承認、それ以外は ask
	defaultPolicy := engine.PermissionPolicy{}

	opts := []engine.Option{
		engine.WithMaxTurns(cfg.maxTurns),
		engine.WithTokenLimit(envCfg.contextSize),
		engine.WithTools(
			readfile.New(),
		),
		engine.WithLogWriter(os.Stderr),
		engine.WithPermissionPolicy(defaultPolicy),
	}

	// 引数ありならワンショットモード（UserApprover なし → ask は fail-closed で拒否）
	if cfg.prompt != "" {
		eng := engine.New(client, opts...)
		result, err := eng.Run(ctx, cfg.prompt)
		if err != nil {
			handleRunError(err)
			os.Exit(1)
		}
		fmt.Println(result.Response)
		return
	}

	// 引数なしならREPLモード（StdinApprover でユーザー確認）
	// 重要: stdinの *bufio.Reader をREPLとApproverで共有する
	stdinReader := bufio.NewReader(os.Stdin)
	opts = append(opts, engine.WithUserApprover(NewStdinApprover(stdinReader, os.Stderr)))
	eng := engine.New(client, opts...)
	if err := runREPL(ctx, eng, stdinReader); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// handleRunError はRun()のエラーを分類して表示する。
func handleRunError(err error) {
	var tw *engine.TripwireError
	if errors.As(err, &tw) {
		fmt.Fprintf(os.Stderr, "[TRIPWIRE] %s: %s\n", tw.Source, tw.Reason)
		fmt.Fprintln(os.Stderr, "Agent stopped for safety.")
		return
	}
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
}

func runREPL(ctx context.Context, eng *engine.Engine, reader *bufio.Reader) error {
	for {
		fmt.Fprint(os.Stderr, "> ")
		rawLine, err := reader.ReadString('\n')
		if err != nil {
			break // EOF
		}
		line := strings.TrimSpace(rawLine)
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
			var tw *engine.TripwireError
			if errors.As(err, &tw) {
				fmt.Fprintf(os.Stderr, "\n[TRIPWIRE] %s: %s\n", tw.Source, tw.Reason)
				fmt.Fprintln(os.Stderr, "Agent loop stopped for safety.")
				return nil
			}
			return fmt.Errorf("run: %w", err)
		}
		fmt.Println(result.Response)
		fmt.Println()
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
	endpoint    string
	model       string
	apiKey      string
	contextSize int
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
	cfg.contextSize = 8192
	if s := os.Getenv("SLLM_CONTEXT_SIZE"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			cfg.contextSize = n
		}
	}
	return cfg
}
