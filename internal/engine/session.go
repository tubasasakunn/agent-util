package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-agent/internal/llm"
)

// ProgressFile はマルチセッションのタスク進捗を追跡する。
type ProgressFile struct {
	Tasks     []TaskEntry `json:"tasks"`
	StartedAt string      `json:"started_at"`
	UpdatedAt string      `json:"updated_at"`
}

// TaskEntry は進捗ファイル内の個別タスク。
type TaskEntry struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"` // "pending", "in_progress", "done", "failed"
	Summary     string `json:"summary,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// SessionConfig は SessionRunner の設定。
type SessionConfig struct {
	ProgressPath string   // 進捗ファイルのパス
	MaxSessions  int      // 最大ループ回数
	EngineOpts   []Option // 各 Engine に適用するオプション
	LogWriter    io.Writer
}

// SessionRunner は Ralph Wiggum ループを管理する。
// ファイルシステムベースの状態継続で、8K コンテキストを超えるタスクを処理する。
type SessionRunner struct {
	completer    llm.Completer
	engineOpts   []Option
	progressPath string
	maxSessions  int
	logw         io.Writer
}

// NewSessionRunner は SessionRunner を生成する。
func NewSessionRunner(completer llm.Completer, cfg SessionConfig) *SessionRunner {
	maxSessions := cfg.MaxSessions
	if maxSessions <= 0 {
		maxSessions = 20
	}
	return &SessionRunner{
		completer:    completer,
		engineOpts:   cfg.EngineOpts,
		progressPath: cfg.ProgressPath,
		maxSessions:  maxSessions,
		logw:         cfg.LogWriter,
	}
}

// RunLoop は Ralph Wiggum ループを実行する。
// Phase 1: 進捗ファイルを初期化（存在しなければ作成）。
// Phase 2: pending/failed のタスクを1つ選び、新 Engine で実行し、結果を記録する。
// 全タスク完了または maxSessions 到達で終了する。
func (sr *SessionRunner) RunLoop(ctx context.Context, tasks []string) (*ProgressFile, error) {
	progress, err := sr.loadOrCreateProgress(tasks)
	if err != nil {
		return nil, fmt.Errorf("initialize progress: %w", err)
	}

	for session := 0; session < sr.maxSessions; session++ {
		select {
		case <-ctx.Done():
			return progress, ctx.Err()
		default:
		}

		task, idx := sr.nextPendingTask(progress)
		if task == nil {
			sr.logf("[session] 全タスク完了 (%d sessions)", session)
			return progress, nil
		}

		sr.logf("[session] %d/%d: タスク %q を開始", session+1, sr.maxSessions, task.Description)

		progress.Tasks[idx].Status = "in_progress"
		progress.Tasks[idx].UpdatedAt = time.Now().Format(time.RFC3339)
		if err := sr.saveProgress(progress); err != nil {
			return progress, fmt.Errorf("save progress: %w", err)
		}

		result, runErr := sr.runTask(ctx, task, progress)

		now := time.Now().Format(time.RFC3339)
		if runErr != nil {
			progress.Tasks[idx].Status = "failed"
			progress.Tasks[idx].Summary = runErr.Error()
			sr.logf("[session] タスク %q 失敗: %s", task.Description, runErr)
		} else {
			progress.Tasks[idx].Status = "done"
			progress.Tasks[idx].Summary = truncateString(result.Response, 500)
			sr.logf("[session] タスク %q 完了 (%d turns)", task.Description, result.Turns)
		}
		progress.Tasks[idx].UpdatedAt = now
		progress.UpdatedAt = now

		if err := sr.saveProgress(progress); err != nil {
			return progress, fmt.Errorf("save progress: %w", err)
		}
	}

	// pending タスクが残っている場合
	return progress, fmt.Errorf("session loop: max sessions (%d) reached with pending tasks", sr.maxSessions)
}

// runTask は1つのタスクを新しい Engine で実行する。
func (sr *SessionRunner) runTask(ctx context.Context, task *TaskEntry, progress *ProgressFile) (*Result, error) {
	sysPrompt := sr.buildSessionPrompt(task, progress)

	opts := make([]Option, len(sr.engineOpts))
	copy(opts, sr.engineOpts)
	opts = append(opts, WithSystemPrompt(sysPrompt))
	if sr.logw != nil {
		opts = append(opts, WithLogWriter(sr.logw))
	}

	eng := New(sr.completer, opts...)
	return eng.Run(ctx, task.Description)
}

// buildSessionPrompt は現在のタスクと進捗状況を含むシステムプロンプトを構築する。
func (sr *SessionRunner) buildSessionPrompt(task *TaskEntry, progress *ProgressFile) string {
	var sb strings.Builder
	sb.WriteString("You are working on a specific task. Complete it thoroughly.\n\n")
	sb.WriteString("## Current Task\n")
	sb.WriteString(task.Description)
	sb.WriteString("\n\n## Progress\n")

	for _, t := range progress.Tasks {
		switch t.Status {
		case "done":
			sb.WriteString(fmt.Sprintf("- [x] %s: %s", t.ID, t.Description))
			if t.Summary != "" {
				sb.WriteString(fmt.Sprintf(" — %s", t.Summary))
			}
			sb.WriteString("\n")
		case "in_progress":
			sb.WriteString(fmt.Sprintf("- [>] %s: %s (current)\n", t.ID, t.Description))
		case "failed":
			sb.WriteString(fmt.Sprintf("- [!] %s: %s (failed: %s)\n", t.ID, t.Description, t.Summary))
		default:
			sb.WriteString(fmt.Sprintf("- [ ] %s: %s\n", t.ID, t.Description))
		}
	}

	return sb.String()
}

// loadOrCreateProgress は進捗ファイルを読み込むか新規作成する。
func (sr *SessionRunner) loadOrCreateProgress(tasks []string) (*ProgressFile, error) {
	data, err := os.ReadFile(sr.progressPath)
	if err == nil {
		var progress ProgressFile
		if err := json.Unmarshal(data, &progress); err != nil {
			return nil, fmt.Errorf("parse progress file: %w", err)
		}
		return &progress, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read progress file: %w", err)
	}

	now := time.Now().Format(time.RFC3339)
	progress := &ProgressFile{
		StartedAt: now,
		UpdatedAt: now,
	}
	for i, desc := range tasks {
		progress.Tasks = append(progress.Tasks, TaskEntry{
			ID:          fmt.Sprintf("task-%d", i+1),
			Description: desc,
			Status:      "pending",
		})
	}
	if err := sr.saveProgress(progress); err != nil {
		return nil, err
	}
	return progress, nil
}

// saveProgress は進捗をファイルに書き込む。
// アトミック書き込み（一時ファイル → rename）でクラッシュ耐性を確保する。
func (sr *SessionRunner) saveProgress(progress *ProgressFile) error {
	data, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal progress: %w", err)
	}

	dir := filepath.Dir(sr.progressPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create progress dir: %w", err)
	}

	// アトミック書き込み: 一時ファイルに書いてrename
	tmp := sr.progressPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write progress tmp: %w", err)
	}
	if err := os.Rename(tmp, sr.progressPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename progress: %w", err)
	}
	return nil
}

// nextPendingTask は次に実行すべきタスクを返す。
// pending → failed の優先度で探索する。
func (sr *SessionRunner) nextPendingTask(progress *ProgressFile) (*TaskEntry, int) {
	// まず pending を探す
	for i := range progress.Tasks {
		if progress.Tasks[i].Status == "pending" {
			return &progress.Tasks[i], i
		}
	}
	// 次に failed を再試行
	for i := range progress.Tasks {
		if progress.Tasks[i].Status == "failed" {
			return &progress.Tasks[i], i
		}
	}
	return nil, -1
}

func (sr *SessionRunner) logf(format string, args ...any) {
	if sr.logw != nil {
		fmt.Fprintf(sr.logw, format+"\n", args...)
	}
}

// truncateString は文字列を最大 n 文字に切り詰める。
func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
