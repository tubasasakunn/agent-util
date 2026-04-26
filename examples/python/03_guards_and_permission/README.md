# 03 · Guards and permission (Python)

Three short scenarios on a single agent instance:

1. A normal prompt is allowed by every layer.
2. A classic prompt-injection input is blocked by the built-in
   `prompt_injection` input guard — the SDK raises `GuardDenied`.
3. A request to call `dangerous_tool` is blocked by `permission.deny`
   before the tool is invoked. The agent observes the rejection and
   replies in natural language.

## Run

```bash
pip install -e ../../../sdk/python
python main.py
```

## Expected output

```
[1] normal: 7 | reason: completed
[2] injection blocked: input guard 'prompt_injection' denied: ...
[3] dangerous_tool result: I am not allowed to use that tool.
    reason: completed
```

The third line is up to the model — but `dangerous_tool` will not have
been executed (no `executed dangerous_tool` substring in the output).

## See also

- [`../../../sdk/python/README.md`](../../../sdk/python/README.md)
- `internal/engine/builtin/registry.go` — built-in guard catalogue.
- ADR-012 (PermissionChecker + GuardRegistry layers)
