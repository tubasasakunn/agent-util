# Builtins

Names available out of the box. Use them in [`agent.configure`](./methods/agent.configure.md) without registering anything via [`guard.register`](./methods/guard.register.md) / [`verifier.register`](./methods/verifier.register.md).

Source files:

- Guards — [`internal/engine/builtin/guards.go`](../../internal/engine/builtin/guards.go)
- Verifiers — [`internal/engine/builtin/verifiers.go`](../../internal/engine/builtin/verifiers.go)
- Summarizers — [`internal/engine/builtin/summarizer.go`](../../internal/engine/builtin/summarizer.go)
- Name-to-instance lookup — [`internal/engine/builtin/registry.go`](../../internal/engine/builtin/registry.go)

## Input Guards

Used in `agent.configure` `guards.input`.

| Name | Behaviour |
|---|---|
| `prompt_injection` | Detects classic injection patterns (case-insensitive): `ignore (all\|prior\|previous) instructions/prompts/context`, `disregard ... above`, `you are now a ...`, leading `system:` markers, `reveal ... system prompt`. |
| `max_length` | Rejects inputs over 50 000 characters (`DefaultMaxInputLength`). |

## Tool Call Guards

Used in `agent.configure` `guards.tool_call`. Triggered when the tool name looks shell-ish (`bash`, `sh`, anything containing `shell` or `exec`).

| Name | Behaviour |
|---|---|
| `dangerous_shell` | Looks at the `command` / `cmd` / `script` argument and denies on: `rm -rf /` (or `~`, `$HOME`, `*`), fork bomb (`:(){ :\|:& };:`), `mkfs.*`, `dd if=... of=/dev/...`, `chmod -R 777 /`, raw writes to `/dev/sd*`, `shutdown`, `reboot`. |

## Output Guards

Used in `agent.configure` `guards.output`.

| Name | Behaviour |
|---|---|
| `secret_leak` | Denies when the output matches any of: OpenAI keys (`sk-...`), Anthropic keys (`sk-ant-...`), AWS access keys (`AKIA...`), GitHub PATs (`ghp_...`), Slack tokens (`xox[baprs]-...`), or PEM-formatted private keys (`-----BEGIN ... PRIVATE KEY-----`). |

## Verifiers

Used in `agent.configure` `verify.verifiers`.

| Name | Behaviour |
|---|---|
| `non_empty` | Fails when `strings.TrimSpace(result) == ""`. |
| `json_valid` | When the result starts with `{` or `[`, parses it with `json.Unmarshal` and fails if invalid. Otherwise passes through with `summary: "not JSON-shaped, skipped"`. |

## Compaction Summarizers

Used in `agent.configure` `compaction.summarizer`. Empty string means "skip Stage 4".

| Name | Behaviour |
|---|---|
| `llm` | Sends the dropped messages to the configured LLM with a fixed instruction asking for a summary under 500 chars that preserves key decisions, file paths, identifiers, tool results, and unresolved tasks. The first choice's content (trimmed) becomes the summary. |

## Decisions and results

Built-in guards return `decision: "deny"` (never `tripwire`) — they are designed for routine policy enforcement. To stop the loop emergency-style, register a wrapper-side guard that returns `tripwire`.

Built-in verifiers always return `passed: true` for empty / non-JSON-shaped results (skip-on-shape-mismatch). The `non_empty` verifier is the one to combine with `json_valid` if you want to enforce both presence and shape.

## Related

- [guards](./concepts/guards.md)
- [verify](./concepts/verify.md)
- [compaction](./concepts/compaction.md)
- [agent.configure](./methods/agent.configure.md)
