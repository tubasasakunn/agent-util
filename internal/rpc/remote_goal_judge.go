package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ai-agent/pkg/protocol"
)

// DefaultJudgeTimeout はリモートゴール判定呼び出しのデフォルトタイムアウト。
const DefaultJudgeTimeout = 30 * time.Second

// RemoteGoalJudge はラッパー側で実装された GoalJudge のプロキシ。
// engine.GoalJudge インターフェースを実装する。
type RemoteGoalJudge struct {
	name    string
	server  remoteSender
	timeout time.Duration
}

// NewRemoteGoalJudge は RemoteGoalJudge を生成する。
func NewRemoteGoalJudge(name string, server remoteSender) *RemoteGoalJudge {
	return &RemoteGoalJudge{name: name, server: server, timeout: DefaultJudgeTimeout}
}

// Name は識別名を返す。
func (j *RemoteGoalJudge) Name() string { return j.name }

// ShouldTerminate はラッパーへ judge.evaluate を送り、ゴール達成判定を返す。
// fail-open: エラー / 不正な応答はループを継続する（terminate=false）。
func (j *RemoteGoalJudge) ShouldTerminate(ctx context.Context, response string, turn int) (bool, string, error) {
	if j.server == nil {
		return false, "", fmt.Errorf("remote judge %q: server unavailable", j.name)
	}

	execCtx, cancel := context.WithTimeout(ctx, j.timeout)
	defer cancel()

	params := protocol.JudgeEvaluateParams{
		Name:     j.name,
		Response: response,
		Turn:     turn,
	}

	resp, err := j.server.SendRequest(execCtx, protocol.MethodJudgeEvaluate, params)
	if err != nil {
		return false, "", nil // fail-open
	}
	if resp == nil || resp.Error != nil {
		return false, "", nil // fail-open
	}

	var result protocol.JudgeEvaluateResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return false, "", nil // fail-open
	}

	return result.Terminate, result.Reason, nil
}
