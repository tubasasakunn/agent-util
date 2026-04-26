# ai-agent examples

Copy-paste runnable examples for the [ai-agent](../) Go harness, in both
Python and TypeScript. Every example spawns a child `agent --rpc` process
and talks to it over JSON-RPC via the official SDKs in [`../sdk/`](../sdk/).

## Prerequisites

- **Go 1.25+** — needed once to build the agent binary.
- **Python 3.10+** — for the `python/` examples.
- **Node.js 20+** (or Bun / Deno with `node:` polyfills) — for the `js/` examples.
- **An OpenAI-compatible SLLM server** at the endpoint of your choice.
  The repo defaults assume Gemma-4 E2B served by LM Studio on `localhost:8080`.

### 1. Build the agent binary (once)

From the repository root:

```bash
go build -o agent ./cmd/agent/
```

The examples reference this binary via the relative path
`../../../agent` (each example sits three levels deep in `examples/`).

### 2. Start an SLLM server

Any OpenAI-compatible chat-completions endpoint works. The repo's reference
setup:

- Endpoint: `http://localhost:8080/v1/chat/completions`
- API key:  `sk-gemma4`
- Model:    `gemma-4-E2B-it-Q4_K_M`

Easiest route: open **LM Studio**, load Gemma 4 E2B, click **Start Server**.
Anything that speaks the OpenAI Chat Completions protocol works (vLLM,
Ollama with the OpenAI shim, llama.cpp's `server`, etc.).

Export the connection details once per shell session:

```bash
export SLLM_ENDPOINT="http://localhost:8080/v1/chat/completions"
export SLLM_API_KEY="sk-gemma4"
# Optional:
# export SLLM_MODEL="gemma-4-E2B-it-Q4_K_M"
# export SLLM_CONTEXT_SIZE="8192"
```

The examples fall back to these defaults when the env vars are unset.

### 3. Install the SDK you want to use

**Python** (from `examples/python/<name>/`):

```bash
pip install -e ../../../sdk/python
```

**TypeScript** (from `examples/js/<name>/`):

```bash
npm install
# then either
npx tsx main.ts
# or
node --experimental-strip-types main.ts
```

The JS examples reference the SDK with `"@ai-agent/sdk": "file:../../../sdk/js"`,
so make sure the SDK is built first:

```bash
( cd ../../sdk/js && npm install && npm run build )
```

## Examples

### Python

| #  | Folder                                                            | Shows                                                      |
| -- | ----------------------------------------------------------------- | ---------------------------------------------------------- |
| 01 | [`python/01_minimal_chat/`](./python/01_minimal_chat/)            | Smallest possible run — spawn, prompt, print, close.       |
| 02 | [`python/02_file_reader_tool/`](./python/02_file_reader_tool/)    | Register a `read_file` tool with `@tool`.                  |
| 03 | [`python/03_guards_and_permission/`](./python/03_guards_and_permission/) | Built-in `prompt_injection` / `secret_leak` + `permission.deny`. |
| 04 | [`python/04_streaming/`](./python/04_streaming/)                  | Live `stream.delta` printing via `stream=` callback.       |
| 05 | [`python/05_custom_remote_guard/`](./python/05_custom_remote_guard/) | Wrapper-side `@input_guard` registered with the core.    |

### TypeScript

| #  | Folder                                                          | Shows                                                      |
| -- | --------------------------------------------------------------- | ---------------------------------------------------------- |
| 01 | [`js/01_minimal_chat/`](./js/01_minimal_chat/)                  | Smallest possible run with `Agent.open()`.                 |
| 02 | [`js/02_http_fetch_tool/`](./js/02_http_fetch_tool/)            | Register an HTTP `fetch_url` tool with `tool()`.           |
| 03 | [`js/03_guards_and_permission/`](./js/03_guards_and_permission/) | Built-in guards + permission `deny`.                       |
| 04 | [`js/04_streaming/`](./js/04_streaming/)                        | `runStream()` AsyncIterable with `for await`.              |
| 05 | [`js/05_custom_remote_guard/`](./js/05_custom_remote_guard/)    | Wrapper-side `inputGuard()` registered with the core.      |

## Troubleshooting

**`connection refused` / endpoint timeouts.**
Confirm the SLLM server is up: `curl -s "$SLLM_ENDPOINT" -H "Authorization: Bearer $SLLM_API_KEY"`
should return JSON (even an error JSON), not a TCP failure. Re-export
`SLLM_ENDPOINT` after starting the server.

**`agent binary not found` / `ENOENT spawn`.**
Build the binary at the repo root: `go build -o agent ./cmd/agent/`. The
examples assume `../../../agent` from each example folder.

**JSON parse / router errors mid-run.**
Small models occasionally emit malformed JSON for the router. The agent
retries automatically; if you see repeated `parse_error_retry` messages on
stderr, raise the model size or shrink your prompt. See ADR-002 for the
router design.

**`max_turns reached` Terminal reason.**
The default per-call cap is small. Raise it via
`AgentConfig(max_turns=20)` (Python) or `agent.configure({ max_turns: 20 })`
(TypeScript) before calling `run()`.

**`AgentBusy` / "agent.run already in progress".**
A single `Agent` instance allows one in-flight run at a time. Either await
the previous run, or open a second `Agent` (separate subprocess).

**Guard `denied` immediately.**
A registered guard rejected the input. Inspect the raised `GuardDenied`
exception (Python) or check the `reason` field on the JSON-RPC error
(TypeScript). Built-in `prompt_injection` flags inputs that look like
"ignore previous instructions"; rephrase to test.

## See also

- [`../docs/openrpc.json`](../docs/openrpc.json) — full JSON-RPC spec.
- [`../sdk/python/README.md`](../sdk/python/README.md) — Python SDK reference.
- [`../sdk/js/README.md`](../sdk/js/README.md) — TypeScript SDK reference.
- ADRs: ADR-001 (JSON-RPC over stdio), ADR-012 (guards + permissions),
  ADR-013 (RemoteTool adapter pattern).
