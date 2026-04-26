# 05 · Custom remote guard (Python)

Defines an input guard in Python with the `@input_guard` decorator,
registers it via `agent.register_guards`, and references its name from
`AgentConfig.guards.input`. The Go core then calls back into Python for
every prompt before any model work happens.

The example denies any input containing the substring `internal-only`.

## Run

```bash
pip install -e ../../../sdk/python
python main.py
```

## Expected output

```
[1] allowed: Octopuses have three hearts and blue, copper-based blood.
[2] denied: input guard 'internal_keyword' denied: input contains the 'internal-only' marker
```

The first run reaches the model; the second is short-circuited at the
input-guard stage and the SDK raises `GuardDenied`.

## See also

- [`../../../sdk/python/README.md`](../../../sdk/python/README.md)
- ADR-013 (RemoteTool / RemoteGuard adapter pattern)
