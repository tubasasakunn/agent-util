package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// isGitRepo は指定ディレクトリが git リポジトリかを判定する。
func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

// setupTestRepo はテスト用の一時 git リポジトリを作成する。
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	commands := [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "test"},
	}
	// 初期コミットを作成（worktreeには最低1つのコミットが必要）
	dummyFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(dummyFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("write dummy file: %v", err)
	}
	commands = append(commands,
		[]string{"git", "-C", dir, "add", "."},
		[]string{"git", "-C", dir, "commit", "-m", "initial"},
	)

	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %s: %v", args, string(out), err)
		}
	}
	return dir
}

func TestCreateWorktree_Success(t *testing.T) {
	repoDir := setupTestRepo(t)

	wt, err := createWorktree(repoDir)
	if err != nil {
		t.Fatalf("createWorktree: %v", err)
	}
	defer wt.cleanup()

	// worktree ディレクトリが存在する
	if _, err := os.Stat(wt.dir); os.IsNotExist(err) {
		t.Fatal("worktree directory does not exist")
	}

	// worktree 内に .git ファイルがある（worktreeでは.gitはファイル）
	gitPath := filepath.Join(wt.dir, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		t.Fatalf("worktree .git not found: %v", err)
	}
	if info.IsDir() {
		t.Error("worktree .git should be a file, not a directory")
	}

	// worktree 内にリポジトリのファイルがチェックアウトされている
	readmePath := filepath.Join(wt.dir, "README.md")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		t.Error("worktree should contain checked-out files")
	}
}

func TestWorktreeCleanup(t *testing.T) {
	repoDir := setupTestRepo(t)

	wt, err := createWorktree(repoDir)
	if err != nil {
		t.Fatalf("createWorktree: %v", err)
	}

	dir := wt.dir
	if err := wt.cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	// ディレクトリが削除されている
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("worktree directory should be removed after cleanup")
	}
}

func TestCreateWorktree_InvalidRepo(t *testing.T) {
	// git リポジトリでないディレクトリで失敗する
	dir := t.TempDir()

	_, err := createWorktree(dir)
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestWorktree_FileIsolation(t *testing.T) {
	repoDir := setupTestRepo(t)

	wt, err := createWorktree(repoDir)
	if err != nil {
		t.Fatalf("createWorktree: %v", err)
	}
	defer wt.cleanup()

	// worktree 内でファイルを変更しても元リポジトリに影響しない
	wtFile := filepath.Join(wt.dir, "new_file.txt")
	if err := os.WriteFile(wtFile, []byte("worktree only"), 0o644); err != nil {
		t.Fatalf("write to worktree: %v", err)
	}

	repoFile := filepath.Join(repoDir, "new_file.txt")
	if _, err := os.Stat(repoFile); !os.IsNotExist(err) {
		t.Error("file created in worktree should not appear in repo")
	}
}
