# 01 · Minimal chat (Python)

Spawn the agent, send one prompt, print the answer, exit. No tools, no
guards, no streaming — the absolute minimum surface to see the loop work.

## Run

```bash
pip install -e ../../../sdk/python
python main.py
```

## Expected output

```
response: 今日の天気は晴れです。
reason:   completed
turns:    1
```

The exact `response` text depends on your SLLM, but `reason` should be
`completed` and `turns` should be `1`–`2`.

## See also

- [`../../../sdk/python/README.md`](../../../sdk/python/README.md)
- ADR-001 (JSON-RPC over stdio)
