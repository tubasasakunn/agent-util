---
id: "008"
title: Worktree実行モデルでworkDirをcontext.Context経由で伝達
date: 2026-04-19
status: accepted
---

## コンテキスト
サブエージェント（delegate_task）でファイル変更を行う場合、親と子が同じファイルシステムを共有するとファイル競合が発生する。git worktreeによる物理分離が必要だが、ツールインスタンスは親子Engine間で共有されるため、ツールごとに異なるワーキングディレクトリを指定する仕組みが必要。

## 検討した選択肢

### A: context.Context経由でworkDirを伝達
- メリット: Toolインターフェース変更不要。Execute(ctx)のctxに自然に載る。ツールはopt-inで対応
- デメリット: ツール側がWorkDirFromContextを呼ぶ必要がある（暗黙的な契約）

### B: Toolインターフェースにワーキングディレクトリメソッドを追加
- メリット: 明示的な契約
- デメリット: 全ツール実装・モック・テストの変更が必要。破壊的変更

### C: ツールをラップして引数のパスを書き換える
- メリット: ツール側の変更不要
- デメリット: ツール固有のJSONスキーマを知る必要があり汎用化が困難

## 判断
選択肢Aを採用。workDirキーをpkg/tool/に定義し、import cycleを回避する。

## 理由
- Execute()の第一引数ctx context.Contextは、リクエストスコープのデータ伝達というGoの慣例に沿った使い方
- pkg/tool/にキーを置くことで、internal/engine/とinternal/tools/の両方からアクセス可能（循環依存なし）
- 既存ツールは影響を受けない（未対応ツールはworkDirを単に無視する）
- delegate_taskのmode="worktree"が失敗した場合はmode="fork"にフォールバックし、安全に劣化する

## 影響
- pkg/tool/tool.goにContextWithWorkDir/WorkDirFromContextを追加
- Engine.toolStep()でtool実行前にctxにworkDirを注入
- readfileツールがworkDir対応（相対パスをworkDir基準で解決）
- 新ツール実装者はWorkDirFromContext(ctx)でopt-in対応可能
