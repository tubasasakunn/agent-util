# 001: SLLM Function Calling 能力テスト

## 目的

Gemma 4 E2B（Q4_K_M量子化）がエージェントループに耐えうるFunction Callingを安定して返せるか検証する。
プロジェクトの最大リスク — 「SLLMのツール呼び出し品質」を設計着手前に把握する。

### 検証項目

| # | テスト | 確認すること |
|---|---|---|
| 1 | 単一ツール呼び出し | tool_callsが正しいJSON形式で返るか |
| 2 | 複数ツールからの選択 | 適切なツールを選択できるか |
| 3 | ツール不要な質問 | ツールを呼ばずにテキストで回答できるか |
| 4 | マルチターン | ツール結果を受け取った後、自然な応答を生成できるか |
| 5 | 構造化出力（JSON mode） | response_format指定でvalidなJSONを返せるか |
| 6 | 安定性（同一リクエスト5回） | 出力形式・ツール選択の一貫性 |

## 手段

- 対象API: `http://192.168.86.200:8000/v1/chat/completions`
- モデル: `gemma-4-E2B-it-Q4_K_M`（コンテキスト8192トークン）
- ツール: curlでOpenAI互換APIを直接叩き、生のレスポンスを記録
- temperature: 0.3（再現性重視）

## 結果

### 総合評価

| # | テスト | 結果 | 判定 |
|---|---|---|---|
| 1 | 単一ツール呼び出し | `tool_calls`に正しいJSON、`finish_reason: "tool_calls"` | **PASS** |
| 2 | 複数ツールからの選択 | `tool_calls: null`、contentにJSON文字列として出力（APIレベルでtool_callsにならない） | **FAIL** |
| 3 | ツール不要な質問 | テキストで「1+1は2です」と回答、ツールは呼ばない | **PASS** |
| 4 | マルチターン | ツール結果を自然な文章にまとめて回答 | **PASS** |
| 5 | 構造化出力（JSON mode） | validなJSON、キー名・値ともに正確 | **PASS** |
| 6 | 安定性（5回） | 5/5回ともtool_callsで同一の正しい出力 | **PASS** |

### 詳細

#### Test 1: 単一ツール呼び出し — PASS
```json
{
  "tool_calls": [
    {
      "id": "call_8357ac97",
      "type": "function",
      "function": {
        "name": "get_weather",
        "arguments": "{\"location\": \"東京\"}"
      }
    }
  ],
  "finish_reason": "tool_calls"
}
```
- `finish_reason`が`tool_calls`で正しい
- argumentsが有効なJSONで、日本語（東京）も正しくエンコード
- `id`フィールドも自動生成されている

#### Test 2: 複数ツールからの選択 — FAIL
```json
{
  "content": "{\"tool_calls\": [{\"name\": \"read_file\", \"arguments\": {\"path\": \"sample.txt\"}]}\n{\"tool_calls\": []}\n",
  "tool_calls": null,
  "finish_reason": "stop"
}
```
- **重大な問題**: `tool_calls`フィールドが`null`で、contentにJSON文字列として出力されている
- ツール選択自体は正しい（`read_file`を選んでいる）が、APIレベルのFunction Callingとして認識されていない
- さらにcontentのJSON自体も壊れている（閉じ括弧の対応が不正）
- **推測**: ツール数が3つに増えたことでモデルの出力形式が不安定になった可能性

#### Test 3: ツール不要な質問 — PASS
```
content: "1+1は2です。"
tool_calls: null
finish_reason: "stop"
```
- ツールが利用可能でも、不要なら呼ばない判断ができている

#### Test 4: マルチターン — PASS
```
content: "東京の天気は、気温22度、天気は晴れ、湿度45%です。"
```
- ツール結果のJSONを自然な日本語に変換できている
- エージェントループの「ツール結果→最終応答」フローが機能する

