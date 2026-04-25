---
id: "012"
title: 権限とガードレールにPermissionChecker+GuardRegistryの2層分離パターンを採用
date: 2026-04-25
status: accepted
---

## コンテキスト
Phase 9でエージェントの行動を安全に制約する多層防御が必要になった。ツール実行前の権限チェック、入力/出力の安全性検証、危険検知時の即時停止を実装する必要がある。

## 検討した選択肢
1. **単一GuardRailインターフェース** — 入力・ツール・出力を1つのインターフェースで処理
2. **PermissionChecker+GuardRegistryの2層分離** — 「誰がツールを使えるか」と「内容が安全か」を分離
3. **Verifierの拡張** — 既存のVerifierインターフェースを事前チェック用にも拡張

## 判断
PermissionChecker（ポリシーベースの静的権限判定）とGuardRegistry（3層ガードレール + トリップワイヤ）の2層分離パターンを採用する。

## 理由
- PermissionCheckerは「このツールを使っていいか」、GuardRegistryは「この呼び出し/入力/出力の内容が安全か」という異なる関心事を持つ
- PermissionCheckerはdeny→allow→readOnly→ask→fail-closedのパイプラインで静的判定、GuardRegistryは動的コンテンツ検証と役割が明確に分かれる
- Verifierは「ツール実行後の結果検証」であり、事前チェックとは時点が異なるため拡張は不適切
- 3つの独立したガードインターフェース（InputGuard/ToolCallGuard/OutputGuard）はGoの小さなインターフェース原則に適合
- 両方nilの場合は完全後方互換を保証

## 影響
- toolStep()の処理順序が固定: ルーター→パーミッション→ガードレール→実行→検証
- 子EngineはPermissionPolicyを継承するがUserApproverはnil（fail-closed）
- トリップワイヤはTripwireError（ErrClassFatal）としてRun()からerrorを返す
- GuardとPermissionは独立して設定可能（片方だけでも動作）
