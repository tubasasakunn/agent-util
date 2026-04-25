---
id: "011"
title: PromptBuilder によるセクションベースのプロンプト構成
date: 2026-04-25
status: accepted
---

## コンテキスト
Phase 7（コンテキスト構成）で、手続き的な `strings.Builder` によるプロンプト構築を構造化する必要があった。8Kコンテキストの限られた空間で、静的/動的セクションの分離、ツールスコーピング、MEMORYインデックス、末尾リマインダーなど複数の構成要素を柔軟に管理する基盤が求められた。

## 検討した選択肢
1. **PromptBuilder を `internal/engine` に配置** — Engine の内部状態（registry, delegateEnabled等）に直接アクセス可能
2. **PromptBuilder を `internal/prompt` として独立パッケージ化** — Engine との疎結合だが、Engine内部をpublicにする必要がある
3. **テンプレートベース** — Go の text/template で構築。柔軟だが実行時エラーのリスクとデバッグ困難

## 判断
PromptBuilder を `internal/engine` パッケージ内に配置し、優先度付き Section 型でプロンプトを宣言的に構成するパターンを採用する。

## 理由
- PromptBuilder は Engine の内部状態（registry, delegateEnabled, toolScope 等）に強く依存しており、パッケージ分離するとこれらをpublic APIにする必要がある
- Section.Dynamic は `func() string`（I/Oなし）とし、プロンプト構築を純粋な計算に保つ。I/Oが必要なデータはツール経由で取得する
- ScopeAll / ScopeRouter / ScopeManual の3スコープにより、chat/router/リマインダーの用途を型安全に区別できる
- dirty フラグによるキャッシュで、reservedTokens の不要な再計算を回避する

## 影響
- プロンプト構築が宣言的になり、セクションの追加/削除が容易になった
- WithDynamicSection() でユーザーがカスタムセクションを注入可能になった
- Phase 10（JSON-RPC）でツール数が増加しても、ToolScope でフィルタリング可能
