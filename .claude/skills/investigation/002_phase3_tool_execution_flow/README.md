# 002: Phase 3 ツール実行フロー統合テスト

## 目的

Phase 3で実装したルーター→ツール直接実行→最終応答のフローが実際のSLLM (Gemma 4 E2B) で正しく動作するか確認する。ADR-003で決定した「サブエージェントステップ省略（ルーター引数直接使用）」方式の実用性を検証する。

### 検証項目

| # | テスト | 確認すること |
|---|---|---|
| 1 | echoツール実行 | ルーターがechoを選択→実行→結果を自然言語で回答 |
| 2 | read_fileツール実行 | ルーターがread_fileを選択→実行→内容を回答 |
| 3 | ツール不要ケース | ルーターがtool=none→直接回答 |
| 4 | 安定性（5回） | 同一プロンプトで一貫した動作 |

## 手段

- 対象API: `http://192.168.86.200:8000/v1/chat/completions`
- モデル: `gemma-4-E2B-it-Q4_K_M`（コンテキスト8192トークン）
- 方法: `go run cmd/agent/main.go "プロンプト"` でワンショット実行
- ツール: echo, read_file（internal/tools/）

## 結果

### 総合評価

| # | テスト | 結果 | 判定 |
|---|---|---|---|
| 1 | echoツール実行 | ルーターがechoを選択→実行→「hello」を回答 | **PASS** |
| 2 | read_fileツール実行 | ルーターがread_fileを選択→go.modを読み取り→内容を回答 | **PASS** |
| 3 | ツール不要ケース | ルーターがtool=none→「2」と直接回答 | **PASS** |
| 4 | 安定性（5回） | 5/5回とも正しくechoツールを選択・実行 | **PASS** |

### 詳細

#### Test 1: echoツール実行 — PASS

```
入力: "hello とechoして"
出力: "hello"
```

- ルーターがechoツールを正しく選択
- 引数 `{"message": "hello"}` を正しく生成
- ツール実行結果をそのまま最終応答として返却

#### Test 2: read_fileツール実行 — PASS

```
入力: "go.mod の中身を読んで"
出力: "go 1.25.1"
```

- ルーターがread_fileツールを正しく選択
- パス `go.mod` を正しく推定
- ファイル内容を読み取り、内容を自然言語で回答

#### Test 3: ツール不要ケース — PASS

```
入力: "1+1は?"
出力: "2"
```

- ルーターがtool=noneを正しく判断
- chatStepで直接回答を生成

#### Test 4: 安定性 — PASS (5/5)

```
Run 1: world
Run 2: world
Run 3: world
Run 4: world
Run 5: world
```

- 5回とも同一の正しい出力

### 検証中に発見・修正した問題

#### 問題1: tool_calls.function.arguments の形式

**症状:** ツール実行後の2回目のルーター呼び出しで500エラー

**原因:** OpenAI互換APIの `tool_calls.function.arguments` はJSON**文字列**(`"{\"key\":\"val\"}"`)であるべきだが、合成メッセージでJSONオブジェクト(`{"key":"val"}`)を送信していた

**修正:** `ToolCallMessage()` で `json.Marshal(string(arguments))` によりJSON文字列に変換

#### 問題2: SLMの連結JSONオブジェクト出力

**症状:** ルーターの出力パースが `invalid character '"' after top-level value` で失敗

**原因:** SLMがJSON modeで2つの別々のJSONオブジェクトを改行区切りで出力:
```
{"tool": "echo", "arguments": {"message": "hello"}}
{"reasoning": "user wants echo"}
```

**修正:** `FixJSON()` に `mergeJSONObjects()` を追加。改行区切りの複数JSONオブジェクトを1つにマージ

### 設計への示唆

1. **ADR-003（ルーター引数直接使用）は実用的** — 4テスト全PASSでサブエージェントステップなしで安定動作
2. **FixJSONの拡張は継続的に必要** — SLMの出力パターンは予測困難。新しいパターンが見つかるたびに追加が必要
3. **OpenAI互換APIの細かい仕様に注意** — arguments の型（string vs object）など、ドキュメントに明示されない暗黙の仕様がある
4. **ツール結果のフィードバックループは正常動作** — ツール実行結果が会話履歴に追加され、次のルーターステップで正しく参照されている
