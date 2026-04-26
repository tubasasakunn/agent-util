# 貢献ガイド

`ai-agent` への貢献を歓迎します。本書は開発環境セットアップ、
コーディング規約、ADR / investigation の書き方、PR の進め方をまとめます。

---

## 開発環境セットアップ

### 必要なツール

| ツール   | バージョン | 用途                                  |
| -------- | ---------- | ------------------------------------- |
| Go       | `go.mod` の `go` 行と一致 (現在 1.25.x) | コアバイナリ / プロトコル / テスト |
| Python   | 3.10 以上                              | Python SDK / OpenRPC 検証 / ツール |
| Node.js  | 20 以上                                | TypeScript SDK / examples          |
| Git      | 2.30 以上                              | 標準                              |

### クローンとビルド

```bash
git clone https://github.com/tubasasakunn/ai-agent.git
cd ai-agent

# Go コア
go build -o agent ./cmd/agent/

# Python SDK（editable install）
pip install -e "./sdk/python[test]"

# TypeScript SDK
cd sdk/js && npm install && npm run build && cd -
```

### ローカルでの全体検証

CI と等価のチェックを 1 コマンドで:

```bash
bash scripts/check_all.sh
```

セクション単位でスキップ可:

```bash
SKIP_JS=1 bash scripts/check_all.sh        # Node 環境がない場合
SKIP_PYTHON=1 bash scripts/check_all.sh    # Python 環境がない場合
```

各セクションの詳細は [`scripts/check_all.sh`](../scripts/check_all.sh)
冒頭のコメントを参照。

---

## コーディング規約

詳細は [`.claude/rules/`](../.claude/rules/) 配下のドキュメントに集約
されています。新規コードを書く際はまず該当ファイルを参照してください。

| ルール             | ファイル                                                |
| ------------------ | ------------------------------------------------------- |
| Go 全体            | [`.claude/rules/go-general.md`](../.claude/rules/go-general.md) |
| `cmd/`             | [`.claude/rules/cmd.md`](../.claude/rules/cmd.md)       |
| `internal/engine/` | [`.claude/rules/engine.md`](../.claude/rules/engine.md) |
| `internal/llm/`    | [`.claude/rules/llm.md`](../.claude/rules/llm.md)       |
| `internal/context/`| [`.claude/rules/context-mgmt.md`](../.claude/rules/context-mgmt.md) |
| `internal/rpc/`    | [`.claude/rules/rpc.md`](../.claude/rules/rpc.md)       |
| `pkg/protocol/`    | [`.claude/rules/protocol.md`](../.claude/rules/protocol.md) |
| `pkg/tool/`        | [`.claude/rules/tool.md`](../.claude/rules/tool.md)     |

抜粋:

- エラーは必ず `fmt.Errorf("操作名: %w", err)` でラップ
- パッケージ名は短く単数形（`engine`, `llm`, `rpc`、`utils`/`common` 禁止）
- 公開 API のコンストラクタは Functional Options パターン
- インターフェースは利用側ではなく **提供側** に置く
- goroutine の起動には必ず `context.Context` を渡す
- `pkg/protocol/` の型変更は `omitempty` 追加で後方互換を保つ

`gofmt -l .` の差分は CI で fail します。コミット前に `gofmt -w .` を
推奨します。

---

## ADR の書き方

技術判断・アーキテクチャ選択を記録します。
`.claude/skills/decisions/` 配下に番号付きで配置:

```
.claude/skills/decisions/
├── SKILL.md                                 — テンプレートと運用ルール
├── 001-json-rpc-over-stdio.md
├── 002-router-single-tool-pattern.md
└── ...
```

新規 ADR は次の連番（既存最大 + 1）で `XXX-kebab-case-title.md` を作成。
テンプレートは [`.claude/skills/decisions/SKILL.md`](../.claude/skills/decisions/SKILL.md)
を参照。最低限以下を含めてください:

- 状況 (Context) — なぜこの判断が必要になったか
- 選択肢 (Options) — 検討した代替案
- 決定 (Decision) — 採用した案
- 理由 (Rationale) — トレードオフと選定根拠
- 影響 (Consequences) — 良い影響、悪い影響、将来の制約

