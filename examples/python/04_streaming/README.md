# 04 · Streaming (Python)

Enables `streaming.enabled` and `context_status` in `AgentConfig`, then
prints `stream.delta` chunks to stdout as they arrive. The
`context.status` callback prints token-budget pressure to stderr so the
two streams don't interleave.

## Run

```bash
pip install -e ../../../sdk/python
python main.py
```

## Expected output

A live, character-by-character stream like:

```
こんにちは。私は SLLM ベースのエージェントで、Go で書かれた...
[ctx 412/8192 = 5%]
---
final reason: completed | turns: 1
```

Without `streaming.enabled=True` the SDK still works but no deltas are
delivered (the callback is never invoked).

## See also

- [`../../../sdk/python/README.md`](../../../sdk/python/README.md)
- `pkg/protocol/methods.go` — `stream.delta` / `context.status` notification shape.
