# 01 · Minimal chat (TypeScript)

Spawn the agent, send one prompt, print the answer, exit. Mirrors the
Python `01_minimal_chat` example.

## Run

```bash
# 1. build the SDK once (from repo root)
( cd ../../../sdk/js && npm install && npm run build )

# 2. install + run this example
npm install
npx tsx main.ts
```

## Expected output

```
response: 今日は晴れです。
reason:   completed
turns:    1
```

`reason` should be `completed`; `turns` should be `1`–`2`.

## See also

- [`../../../sdk/js/README.md`](../../../sdk/js/README.md)
- ADR-001 (JSON-RPC over stdio)