ADR 追加時は `CLAUDE.md` の「主要な設計判断」リストにも 1 行追記。

---

## investigation の書き方

実験・検証・PoC・ベンチマークを記録します。
`.claude/skills/investigation/` 配下に番号付きで配置:

```
.claude/skills/investigation/
├── SKILL.md
├── 001_sllm_function_calling_test/
├── 002_phase3_tool_execution_flow/
└── ...
```

各 Phase の実装完了後は **investigation でのシナリオベース統合検証が必須**
です（[`CLAUDE.md`](../CLAUDE.md) の開発ルール）。

検証は実機 SLLM で手動実行し結果を記録します（mock のみは不可）。
詳細は [`.claude/skills/investigation/SKILL.md`](../.claude/skills/investigation/SKILL.md)
を参照。

---

## PR の手順

1. **issue を立てる**（任意だが推奨）
   - 大きな変更や設計判断を含む場合は事前合意のために issue で議論
2. **ブランチを作成**
   ```bash
   git checkout -b feature/short-description
   ```
3. **実装 + テスト + ドキュメント**
   - コード変更には対応するテストを追加
   - 公開 API を変更したなら `docs/openrpc.json` と `pkg/protocol/spec_test.go`
     を更新
   - 設計判断を伴う変更は ADR を追加
4. **ローカル検証**
   ```bash
   bash scripts/check_all.sh
   ```
5. **コミット**（[コミットメッセージ規約](#コミットメッセージ規約)参照）
6. **PR を開く**
   - PR テンプレート (`.github/pull_request_template.md`) があれば
     自動で適用される
   - なければ以下のセクションを含める:

```markdown
## Summary
- 変更概要を 1〜3 行

## Why
- 動機・背景

## Changes
- ファイル/モジュール単位の主な変更点

## Test plan
- [ ] 追加したテスト
- [ ] 手動確認した内容
- [ ] `bash scripts/check_all.sh` PASS

## Related
- Closes #<issue>
- Refs ADR-XXX / investigation-XXX
```

7. **CI が緑になるまで対応**
8. **レビューを受けて merge**
   - merge 戦略は **squash** を基本（履歴を線形に保つ）

### 破壊的変更を含む PR

- `pkg/protocol/` の型変更、JSON-RPC メソッドのシグネチャ変更などは
  破壊的になりがち。[`docs/VERSIONING.md`](VERSIONING.md) を参照
- 必要に応じてメソッドをバージョン併走させる（`agent.run` と
  `agent.run.v2` の併存等）
- 次のリリースが MAJOR バンプ (`1.0.0` 以降) または MINOR バンプ (`0.x` 中)
  になることを PR 説明で明記

---

## コミットメッセージ規約

短く、現在形・命令形で書きます。1 行目は 72 文字以内。

```
<scope>: <概要>

<必要なら詳細説明>

Refs: ADR-013, investigation-009

Co-Authored-By: <name> <email>
```

例:

```
engine: PEV サイクルでの format エラー判定を厳格化

Verifier の戻り値 nil と {"ok": false} を取り違えるバグを修正。
RetryDecisionTable も nil の場合 transient ではなく format に
分類されるよう更新。

Refs: investigation-007

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

- `<scope>` は `engine`, `rpc`, `protocol`, `sdk-py`, `sdk-js`, `docs`,
  `ci` などの 1 ワード
- AI ツールを併用した場合は `Co-Authored-By` を付与（推奨）
- 機密情報（鍵、社内 URL、個人名等）は含めない

---

## リリース

リリース作業は [`docs/RELEASE.md`](RELEASE.md) と
[`docs/RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md) を参照。
通常コントリビュータがタグを切る必要はありませんが、リリース時の
バージョン同期は本書の 6 箇所をすべて更新する必要がある点に
注意してください。

---

## 行動規範

すべてのコントリビュータは敬意ある言葉遣いを保ち、技術的な議論に
集中してください。レビューは「コードに対するもの」であり、
「人に対するもの」ではありません。

---

## ライセンス

本リポジトリへの貢献は [MIT License](../LICENSE) の下で公開されることに
同意したものとみなされます。
