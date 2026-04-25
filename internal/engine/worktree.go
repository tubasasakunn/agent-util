package engine

import (
	"fmt"
	"os"
	"os/exec"
)

// worktree は一時的な git worktree を管理する。
// サブエージェントのファイルシステム分離に使用する。
type worktree struct {
	dir     string // worktree ディレクトリのパス
	repoDir string // 元リポジトリのディレクトリ
}

// createWorktree は現在の HEAD から detached な worktree を作成する。
// repoDir は git リポジトリのルートディレクトリ。
func createWorktree(repoDir string) (*worktree, error) {
	dir, err := os.MkdirTemp("", "agent-worktree-*")
	if err != nil {
		return nil, fmt.Errorf("create worktree tmpdir: %w", err)
	}

	// git worktree add --detach <dir>
	cmd := exec.Command("git", "-C", repoDir, "worktree", "add", "--detach", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("git worktree add: %s: %w", string(out), err)
	}

	return &worktree{dir: dir, repoDir: repoDir}, nil
}

// cleanup は worktree を削除する。
func (w *worktree) cleanup() error {
	cmd := exec.Command("git", "-C", w.repoDir, "worktree", "remove", "--force", w.dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		// ベストエフォートでディレクトリを直接削除
		os.RemoveAll(w.dir)
		return fmt.Errorf("git worktree remove: %s: %w", string(out), err)
	}
	return nil
}
