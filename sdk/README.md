# ai-agent SDKs

Wrappers around the `ai-agent` agent loop. Three SDKs are available and pick a different deployment target — all of them expose the same shape of API (`Agent`, `tool()`, `inputGuard()`, `runStream`, ...) so user code stays portable.

| Language | Package                | Path                       | Runtime                                   |
| -------- | ---------------------- | -------------------------- | ----------------------------------------- |
| Python   | `ai-agent` (local)     | [`./python/`](./python/)   | Subprocess: `agent --rpc` over stdio      |
| TypeScript (Node) | `@ai-agent/sdk`     | [`./js/`](./js/)           | Subprocess: `agent --rpc` over stdio      |
| TypeScript (Browser) | `@ai-agent/browser` | [`./js-browser/`](./js-browser/) | In-page; LLM via WebLLM (WebGPU)   |

## Which SDK should I use?

| Need                                              | Pick                |
| ------------------------------------------------- | ------------------- |
| Server-side script, CI agent, CLI tool            | Node SDK or Python  |
| Web app where the LLM runs on the user's GPU      | Browser SDK         |
| MCP integration / sub-agents (`delegate_task`)    | Node SDK or Python  |
| Zero-server demo / offline-first                  | Browser SDK         |
| Python ecosystem / LangChain interop              | Python              |

## Node / Python: build the agent first

```bash
go build -o agent ./cmd/agent/
```

Both `Agent` classes spawn that binary and speak JSON-RPC 2.0 to it
([`docs/openrpc.json`](../docs/openrpc.json)).

## Browser: no build needed

```bash
cd examples/browser
npm install
npm run dev
```

Open the printed URL in any WebGPU-capable browser (Chrome 113+, Edge,
Safari 17.4+). The first run downloads ~1 GB of model weights into
IndexedDB; subsequent runs are instant.

## Feature parity

| Feature                       | Python | Node | Browser |
| ----------------------------- | :----: | :--: | :-----: |
| `agent.run` / streaming       |   x    |  x   |    x    |
| Tool registration             |   x    |  x   |    x    |
| Built-in guards / verifiers   |   x    |  x   |    x    |
| Custom guards / verifiers     |   x    |  x   |    x    |
| Permission policy             |   x    |  x   |  partial (no `ask`) |
| Router (JSON mode + jsonfix)  |   x    |  x   |    x    |
| Compaction (4-stage cascade)  |   x    |  x   | partial (no LLM stage) |
| `delegate_task`/`coordinate`  |   x    |  x   |   no    |
| MCP integration               |   x    |  x   |   no    |
| Worktree / SessionRunner      |   x    |  x   |   no    |
| Audit log                     |   x    |  x   |   no    |

For protocol-level documentation and ADRs, see [`../docs/`](../docs/) and
`/decision list` from the project root.
