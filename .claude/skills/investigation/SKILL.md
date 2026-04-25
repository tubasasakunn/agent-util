---
name: investigate
description: 実験・検証の計画、実行、結果記録を管理します。技術検証、能力テスト、PoC、ベンチマーク、比較実験を行う場合や、過去の検証結果を参照したい場合に使用してください。試す、テスト、検証、実験、動作確認、比較、ベンチマーク、といった話題で使用してください。
user-invocable: true
argument-hint: "[list | キーワード | 空=新規検証]"
allowed-tools:
  - Read
  - Write
  - Edit
  - Glob
  - Grep
  - Bash
---

# Investigation Log

引数: $ARGUMENTS

プロジェクトの技術検証・実験をディレクトリ単位で管理する。
記録は `.claude/skills/investigation/` に連番ディレクトリとして保存する。

## ワークフロー

引数に応じて3つのモードで動作する。

### モード1: 新規検証（引数なし）

新しい検証を開始する。

1. `.claude/skills/investigation/` の既存ディレクトリを確認し、次の連番を決定する
2. ユーザーと検証の目的・手段を確認する
3. 以下の構成でディレクトリとREADMEを作成する:
   ```
   .claude/skills/investigation/NNN_検証名/
   ├── README.md        ← 目的・手段・結果
   ├── *.sh / *.go / *  ← 検証スクリプト
   └── results/         ← 実行結果の保存先
   ```
4. 検証スクリプトを作成し、実行する
5. 結果をREADMEに記録する

### モード2: 一覧（引数が `list`）

`.claude/skills/investigation/` 内の全ディレクトリ（連番で始まるもの）を読み、以下の形式で一覧表示する:

```
001 - SLLM Function Calling能力テスト [完了]
002 - ルーターパターン検証 [進行中]
```

各ディレクトリのREADME.mdから目的と判定を抽出して表示する。

### モード3: 検索（その他の引数）

`.claude/skills/investigation/` 内のファイルをGrepで検索し、引数に関連する検証を見つけて内容を表示する。
新しい検証を始める前に、過去に同様の検証がないか確認する用途で使う。

## READMEテンプレート

```markdown
# NNN: 検証タイトル

## 目的

何を確認するのか。なぜこの検証が必要か。

### 検証項目

| # | テスト | 確認すること |
|---|---|---|
| 1 | ... | ... |

## 手段

- 対象: ...
- 方法: ...
- 条件: ...

## 結果

### 総合評価

| # | テスト | 結果 | 判定 |
|---|---|---|---|
| 1 | ... | ... | PASS / FAIL / PARTIAL |

### 詳細

(各テストの詳細な結果とデータ)

### 設計への示唆

(この検証結果がプロジェクト設計にどう影響するか)
```

## ディレクトリ命名規則

`NNN_英語snake_case` （NNNは連番3桁）

例:
- `001_sllm_function_calling_test`
- `002_router_pattern_validation`
- `003_context_window_benchmark`

## 注意事項

- 検証スクリプトは再実行可能な形で保存する（再現性）
- 結果のJSONファイルは `results/` サブディレクトリに保存する
- READMEの「設計への示唆」セクションで、結果がプロジェクト設計にどう反映されるべきかを明記する
- 検証結果から設計判断が導かれた場合は `/decision` で別途記録する
- **mockテストだけで終わらせず、必ず実機SLLM（Gemma 4 E2B）でも手動実行して結果を記録すること。** mockはロジックの正しさを保証するが、SLLMの実際の出力品質（JSON形式の正確さ、ツール選択の判断力、レイテンシ）は実機でしか検証できない。READMEに「実機検証」セクションを設け、以下のコマンドで実行した結果を記録する

## 実機SLLM検証の手順

### 接続情報
```bash
export SLLM_ENDPOINT="http://localhost:8080/v1/chat/completions"
export SLLM_API_KEY="sk-gemma4"
```

### 実行方法

**ワンショットモード（推奨）:**
```bash
SLLM_ENDPOINT="http://localhost:8080/v1/chat/completions" SLLM_API_KEY="sk-gemma4" \
  go run ./cmd/agent/ "プロンプト"
```

**REPLモード:**
```bash
SLLM_ENDPOINT="http://localhost:8080/v1/chat/completions" SLLM_API_KEY="sk-gemma4" \
  go run ./cmd/agent/
```

### サーバー疎通確認
```bash
curl -s http://localhost:8080/v1/models -H "Authorization: Bearer sk-gemma4"
```
→ モデル一覧が返ればOK。`Invalid or missing token` ならAPIキーを確認。接続拒否ならサーバー未起動。

### 検証時の注意
- SLLMはレスポンスに5〜60秒かかる（タスクの複雑さによる）。タイムアウトは120秒以上に設定する
- 英語プロンプトの方がJSON出力の精度が高い傾向がある
- ツール選択の正確性はプロンプトの明示性に依存する（「delegate a subtask to...」のように明確に指示すると正確）
