# リリース手順書

`ai-agent` のリリース作業フロー。タグ push を起点に
[`.github/workflows/release.yml`](../.github/workflows/release.yml)
が動作し、5 プラットフォーム向けバイナリのビルドと GitHub Release の
作成までを自動化する。本書はそこへ至るまでの人間の操作を順序立てて
記述する。

タグ単位のチェックリストは [`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md)、
リリースノートのテンプレートは
[`RELEASE_NOTES_v0.1.0.md`](RELEASE_NOTES_v0.1.0.md) を参照。

---

## 0. 前提

- 対象バージョン例: `0.1.0`（タグ名は `v0.1.0`）
- リポジトリ: <https://github.com/tubasasakunn/ai-agent>
- 真実の源: [`pkg/protocol/version.go`](../pkg/protocol/version.go) の
  `LibraryVersion` 定数
- バージョニング規約: [`docs/VERSIONING.md`](VERSIONING.md)

---

## 1. リリース前チェック (Definition of Ready)

タグを切る前に、以下が満たされていることを確認する。
詳細項目は [`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md) に同期。

- [ ] `CHANGELOG.md` の `[Unreleased]` セクションが空、または
      新しい `[X.Y.Z] - YYYY-MM-DD` セクションへ移動済み
- [ ] バージョン文字列が以下 6 箇所で一致している
  - [ ] `pkg/protocol/version.go` (`LibraryVersion`)
  - [ ] `sdk/python/pyproject.toml` (`version`)
  - [ ] `sdk/js/package.json` (`version`)
  - [ ] `README.md` の Latest バッジ
  - [ ] `CLAUDE.md` の `## バージョン` セクション
  - [ ] `CHANGELOG.md` 見出し
- [ ] `scripts/check_all.sh` が PASS する
  ```bash
  bash scripts/check_all.sh
  ```
- [ ] `git status` がクリーンである
  ```bash
  git status --short        # 何も出ないこと
  ```
- [ ] `main` ブランチに居る
  ```bash
  git rev-parse --abbrev-ref HEAD   # → main
  ```
- [ ] 直前のコミットが CI で PASS している
  （Actions タブの `CI` ワークフローが緑）

---

## 2. リリースタグ作成

annotated tag をローカルに作成する。

```bash
VERSION=0.1.0
git tag -a "v${VERSION}" -m "Release ${VERSION}"
```

GPG 署名する場合 (`-s` を使う):

```bash
git tag -s "v${VERSION}" -m "Release ${VERSION}"
```

