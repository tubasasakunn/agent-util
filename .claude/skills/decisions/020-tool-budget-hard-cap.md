---
id: "020"
title: ToolScopeConfig.tool_budget による同一ツール連続呼び出しの hard cap
date: 2026-05-17
status: accepted
---

## コンテキスト

SLLM (Gemma 4 E2B / Qwen 1.5B / Llama 3.2 3B クラス) で発生する典型的な
暴走パターンとして「同じツールを何度も呼び続ける」がある (A1, A4):

> 「サイコロを 1 回だけ振って」と言っても 6 回振り続けた。
> systemPrompt に「1 回しか呼ぶな」と書いても無視される。

systemPrompt の指示は SLLM では確実に守られない。defensive な利用者のために
**SDK レベルで hard cap** を設けたい。

## 検討した選択肢

### A. systemPrompt のテンプレートで案内する
PromptBuilder セクションに「呼び出し履歴: tool_x: 3 回」を埋め込み、
SLLM 自身に判断させる。**実測で効果が薄い** (小型モデルは履歴を読まない)。

### B. ToolScopeConfig.tool_budget で予算超過したツールをルーターから除外
ツール呼び出し回数を Engine 内で per-run トラッキングし、上限到達後は
**ルーターに提示するツール一覧から外す**。提示されなければ選びようがない。

### C. 強制 finish ツールの自動注入
予算超過時に強制 `finish` ツールを差し込む。意味的に「予算オーバー」を
LLM に伝える方法だが、ツール枠を 1 つ消費する副作用がある。

## 判断

**B を採用する**。

- `protocol.ToolScopeConfig.ToolBudget map[string]int` を追加
- `engine.ToolScope.ToolBudget` も同様に追加
- `Engine` に `toolCalls map[string]int` を追加 (Run() 冒頭でリセット)
- `executeAndRecord` 内で呼び出し成功時にインクリメント
- `Registry.ScopedFormatForPromptWithCalls(scope, currentCalls)` で
  予算超過のツールを除外して prompt 生成
- 既存 `ScopedFormatForPrompt(scope)` は currentCalls=nil 経路で
  予算除外なしの旧挙動を維持

設定例:

```swift
AgentConfig(
    toolScope: ToolScopeConfig(
        maxTools: 5,
        includeAlways: ["finish"],
        toolBudget: ["shell": 1, "fetch": 3]
    )
)
```

## 理由

- **「見せない」が最も確実**: SLLM はリストにないツールは絶対に選ばない
- **systemPrompt 改善より低コスト**: PromptBuilder の階層、SLLM 個別の癖、
  プロンプトインジェクション耐性などを考えなくて済む
- **既存 ToolScope の延長**: `maxTools` / `includeAlways` と同じ「動的に
  prompt を絞る」抽象に乗っている。実装が小さい
- **per-run リセット**: 会話セッション全体でのカウントだと、長期セッションで
  上限到達後にツールが使えなくなる。`agent.run` 単位の判断が妥当

## 影響

- **後方互換性**: `tool_budget` 未指定なら旧挙動
- **0 を渡した場合**: 「初回から提示されない」=「最初から禁止に近い動作」になる。
  完全に禁止するには `permission.deny` を使う方が明示的
- **`always_include` との干渉**: budget 超過したツールが `includeAlways` に
  入っているとどうなるか → 現実装では除外が先に走り、`includeAlways` でも
  プロンプトから消える (利用者が「常に出す」より「予算が優先」を選んだとみなす)
- **削減効果**: 「再帰的に同じツールを呼び続けて max_turns に当たる」シナリオ
  でターン数を 1〜3 程度に絞れる
- **A2 (内蔵 Judge) と相補的**: budget で「これ以上ツール呼ばせない」、
  Judge で「これ以上ターン回さない」の二段構え