#### Test 5: 構造化出力 — PASS
```json
{"name": "Tokyo Tower", "height_m": 333, "built_year": 1958}
```
- validなJSON
- 値も事実として正確

#### Test 6: 安定性 — PASS (5/5)
```
Run 1: tool=get_weather args={"location": "東京"}
Run 2: tool=get_weather args={"location": "東京"}
Run 3: tool=get_weather args={"location": "東京"}
Run 4: tool=get_weather args={"location": "東京"}
Run 5: tool=get_weather args={"location": "東京"}
```
- temperature 0.3で完全に一貫した出力

### 追加検証: ルーター + 単一ツール パターン

Test 2の失敗を受けて、「ルーター（JSON mode）→サブエージェント（単一ツール）」の2段階パターンを検証。

| # | テスト | 結果 | 判定 |
|---|---|---|---|
| A | ルーター: ファイル読み取り | `read_file` + 正しい引数 + 理由を出力 | **PASS** |
| B | ルーター: 天気要求 | `get_weather` + `{"location":"大阪"}` | **PASS** |
| C | ルーター: ツール不要 | `"tool":"none"` と判断。ただし `{null}` と不正JSON | **PARTIAL** |
| D | 完全フロー3ステップ | ルーター→サブエージェント→最終応答、全段階成功 | **PASS** |
| E | ルーター安定性(5回) | 5/5回同一の正しい出力 | **PASS** |

#### Test A: ルーター（ファイル読み取り） — PASS
```json
{"tool": "read_file", "arguments": {"path": "sample.txt"}, "reasoning": "ユーザーは...read_fileツールを使用するのが適切である。"}
```
- 4つのツールから正しく`read_file`を選択
- 引数も正確
- 選択理由も論理的

#### Test B: ルーター（天気要求） — PASS
```json
{"tool": "get_weather", "arguments": {"location": "大阪"}, "reasoning": "ユーザーは大阪の天気を尋ねているため..."}
```

#### Test C: ルーター（ツール不要） — PARTIAL
```json
{"tool": "none", "arguments": {null}, "reasoning": "ユーザーは単純な算数の質問をしています...外部ツールは不要です。"}
```
- ツール不要の判断は正しい
- ただし `{null}` は不正なJSON（`null` が正しい）。パース補正が必要

#### Test D: 完全フロー — PASS
```
Step 1 ルーター: tool=get_weather, args={"location":"東京"}
Step 2 サブエージェント: tool_calls正常、finish_reason=tool_calls
Step 3 最終応答: 「東京の天気は、気温25度、曇り、湿度60%、風は南東 3m/sです。」
```
- 3ステップ全てが正常に動作
- エージェントループとして完全に機能する

#### Test E: ルーター安定性 — PASS (5/5)
```
Run 1-5: tool=read_file args={"path":"sample.txt"} （全て同一）
```

### 設計への示唆

1. **単一ツールのFunction Callingは安定** — 5/5で完全一致
2. **複数ツール提示は不安定** — ツール3個でtool_callsにならずcontent内に壊れたJSONで出力
3. **ルーターパターンで解決可能** — JSON modeでツール選択→単一ツールで実行の2段階が有効
4. **JSON modeは信頼できる** — ルーティング判断の品質は高い（4ツールから正確に選択）
5. **マルチターンは問題なし** — ツール結果フィードバック後の応答生成は安定
6. **軽微なパース補正は必要** — `{null}` のような不正JSONへの対応

### 結論: 推奨アーキテクチャ

```
ユーザー要求
  ↓
ルーター（JSON mode、ツール定義なし）
  → どのツールを使うか判断、引数も生成
  ↓
サブエージェント（選ばれた単一ツールのみ提示）
  → tool_callsで安定した呼び出し
  ↓
ツール実行結果をフィードバック
  ↓
最終応答生成
```

**APIコール数:** 最小2回（ルーター + サブエージェント）、ツール結果の応答生成を含めると3回。
SLLMの推論速度を考慮しても、信頼性の向上がコスト増を上回る。
