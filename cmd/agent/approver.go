package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// StdinApprover は stdin/stdout でユーザーに承認を求める UserApprover。
// REPLモードで使用する。
// 重要: REPLと同じ *bufio.Reader を共有すること。
// 別々のバッファで同一stdinを読むとデータ競合が発生する。
type StdinApprover struct {
	reader *bufio.Reader
	writer io.Writer
}

// NewStdinApprover は StdinApprover を生成する。
// reader はREPLループと共有する *bufio.Reader を渡すこと。
func NewStdinApprover(r *bufio.Reader, w io.Writer) *StdinApprover {
	return &StdinApprover{
		reader: r,
		writer: w,
	}
}

// Approve はユーザーにツール実行の承認を求める。
// y/yes で承認、それ以外は拒否（デフォルト拒否）。
func (a *StdinApprover) Approve(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	// 引数の表示用整形（長すぎる場合は切り詰め）
	argsStr := string(args)
	if len(argsStr) > 200 {
		argsStr = argsStr[:200] + "..."
	}

	fmt.Fprintf(a.writer, "\n[permission] Tool %q を実行しますか？\n  引数: %s\n  [y/N]: ", toolName, argsStr)

	line, err := a.reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read approval: %w", err)
	}

	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes", nil
}
