# Deep Research (Node.js + LM Studio)

`@ai-agent/browser` を Node.js から直接呼び、ローカル LM Studio（または任意の
OpenAI 互換 HTTP エンドポイント）で **複数ソース集約 + 構造化レポート** を生成する例。

ブラウザ版 SDK は WebGPU 内蔵 LLM (WebLLM) で動くが、`Completer` インターフェース
を満たす HTTP クライアントを差し込むだけで、LM Studio や llama-server、OpenAI API、
任意のプロキシなどに切り替えられる。本例は HttpCompleter を 1 ファイル（dr.mjs）に
書き下ろし、Gemma 4 E2B を呼ぶ。

## 必要なもの

- Node.js 22+
- LM Studio または同等の OpenAI 互換 HTTP サーバが起動していること
  - 例: LM Studio で Gemma 4 E2B Q4_K_M を読み込み、`localhost:8080` で公開
- `Ctrl+C` で停止可能

## 使い方

```bash
cd examples/node/deep-research
npm install
PROMPT="What is the Linux kernel? Also check Hacker News for trending topics. Write a structured report." \
  npm start
```

環境変数:
- `SLLM_ENDPOINT` — デフォルト `http://localhost:8080/v1/chat/completions`
- `SLLM_API_KEY` — デフォルト `sk-gemma4`
- `SLLM_MODEL`   — デフォルト `gemma-4-E2B-it-Q4_K_M`
- `PROMPT`       — リサーチ題材

## 出力例

```
## Summary
The Linux kernel is a free and open-source Unix-like kernel created by Linus Torvalds in 1991, which is widely used in various computer systems and operating systems like Android. The investigation also fetched the current top stories from Hacker News.

## Key facts
- The Linux kernel is a free and open-source Unix-like kernel created by Linus Torvalds in 1991 (source: search_wikipedia).
- It has been adopted by many operating system distributions, including Android (source: search_wikipedia).
- The current top Hacker News stories include topics such as progress reports, statecharts, and amateur problem-solving with AI (source: hn_top_stories).

## Answer
The Linux kernel is a free and open-source Unix-like kernel that was created by Linus Torvalds in 1991 ...

Turns: 8, reason: completed, elapsed: 52.3s
Tools used (2): search_wikipedia, hn_top_stories
```

## 仕組み

| 部品 | 役割 |
|---|---|
| `HttpCompleter` (dr.mjs) | OpenAI 互換 HTTP API を呼ぶ `Completer` 実装。LM Studio は OpenAI の `json_schema` 型を完全サポートしないので `type: "json_object"` のみで送信 |
| `agent.configure({ min_tool_kinds: 2 })` | router が "none" を選んでも minimum N 種類のツール呼ぶまで再ルーティング |
| 内部 backstop (SDK 側) | router が既使用ツールを再選択 → reject + reminder。同じツール 3 連続 → chat step 強制 |
| ツール群 | URL 不要なものに限定（小型モデルが URL を幻覚するのを防ぐ）: `search_wikipedia`, `hn_top_stories`, `get_current_time`, `calculator` |

## 実機検証ログ（Gemma 4 E2B Q4_K_M）

| シナリオ | turns | 時間 | ツール |
|---|---|---|---|
| Rust programming language | 5 | 35.7s | search_wikipedia + hn_top_stories |
| Linux kernel | 8 | 52.3s | search_wikipedia + hn_top_stories |
| WebAssembly | 7 | 80.7s | search_wikipedia + hn_top_stories |
