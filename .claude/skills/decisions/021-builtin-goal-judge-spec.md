---
id: "021"
title: JudgeConfig.builtin で内蔵 GoalJudge を spec 文字列から組み立てる
date: 2026-05-17
status: accepted
---

## コンテキスト

SLLM では「judge を自分で書かないとループが終わらない」現象が頻発する (A2):

> `finish` 判定が外注。judge を自分で書かないと「ループが終わらない」現象が
> 起きる。デフォルトで「自然言語が一定長返ったら完了」程度のサニティチェック
> が欲しい。

利用者は `judge.register` で外部ハンドラを登録できるが、毎回書くのは煩雑。
よくあるパターン (応答長で打ち切り、特定キーワードで完了) は SDK 内蔵すべき。

## 検討した選択肢

### A. SDK 側でデフォルト Judge を register する
Python / JS / Swift それぞれが「30 文字以上で done」を内部 register する。
**SDK 間で挙動が分散しがち** で、Go コア側の仕様としては不明瞭。

### B. JudgeConfig.builtin = "min_length:30" の spec 文字列
Go コアに `BuildBuiltinGoalJudge(spec string)` を新設し、
`judge.builtin` から組み立てる。SDK は何もせず、AgentConfig で名前指定するだけ。

### C. 専用 struct (`MinLengthJudge: int`, `ContainsJudge: string`)
構造化されているが、Judge の種類が増えるたびに protocol struct を拡張する
必要がある。OpenRPC スキマの維持コストが上がる。

## 判断

**B を採用する** (spec 文字列パーサパターン)。

- `protocol.JudgeConfig.Builtin string` を追加 (omitempty)
- `Name` と `Builtin` の優先順位は **Name > Builtin** (明示登録の Judge が優先)
- `internal/engine/builtin_judge.go` の `BuildBuiltinGoalJudge(spec string)`:
  - `"min_length:N"` — 応答 (rune 数) が N 以上で done
  - `"contains:KW"` — 応答に KW を含めば done
- 未知の spec はエラー (`unknown builtin judge kind`)
- 将来追加例: `"regex:^FINAL.*$"`, `"json_valid"` など

Swift SDK 側:

```swift
AgentConfig(judge: JudgeConfig(builtin: "min_length:30"))
```

## 理由

- **拡張性**: 新しい spec を Go 側に 1 関数足すだけで全 SDK から使える
- **OpenRPC スキマが安定**: JudgeConfig に Name と Builtin の 2 文字列フィールド
  だけ。type-safe な struct を増やすより JSON Schema 維持が楽
- **小型モデル運用のベストプラクティスを SDK 標準で配布できる**: README の
  「小型モデル向けチューニング指針」で `min_length:30` を推奨できる
- **テスト容易性**: 単体テストは `BuildBuiltinGoalJudge("min_length:30")` を直接
  呼ぶだけで完結。E2E 不要

## 影響

- **後方互換性**: 既存 `JudgeConfig(name: "...")` 経路は無変更
- **Swift SDK の `JudgeConfig.name` を Optional<String> 化**: name="" のとき
  Go 側の `if p.Judge.Name != ""` ブランチに入らない問題を解消するため
  (Swift Codable は空文字でもエンコードするので omitempty が効かない)
- **Python / JS SDK の追従**: 同じ `JudgeConfig(builtin=...)` をサポートする
  プロパティ追加が必要 (次回フォローアップ)
- **将来の `min_length` の単位**: 現状 rune (Unicode コードポイント) 数。
  英語の単語 / 日本語の文字 のどちらも 30 で妥当な閾値になる
