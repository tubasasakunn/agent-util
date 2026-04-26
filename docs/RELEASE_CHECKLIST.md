# Release Checklist

タグを切る直前にチェックする最小リスト。詳細手順は
[`RELEASE.md`](RELEASE.md)、バージョニング規約は
[`VERSIONING.md`](VERSIONING.md) を参照。

`$VERSION` は `0.1.0` のような MAJOR.MINOR.PATCH 形式（先頭 `v` なし）。

## Pre-tag

- [ ] `CHANGELOG.md` の `[Unreleased]` セクションが空、または
      `[X.Y.Z] - YYYY-MM-DD` セクションへ移動済み
- [ ] バージョン文字列が以下 6 箇所で `$VERSION` に揃っている
  - [ ] `pkg/protocol/version.go` (`LibraryVersion`)
  - [ ] `sdk/python/pyproject.toml` (`version`)
  - [ ] `sdk/js/package.json` (`version`)
  - [ ] `README.md` の Latest バッジ
  - [ ] `CLAUDE.md` の `## バージョン` セクション
  - [ ] `CHANGELOG.md` の `## [X.Y.Z]` 見出し
- [ ] `bash scripts/check_all.sh` が PASS
- [ ] `git status --short` がクリーン（出力なし）
- [ ] 現在 `main` ブランチに居る (`git rev-parse --abbrev-ref HEAD`)
- [ ] 直前の main コミットが CI 上で緑になっている
- [ ] `docs/RELEASE_NOTES_v$VERSION.md` を作成し、CHANGELOG と整合済み

## Tag

```bash
VERSION=0.1.0
git tag -a "v${VERSION}" -m "Release ${VERSION}"
git push origin "v${VERSION}"
```

- [ ] annotated tag を作成（`-a` または `-s`）
- [ ] タグを `origin` に push（`release.yml` が発火）

## Post-tag

- [ ] Actions の `Release` ワークフローが成功
- [ ] 5 プラットフォーム分のアーティファクトが Releases ページに揃っている
  - `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`
- [ ] Release ノートが `docs/RELEASE_NOTES_v$VERSION.md` と整合
- [ ] 少なくとも 1 プラットフォームでバイナリの DL → 起動確認
- [ ] Python / TypeScript SDK のサンプル (`examples/`) が動く
- [ ] README の Latest バッジ表示が `v$VERSION` を反映している
- [ ] 必要に応じて Issue / Discussion を更新・クローズ
