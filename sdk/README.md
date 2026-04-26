# ai-agent SDKs

Wrappers around the Go `agent --rpc` JSON-RPC server. Pick the language you
need — they all speak the same protocol described in
[`../docs/openrpc.json`](../docs/openrpc.json).

| Language | Status | Path                       |
| -------- | ------ | -------------------------- |
| Python   | alpha  | [`./python/`](./python/)   |
| JavaScript / TypeScript | _planned_ | _todo_ |

All SDKs assume the user has built the agent binary first:

```bash
go build -o agent ./cmd/agent/
```

For protocol-level documentation and ADRs, see [`../docs/`](../docs/) and
`/decision list` from the project root.