タグは [Semantic Versioning](https://semver.org/lang/ja/) に従い
`vMAJOR.MINOR.PATCH` 形式。プレリリースは `v0.2.0-rc.1` のように
ハイフン以降を付与すると workflow が `prerelease=true` で公開する
（[release.yml の `prerelease`](../.github/workflows/release.yml) を参照）。

---

## 3. タグ push

タグを GitHub にプッシュすると Release ワークフローが起動する。

```bash
git push origin "v${VERSION}"
```

`refs/tags/v*.*.*` にマッチする push が
[`release.yml`](../.github/workflows/release.yml) の `on.push.tags` を
発火させる。本流のブランチ push は不要（コードは既に main に居る前提）。

> **注意**: `git push --tags` は他のローカルタグも一括送信されるため、
> 単一タグだけを送る `git push origin v${VERSION}` を推奨する。

---

## 4. CI での自動処理

タグ push をトリガに、Actions の `Release` ワークフローが以下を実行する。

1. **クロスコンパイル** (`build` ジョブ、5 並列マトリクス)
   - `linux/amd64`
   - `linux/arm64`
   - `darwin/amd64`
   - `darwin/arm64`
   - `windows/amd64` (`.exe`)
   - すべて `CGO_ENABLED=0`、`-trimpath -ldflags "-s -w"` で静的バイナリ
   - 各成果物に `LICENSE` / `README.md` / `CHANGELOG.md` を同梱
   - Linux / macOS は `tar.gz`、Windows は `zip`
2. **GitHub Release 公開** (`release` ジョブ)
   - 全プラットフォームのアーティファクトを集約
   - `CHANGELOG.md` から該当 `## [X.Y.Z]` セクションを `awk` で抽出し
     リリースノート本文として登録
   - タグ名にハイフン (`-`) を含む場合は `prerelease: true`

ワークフローの状態は Actions タブで `Release / Publish GitHub Release`
を確認する。失敗した場合はジョブログを確認し、必要に応じて
`workflow_dispatch` で再実行（`inputs.tag` に `v0.1.0` を指定）。

---

## 5. リリース後検証

ワークフロー成功後、以下を確認する。

1. **Releases ページ**
   <https://github.com/tubasasakunn/ai-agent/releases/tag/v0.1.0>
   - 5 プラットフォーム分の `.tar.gz` / `.zip` がアップロードされている
   - リリース本文が `CHANGELOG.md` の該当セクションと一致する
2. **バイナリの動作確認**（最低 1 プラットフォーム、推奨は手元の OS）
   ```bash
   # macOS arm64 の例
   curl -L -o agent.tar.gz \
     https://github.com/tubasasakunn/ai-agent/releases/download/v0.1.0/agent_v0.1.0_darwin_arm64.tar.gz
   tar -xzf agent.tar.gz
   ./agent_darwin_arm64/agent --version 2>/dev/null || echo '{"jsonrpc":"2.0","id":1,"method":"agent.run","params":{"prompt":"hi"}}' | ./agent_darwin_arm64/agent
   ```
3. **SDK の動作確認**
   - Python: `pip install -e ./sdk/python` 後、
     [`examples/python/01_minimal_chat`](../examples/) を実行
   - TypeScript: `cd sdk/js && npm install && npm run build` 後、
     [`examples/typescript/01_minimal_chat`](../examples/) を実行
   - **PyPI / npm への publish は未対応**。`0.x` の間は git からの
     install (editable / `npm pack`) を案内する

---

## 6. アナウンス（任意）

- README の `Latest` バッジは [shields.io](https://img.shields.io) の
  カスタムバッジで、`README.md` 上の文字列を毎リリース手で更新する。
  リリース後に GitHub 上で表示が古いままなら、ブラウザのキャッシュ・
  shields.io 側のキャッシュを疑う（force refresh で解決することが多い）
- 必要なら以下のチャネルで告知
  - [GitHub Releases ページ](https://github.com/tubasasakunn/ai-agent/releases) の URL
  - 関連 Issue / Discussion のクローズ
  - SNS / 社内チャンネル等

---

## トラブルシュート

| 症状                                            | 対処                                                                 |
| ----------------------------------------------- | -------------------------------------------------------------------- |
| `release.yml` が起動しない                      | タグ名が `v*.*.*` 形式か確認。`git push origin <tag>` でないと不発     |
| ビルドが特定 OS だけ失敗する                    | `fail-fast: false` のため他の OS は完走する。失敗ジョブのログを確認   |
| リリース本文が `Release vX.Y.Z.` だけになる     | `CHANGELOG.md` のセクション抽出に失敗。`## [X.Y.Z]` 形式を確認        |
| 誤ったタグを push してしまった                  | リモートタグ削除 (`git push --delete origin vX.Y.Z`) → ローカルタグ削除 (`git tag -d vX.Y.Z`) → Release を Draft 化または削除 |
| アーティファクト名が衝突した                    | 既存 Release を削除してから再 push、または別バージョンで切り直す       |

---

## 参考

- [`docs/VERSIONING.md`](VERSIONING.md) — バージョニング方針
- [`docs/RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md) — タグ直前のチェックリスト
- [`docs/RELEASE_NOTES_v0.1.0.md`](RELEASE_NOTES_v0.1.0.md) — v0.1.0 のリリースノート下書き
- [`docs/CONTRIBUTING.md`](CONTRIBUTING.md) — 貢献ガイド
- [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/)
- [Semantic Versioning 2.0.0](https://semver.org/lang/ja/)
