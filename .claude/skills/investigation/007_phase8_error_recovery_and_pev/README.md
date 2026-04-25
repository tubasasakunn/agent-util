# 007: Phase 8 エラー回復と PEV サイクルの検証

## 目的

Phase 8 で実装したエラー分類・stepリトライ・連続失敗キャップ・PEVサイクルが
実機SLLM (Gemma 4 E2B) で正しく動作するかを確認する。

mockテストでは論理的な正しさを保証しているが、SLLMの実際の出力特性
（不安定なJSON、空レスポンス、ツール選択ミス）に対してエラー回復が機能するかは
実機でしか検証できない。

### 検証項目

| # | テスト | 確認すること |
|---|---|---|
| 1 | 正常なツール実行 | エラー回復を入れても正常フローが壊れていないこと |
| 2 | LLM回復パターン | ツール実行エラー時にLLMが自己修正すること |
| 3 | 連続失敗の安全停止 | 常に失敗するツールで安全停止すること |
| 4a | Verifier 統合 (パス) | Verifier がパスする正常フロー |
| 4b | Verifier 統合 (失敗→修正) | 検証失敗時にLLMが修正を試みること |

## 手段

- 対象: `cmd/agent/` のワンショットモードおよびカスタム検証スクリプト
- 方法: 各シナリオ用の検証スクリプトを実行
- 条件: SLLM_ENDPOINT=http://localhost:8080/v1/chat/completions, SLLM_API_KEY=sk-gemma4

## 結果

### 総合評価

| # | テスト | 結果 | 判定 |
|---|---|---|---|
| 1 | 正常なツール実行 | read_file → none → 応答の3ステップが正常完了。2 turns, 3743 tokens | PASS |
| 2 | LLM回復パターン | read_file(directory) → エラー → LLMが自己修正して応答 | PASS |
| 3 | 連続失敗の安全停止 | SLLMが1回の失敗で自己修正。キャップ発動せず。ユニットテストで論理は検証済み | PARTIAL |
| 4a | Verifier 統合 (パス) | `[verify] echo 検証パス` が正しく出力。PEVサイクル正常動作 | PASS |
| 4b | Verifier 統合 (失敗→修正) | "hello world" → 検証失敗 → LLMが "HELLO WORLD" に修正 → 検証パス | PASS |

### 詳細

#### テスト 1: 正常なツール実行
```
read_file cmd/agent/main.go → 2726 bytes → router selects "none" → chatStep → completed
2 turns, 3743 tokens, 正常終了
```
Phase 8 のエラー回復ロジックを追加しても正常フローに影響なし。

#### テスト 2: LLM回復パターン (ツール実行エラー)
```
User: "List files in internal/engine/"
→ Router selects read_file → ディレクトリなのでエラー
→ "Error: failed to read file: read internal/engine: is a directory" が履歴に追加
→ Router selects "none" → LLMが自己修正して応答
```
toolStep 内のエラー情報化パターン（error → ToolResultMessage → Continue）が
LLM の自己修正を支えていることを確認。

#### テスト 3: 連続失敗の安全停止
```
broken_tool (常にエラー) のみ登録
→ Router が broken_tool を選択 → エラー (consecutive=1)
→ Router が self-correct して "none" を選択 → chatStep → completed
```
SLLMは1回の失敗で自己修正する能力がある。連続失敗キャップは発動しなかったが、
これはSLLMの自己修正能力の高さを示す。キャップの論理的正しさはユニットテスト
（TestRun_ConsecutiveFailures_StopsAtLimit）で保証済み。

#### テスト 4a: Verifier 統合 (パス)
```
echo("HELLO WORLD") → [verify] echo 検証パス → none → completed
PEV cycle: Plan(router) → Execute(echo) → Verify(uppercase_check: PASS)
2 turns, verifier_calls=1
```

#### テスト 4b: Verifier 統合 (失敗→修正) **最重要テスト**
```
echo("hello world") → [verify] echo 検証失敗
  "Output must be ALL UPPERCASE. Got: 'hello world'"
→ [Verification Failed] メッセージが履歴に追加
→ Router が修正: echo("HELLO WORLD") → [verify] echo 検証パス
→ none → completed
3 turns, verifier_calls=2
```
PEVサイクルの Verify → 修正ループが実機SLLMで正常動作することを確認。
LLMは検証失敗メッセージを正しく理解し、引数を修正して再実行した。

### 設計への示唆

1. **SLLMの自己修正能力は十分高い**: Gemma 4 E2B は1回のエラーで自己修正できる。
   連続失敗キャップはより弱いモデルやエッジケースに対する安全装置として機能する。

2. **PEVサイクルの検証フィードバックが効果的**: 検証失敗メッセージをユーザーメッセージとして
   履歴に追加する方式は、SLLMが理解しやすく修正行動に直結する。

3. **Verifier の粒度**: 今回の uppercase_check は単純なルールベース検証だが、
   実際のユースケースでは `go test`, `go vet`, `shellcheck` 等の外部ツール実行が
   Verifier として有効。Phase 9 以降で具体的な Verifier 実装を追加する際の
   インターフェースは十分に柔軟。

4. **エラー回復のオーバーヘッド**: Transient エラーのバックオフ（最大8秒）は
   SLLMのレスポンスタイム（3〜7秒）と同程度。パースエラーが連続する場合の
   合計遅延は許容範囲内（ユニットテストでは3〜9秒）。
