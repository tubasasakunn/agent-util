---
id: "006"
title: サブエージェント統合にEngine内バーチャルツールパターンを採用
date: 2026-04-19
status: accepted
---

## コンテキスト
Phase 6でサブエージェント（delegate_task）の統合方式を決定する必要がある。SLLMのルーターがツール選択する既存フロー（ADR-002）に、タスク委譲機能を自然に組み込みたい。

## 検討した選択肢

### A: delegate_task を pkg/tool.Tool として実装
- メリット: Tool interface に準拠、テストしやすい
- デメリット: ツール内で Engine を生成する必要があり、pkg/tool → internal/engine の循環依存が発生。EngineRunner コールバック注入で技術的には回避可能だが、ツール側にエージェントライフサイクル管理（context伝播、goroutine管理）の責務が漏れ、Tool interface の「最小インターフェース」原則に反する

### B: Engine 内バーチャルツールとして処理
- メリット: 循環依存なし、ルーターパターン(ADR-002)との自然な統合、サブエージェントのライフサイクルをEngineが一元管理
- デメリット: Tool interface に準拠しない（Registry外管理）

### C: 外部オーケストレーター（cmd/ レベル）
- メリット: Engine はシンプルなまま
- デメリット: ループ内での動的分岐ができない。ルーターの判断と統合しにくい

## 判断
選択肢Bを採用。delegate_task はRegistryに登録せず、routerSystemPromptに定義を埋め込み、toolStep()内で rr.Tool == "delegate_task" を検出して delegateStep() に分岐する。

## 理由
- pkg/tool → internal/engine の循環依存を完全に回避
- ルーターが既にJSON modeでツール選択する構造（ADR-002）に自然に統合。ルーターにとってdelegate_taskは他のツールと同じ選択肢
- context.Context によるキャンセル伝播をEngine内で直接管理でき、サブエージェントのライフサイクルが明確
- Fork()で子Engineを生成する際にdelegateEnabledをfalseにすることで、ネスト再帰を型安全に防止

## 影響
- toolStep() に if 文1つの分岐が追加される
- delegate_task は Registry 外で管理される特殊なツール（バーチャルツール）
- 将来の coordinate_tasks も同じパターンで追加可能
