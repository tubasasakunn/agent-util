# バージョニング方針

`ai-agent` は [Semantic Versioning 2.0.0](https://semver.org/lang/ja/) を採用する。
バージョン文字列は `MAJOR.MINOR.PATCH` 形式で、コア（Go バイナリ / `pkg/protocol`）
と各 SDK（Python / TypeScript）で同一の値を維持する。

## バージョンインクリメントの基準

### MAJOR — 破壊的変更

以下の変更は MAJOR を上げる:

- JSON-RPC メソッドの削除
- 既存メソッドの必須パラメータ追加
- 既存パラメータ/結果フィールドの型変更（例: `string` → `int`）
- 既存パラメータ/結果フィールドの削除またはリネーム
- エラーコードの意味変更
- 既存通知 (`stream.delta` 等) の削除またはペイロード型変更

#### メソッドバージョニング併用

破壊的な変更が必要なメソッドは、可能な限り**メソッド名にバージョンを付けて
旧版と並走させる**運用を推奨する:

```
agent.run        — v1（既存）
agent.run.v2     — v2（新形式）
```

旧メソッドは 1 メジャーバージョン分は deprecated として残し、削除は次の
MAJOR で行う。これにより、ラッパー側の段階的移行が可能になる。

### MINOR — 後方互換な機能追加

以下の変更は MINOR を上げる:

- 新メソッドの追加 (`xxx.yyy`)
- 既存パラメータ/結果への `omitempty` フィールド追加
- 新しい通知の追加
- 新しいビルトイン Guard / Verifier の追加
- 新しいエラーコードの追加（既存コードの意味は変えない）
- SDK への新ヘルパー / デコレータ追加

### PATCH — バグ修正・内部改善

以下の変更は PATCH を上げる:

- バグ修正（API は変えない）
- ドキュメント修正
- 内部リファクタリング
- パフォーマンス改善
- 依存ライブラリの更新（API に影響しない範囲）

## 0.x の特例

semver の慣例に従い、`0.x.y` の間は **MINOR バンプで破壊的変更を許容**する。
これは API がまだ安定していないことを示すシグナルでもある。

- `0.1.0` → `0.2.0`: 破壊的変更を含みうる
- `0.1.0` → `0.1.1`: 破壊的変更を含まない（後方互換）

`1.0.0` のリリース以降、JSON-RPC API は本ドキュメントの規則に従って
安定化を約束する。

## バージョン同期

バージョン文字列は以下の場所で同一に保つ:

| 対象              | ファイル                          |
| ----------------- | --------------------------------- |
| Go (真実の源)     | `pkg/protocol/version.go` (`LibraryVersion`) |
| Python SDK        | `sdk/python/pyproject.toml`       |
| TypeScript SDK    | `sdk/js/package.json`             |
| CHANGELOG         | `CHANGELOG.md` 見出し             |
| README            | `README.md` バッジ                |
| CLAUDE.md         | `## バージョン` セクション        |

新しいリリースを切るときは、上記の全てを同じ値で更新する。

## リリースプロセス

1. **CHANGELOG 更新** — `[Unreleased]` セクションの内容を新しい
   `[X.Y.Z] - YYYY-MM-DD` セクションへ移動。新しい `[Unreleased]` を空で残す
2. **バージョン bump** — 以下を同じ値に更新:
   - `pkg/protocol/version.go` の `LibraryVersion` 定数（`Version` は JSON-RPC 仕様の "2.0" を指すので別物。混同しない）
   - `sdk/python/pyproject.toml` の `version`
   - `sdk/js/package.json` の `version`
   - `README.md` の Latest バッジ
   - `CLAUDE.md` の `## バージョン`
3. **検証** — `go build ./...`、`go test ./...`、Python と JS の test を全てグリーンに
4. **コミット** — `release: vX.Y.Z` 形式
5. **タグ** — `git tag -a vX.Y.Z -m "Release vX.Y.Z"`
6. **push** — `git push && git push --tags`
7. **必要なら** SDK の publish (`pip publish` / `npm publish`)。
   `0.x` の間は publish しなくてよい

## 後方互換性ガイドライン

`pkg/protocol/` の型を変更する際は、以下を守る:

- 新フィールドは必ず `omitempty` を付ける
- ポインタ型でなく値型を使う場合、ゼロ値が「未指定」と区別されない点に注意
- `json.RawMessage` を使う柔軟なフィールドは、新しいバリエーションを
  追加する際に既存ラッパーが破綻しないことを確認する
- メソッド命名は `namespace.action` 形式 (`agent.run`, `tool.execute` など)

詳細は `.claude/rules/protocol.md` を参照。
