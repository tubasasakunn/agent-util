# 04 · Streaming (TypeScript)

Uses `agent.runStream()`, which returns an `AsyncIterable<StreamEvent>`.
We branch on `ev.kind` (`delta` / `status` / `end`) to render tokens to
stdout, context-pressure stats to stderr, and the final summary at the
end.

`streaming.enabled` must be set in `configure({ streaming: ... })` for
deltas to be emitted.

## Run

```bash
( cd ../../../sdk/js && npm install && npm run build )
npm install
npx tsx main.ts
```

## Expected output

A live token stream like:

```
こんにちは。私は SLLM ベースのエージェントで...
[ctx 412/8192 = 5%]
---
final reason: completed | turns: 1
```

## See also

- [`../../../sdk/js/README.md`](../../../sdk/js/README.md)
- `pkg/protocol/methods.go` — `stream.delta` / `context.status` notification shape.
