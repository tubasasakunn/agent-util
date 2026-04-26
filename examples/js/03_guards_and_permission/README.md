# 03 · Guards and permission (TypeScript)

Three back-to-back scenarios on a single `Agent`:

1. A plain prompt is allowed.
2. A prompt-injection input is blocked by the built-in `prompt_injection`
   guard. The SDK throws `GuardDenied`.
3. A request that would call `dangerous_tool` is blocked by
   `permission.deny`; the model observes the rejection and replies in
   prose.

## Run

```bash
( cd ../../../sdk/js && npm install && npm run build )
npm install
npx tsx main.ts
```

## Expected output

```
[1] normal: 7 | reason: completed
[2] injection blocked: input guard 'prompt_injection' denied: ...
[3] dangerous_tool result: I'm not allowed to use that tool.
    reason: completed
```

The third line is the SLLM's natural-language reply; what matters is
that the literal string `executed dangerous_tool` does **not** appear.

## See also

- [`../../../sdk/js/README.md`](../../../sdk/js/README.md)
- `internal/engine/builtin/registry.go` — built-in guard catalogue.
- ADR-012 (PermissionChecker + GuardRegistry layers)
