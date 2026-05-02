# 013: Agent Skills E2E検証

## 目的

Agent Skills（agentskills.io仕様）のエンドツーエンド動作確認。
Progressive Disclosureの3段階（Discovery → Activation → Execution）が実機SLLMで正常動作するか検証する。

### 検証項目

| # | テスト | 確認すること |
|---|---|---|
| 1 | 明示的スキル起動 | ユーザーがスキル名を直接指定した場合に`activate_skill`が呼ばれるか |
| 2 | 暗黙的スキルマッチ | タスク内容からdescriptionマッチでスキルが選択されるか |
| 3 | スキル不要リクエスト | スキルと無関係な質問でactivateが呼ばれないか |
| 4 | ループ防止 | スキル内容取得後に`none`へ遷移するか（無限ループしないか） |

## 手段

- 対象: Gemma-4-E2B-it-Q4_K_M (localhost:8080)
- スキル: `.agents/skills/greet-japanese/` (SKILL.md ファイルベース)
- 方法: `go run ./cmd/agent/` ワンショットモード

## 結果

### 総合評価

| # | テスト | 結果 | 判定 |
|---|---|---|---|
| 1 | 明示的スキル起動 | `activate_skill(name=greet-japanese)`が正しく呼ばれ、スキル指示通りの応答 | PASS |
| 2 | 暗黙的スキルマッチ | "Hello! Say hello to me" → greet-japaneseが選択され正常応答 | PASS (修正後) |
| 3 | スキル不要リクエスト | "1+1は何ですか" → ツール呼び出しなし、直接回答 | PASS |
| 4 | ループ防止 | activate_skill結果に`Select tool="none" next`を追加して解消 | PASS (修正要) |

### 詳細

#### シナリオ1: 明示的スキル起動（PASS）

```
プロンプト: "Please activate the greet-japanese skill and greet me"

[router] activate_skill を選択 | 引数: {"name": "greet-japanese"}
[router] 理由: The user explicitly requested to activate the 'greet-japanese' skill
[chat] こんにちは！今日は何かお手伝いできますか？一緒に頑張りましょう！
[done] 2 turns, 3407 tokens
```

#### シナリオ2: 暗黙的スキルマッチ（修正前FAIL → 修正後PASS）

**初回（修正前）**:
スキル読み込み後も`activate_skill`を呼び続け、max_turns(10)で終了。
カタログに`greet-japanese`の名前が見えているため、ルーターが「まだ使っていない」と判断し続ける。

**修正内容**: `activate_skill`の返却テキストに以下を追加:
```
Instructions loaded. Now respond to the user following the above instructions. Select tool="none" next.
```

**修正後**:
```
[router] activate_skill → [router] ツール不要（The skill result provides the exact response to use）
[chat] こんにちは！今日は何かお手伝いできますか？一緒に頑張りましょう！
[done] 2 turns, 3441 tokens
```

#### シナリオ3: スキル不要リクエスト（PASS）

```
プロンプト: "1 + 1 は何ですか"

[router] ツール不要 → 直接応答（simple arithmetic question）
[chat] 1 + 1 は **2** です。
[done] 1 turns, 1781 tokens
```

### シナリオ4: Skill-as-Tool統合後のE2E（PASS）

```
設計変更: activate_skill メタツール廃止 → 各スキルを直接ツール登録

プロンプト: "こんにちは！挨拶してください"

[skills] loaded 6 skill(s)
[router] greet-japanese を選択 | 引数: {}
[router] 理由: The user said 'こんにちは！挨拶してください', which directly calls for the greet-japanese tool.
[tool] greet-japanese 完了 (511 bytes)
[router] ツール不要 → 直接応答
[chat] こんにちは！今日は何かお手伝いできますか？一緒に頑張りましょう！
[done] 2 turns, 2865 tokens  ← activate_skill版より634トークン削減
```

`activate_skill(name="greet-japanese")` ではなく `greet-japanese` が直接呼ばれるように変化。

### 設計への示唆

1. **ループ防止はツール結果レベルで行うのが実用的**  
   エンジン側でdeduplicationを実装する方法もあるが、SLLMの判断力への明示的ヒントの方が汎用性が高い。
   スキルに限らず「このツールを再度呼ぶ必要はない」という結果返却のパターンは他ツールにも適用を検討。

2. **ファイルベース以外のSkillが容易に追加できる抽象設計が有効**  
   `Activate func() (string, error)` による抽象化により、インライン定義・プログラム生成・リモート取得が
   同一インターフェースで扱える。テスト時のモックも簡単。

3. **スキル発見の精度はdescriptionの質に依存**  
   "Say hello to me" → `greet-japanese` のマッチは description に "Use when the user says hello" が
   あったため成功。description のキーワード選定がSLLMの判断精度を左右する。

4. **ScopeAll でのカタログ注入は有効**  
   ルーター・チャット両方にカタログが見えることで、ルーターは適切なスキルを選択でき、
   チャットはスキルの文脈を把握して応答できる。
