# llm.execute

Sent by the core when `agent.configure` has activated `llm.mode="remote"`. Instead of dialing the OpenAI-compatible HTTP endpoint, the core forwards every `ChatCompletion` here so the wrapper can translate it into any backend format (Anthropic, Bedrock, Vertex AI, ollama, mock, ...).

## Direction

Core → Wrapper (request / response). Implements ADR-016 (`llm.execute` reverse RPC).

## Activation

By default the core uses its built-in OpenAI-compatible HTTP client (`SLLM_ENDPOINT`). To divert calls to the wrapper:

```jsonc
// agent.configure params
{
  "llm": {
    "mode": "remote",
    "timeout_seconds": 120  // optional; 0/omitted -> 120s default
  }
}
```

Once configured, `SLLM_ENDPOINT` is no longer touched.

## Request

```json
{
  "jsonrpc": "2.0",
  "method": "llm.execute",
  "params": {
    "request": {
      "model": "gpt-4o-mini",
      "messages": [
        { "role": "system", "content": "..." },
        { "role": "user", "content": "hi" }
      ],
      "temperature": 0.0,
      "response_format": { "type": "json_object" },
      "tools": []
    }
  },
  "id": 42
}
```

## Params

| field | type | required | description |
|---|---|---|---|
| `request` | object | yes | Opaque OpenAI-compatible `ChatRequest`. Forwarded verbatim as `json.RawMessage`, so additional fields (router JSON mode, tools, stop sequences, ...) pass through unchanged. |

The wrapper should inspect at least:

- `messages` (`[{role, content, tool_calls?, tool_call_id?}]`) — conversation history.
- `model` — string identifier; map to your backend's model id.
- `response_format.type === "json_object"` — router step is asking for JSON. The wrapper **must** return strictly-valid JSON in `choices[0].message.content` when this is set.
- `tools` — function-call tool definitions (currently unused by the harness, but forwarded if you ever set them).
- `temperature` / `max_tokens` — passed through if present.

## Result

The wrapper must reply with:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "response": {
      "id": "wrapper-1",
      "object": "chat.completion",
      "created": 0,
      "model": "gpt-4o-mini",
      "choices": [
        {
          "index": 0,
          "message": { "role": "assistant", "content": "hello back" },
          "finish_reason": "stop"
        }
      ],
      "usage": { "prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3 }
    }
  },
  "id": 42
}
```

| field | type | required | description |
|---|---|---|---|
| `response` | object | yes | Opaque OpenAI-compatible `ChatResponse`. Must include `choices[0].message.content` (or `choices[0].message.tool_calls`) and `finish_reason`. `usage` is consumed but optional. |

## Errors

If the wrapper raises an exception or returns an error JSON-RPC response, the core wraps the failure as `remote llm: wrapper returned error: <message>` and propagates it up to `agent.run`. Use `-32603 Internal` (`protocol.ErrCodeInternal`) for generic failures.

## Streaming

When `streaming.enabled=true` is combined with `llm.mode="remote"`, the engine falls back to non-streaming (the `RemoteCompleter` does not implement `StreamingCompleter`). `stream.delta` notifications are not emitted; the final response is delivered as a single chunk after the handler returns.

## Examples

### Python SDK

```python
from ai_agent import Agent, AgentConfig

def my_llm(request: dict) -> dict:
    # Translate to your backend (Anthropic / Bedrock / ollama / mock / ...)
    messages = request["messages"]
    # ... call your real API ...
    return {
        "choices": [{
            "index": 0,
            "message": {"role": "assistant", "content": "..."},
            "finish_reason": "stop",
        }],
        "usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
    }

# llm_handler implies llm.mode="remote" automatically
async with Agent(AgentConfig(binary="./agent", llm_handler=my_llm)) as agent:
    print(await agent.input("hi"))
```

### TypeScript SDK (Node)

```ts
import { Agent, type LLMHandler } from '@ai-agent/sdk';

const myLLM: LLMHandler = async (request) => ({
  choices: [{
    index: 0,
    message: { role: 'assistant', content: '...' },
    finish_reason: 'stop',
  }],
  usage: { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 },
});

const agent = new Agent({ binaryPath: './agent' });
await agent.start();
agent.setLLMHandler(myLLM);
await agent.configure({ llm: { mode: 'remote' } });
const r = await agent.run('hi');
```

### Swift SDK

```swift
import AIAgent

let myLLM: LLMHandler = { request in
    return .object([
        "choices": .array([
            .object([
                "index": .int(0),
                "message": .object([
                    "role": .string("assistant"),
                    "content": .string("..."),
                ]),
                "finish_reason": .string("stop"),
            ])
        ]),
        "usage": .object([
            "prompt_tokens": .int(0),
            "completion_tokens": .int(0),
            "total_tokens": .int(0),
        ]),
    ])
}

let agent = Agent(config: AgentConfig(
    binary: "./agent",
    llmHandler: myLLM   // implies llm.mode=.remote
))
```

## Related

- [agent.configure](./agent.configure.md) — sets `llm.mode` and `llm.timeout_seconds`.
- [ADR-016: `llm.execute` reverse RPC](../../../.claude/skills/decisions/016-llm-execute-reverse-rpc.md)
- [ADR-013: RemoteTool + PendingRequests](../../../.claude/skills/decisions/013-rpc-remote-tool-pending-requests.md) — the same pattern this method follows.
