# 05 · Custom remote guard (TypeScript)

Builds an input guard with `inputGuard(name, fn)` from `@ai-agent/sdk`,
registers it via `agent.registerGuards`, and references its name in
`AgentConfig.guards.input`. The Go core then calls back into JS for
every prompt before any model work.

The guard denies any input containing the substring `internal-only`.

## Run

```bash
( cd ../../../sdk/js && npm install && npm run build )
npm install
npx tsx main.ts
```

## Expected output

```
[1] allowed: Octopuses have three hearts and blue, copper-based blood.
[2] denied: input guard 'internal_keyword' denied: input contains the 'internal-only' marker
```

The first run reaches the SLLM; the second is short-circuited at the
input-guard stage and the SDK throws `GuardDenied`.

## See also

- [`../../../sdk/js/README.md`](../../../sdk/js/README.md)
- ADR-013 (RemoteTool / RemoteGuard adapter pattern)
