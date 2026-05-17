package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// BuildBuiltinGoalJudge は仕様文字列から内蔵判定器を組み立てる (A2)。
//
// 仕様の文法:
//
//	"min_length:N"  — assistant 応答が N 文字以上なら terminate=true
//	"contains:KW"   — assistant 応答に KW が含まれていれば terminate=true
//
// 利用例 (JudgeConfig.Builtin):
//
//	"min_length:30"  — 30 文字以上で done
//	"contains:FINAL" — "FINAL" 含有で done
//
// 未対応の仕様文字列はエラー。
func BuildBuiltinGoalJudge(spec string) (GoalJudge, error) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid spec %q (want \"kind:value\")", spec)
	}
	switch parts[0] {
	case "min_length":
		n, err := strconv.Atoi(parts[1])
		if err != nil || n < 0 {
			return nil, fmt.Errorf("min_length: invalid number %q", parts[1])
		}
		return minLengthJudge(n), nil
	case "contains":
		kw := parts[1]
		if kw == "" {
			return nil, fmt.Errorf("contains: empty keyword")
		}
		return containsJudge(kw), nil
	default:
		return nil, fmt.Errorf("unknown builtin judge kind %q", parts[0])
	}
}

// minLengthJudge は応答が n 文字以上なら done を返す判定器。
// 短すぎる応答 (例: "OK" や "はい") をルーターの "completed" として扱わない用途。
func minLengthJudge(n int) GoalJudge {
	return GoalJudgeFunc(func(ctx context.Context, response string, turn int) (bool, string, error) {
		if len([]rune(response)) >= n {
			return true, fmt.Sprintf("min_length:%d satisfied", n), nil
		}
		return false, "", nil
	})
}

// containsJudge は応答が指定キーワードを含むなら done を返す判定器。
func containsJudge(kw string) GoalJudge {
	return GoalJudgeFunc(func(ctx context.Context, response string, turn int) (bool, string, error) {
		if strings.Contains(response, kw) {
			return true, fmt.Sprintf("contains:%q satisfied", kw), nil
		}
		return false, "", nil
	})
}
