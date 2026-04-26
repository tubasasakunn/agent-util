# Errors

All errors follow JSON-RPC 2.0 error semantics:

```json
{
  "jsonrpc": "2.0",
  "error": { "code": -32602, "message": "Invalid params" },
  "id": 1
}
```

Codes are mirrored from [`pkg/protocol/errors.go`](../../pkg/protocol/errors.go).

## Error code reference

| Code | Name | Source | Recovery |
|---|---|---|---|
| `-32700` | `ParseError` | Wrapper sent invalid JSON | Fix wrapper serialization. |
| `-32600` | `InvalidRequest` | Malformed JSON-RPC envelope | Fix wrapper (missing `jsonrpc` / `method`). |
| `-32601` | `MethodNotFound` | Unknown method name | Check `info.version` of [`../openrpc.json`](../openrpc.json) and ensure the wrapper targets the same. |
| `-32602` | `InvalidParams` | Wrong params shape (missing required fields, type mismatch, unknown enum) | Validate against [`../schemas/`](../schemas/) before sending. |
| `-32603` | `InternalError` | Core raised an unexpected error | Report a bug. Include the `data` field if present. |
| `-32000` | `ToolNotFound` | `tool.execute` for an unregistered tool | Check `tool.register` ran successfully and the names match. |
| `-32001` | `ToolExecFailed` | Wrapper-side tool raised an exception | Inspect args; treat like an `is_error: true` result. |
| `-32002` | `AgentBusy` | Concurrent `agent.run` / `agent.configure` | Serialize calls per agent instance. |
| `-32003` | `Aborted` | `agent.abort` was processed before a result was emitted | Treat as normal flow when the wrapper invoked `agent.abort` itself. |
| `-32004` | `MessageTooLarge` | Message exceeded 10 MiB | Reduce payload size; chunk outputs. |

The `-32700` to `-32603` range is the JSON-RPC 2.0 standard. The `-32000` to `-32099` range is reserved for application-specific errors (the `-32000` ... `-32004` codes above).

## `agent.run` `reason` reference

`agent.run` always succeeds at the JSON-RPC layer; the loop's outcome is encoded in the `reason` field of the result.

| Reason | Meaning | Wrapper action |
|---|---|---|
| `completed` | Model produced a terminal answer. | Display `response`. |
| `max_turns` | Loop hit the configured `max_turns`. | Increase `max_turns` or change the prompt. |
| `context_overflow` | Even after compaction the context exceeds the limit. | Lower history size, raise `token_limit`, tune `compaction`. |
| `model_error` | Unrecoverable LLM error after retries. | Inspect logs; check the SLLM endpoint. |
| `aborted` | The wrapper called `agent.abort`. | Normal flow. |
| `input_denied` | An input guard returned `deny`. | Show the user the reason from the response. |
| `output_blocked` | An output guard returned `deny`. | Show a generic refusal. |
| `user_fixable` | LLM returned 400 / 401 / 403 / 404. | Surface the message to the user (auth, quota, etc.). |
| `max_consecutive_failures` | Too many failed turns in a row. | Inspect tool errors; relax verifiers; check the model. |

## Tripwire vs Aborted

A `tripwire` decision from a guard surfaces as an error from `agent.run` (the loop never gets a chance to return a result). The error code is `-32603 InternalError` with `data` containing the tripwire source (`"input"`, `"tool_call"`, `"output"`) and reason. This is intentional — tripwires are emergency stops, not normal flow.

`-32003 Aborted` is reserved for explicit `agent.abort` cancellation.

## Reference

- Source: [`pkg/protocol/errors.go`](../../pkg/protocol/errors.go)
- Engine error classification: [`internal/engine/errors.go`](../../internal/engine/errors.go)
- See [verify](./concepts/verify.md) for how the engine maps internal errors to the four error classes.
