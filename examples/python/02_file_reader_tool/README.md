# 02 · File reader tool (Python)

Demonstrates the `@tool` decorator. We expose a single `read_file(path)`
tool, allow it via `permission.allow`, and ask the model to summarise
`README.md`. The router (ADR-002) picks the tool, the SDK dispatches to
the Python function, the result is fed back into the loop.

## Run

```bash
pip install -e ../../../sdk/python
python main.py
```

## Expected output

```
response:
ai-agent は SLLM 用の Go 製エージェントハーネスです。
JSON-RPC over stdio でホストプロセスと会話します。
Phase 1 から段階的に機能を増やしながら開発しています。
---
reason: completed | turns: 2
```

`response` shape depends on the SLLM; what matters is that `reason` is
`completed` (not `max_turns`) and `turns` reflects ≥ 1 tool use round.

## Notes

- `read_only=True` lets the core auto-approve the call when the
  permission system is otherwise restrictive.
- The path traversal check inside `read_file` is the kind of tiny
  guardrail you should put on every filesystem-touching tool — guards
  (example 03) are the second line of defence.

## See also

- [`../../../sdk/python/README.md`](../../../sdk/python/README.md)
- ADR-002 (router + JSON mode), ADR-012 (permissions)
