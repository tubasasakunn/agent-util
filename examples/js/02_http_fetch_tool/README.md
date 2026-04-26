# 02 · HTTP fetch tool (TypeScript)

Registers a single `fetch_url(url)` tool with `tool()` from
`@ai-agent/sdk`, allows it via `permission.allow`, and asks the model to
fetch `https://example.com` and report the title.

## Run

```bash
( cd ../../../sdk/js && npm install && npm run build )
npm install
npx tsx main.ts
```

## Expected output

```
response: The page title is "Example Domain".
---
reason: completed | turns: 2
```

The model has to (a) call `fetch_url`, (b) parse the HTML in the
returned snippet, (c) reply in natural language. `turns` is therefore
≥ 2.

## Notes

- The tool truncates to 4 KiB to keep router context small. Larger pages
  would push the SLLM past its window.
- For real-world use, wrap external HTTP in a tool-call guard (a
  follow-up to example 03) to enforce allow-listed hosts.

## See also

- [`../../../sdk/js/README.md`](../../../sdk/js/README.md)
- ADR-002 (router + JSON mode)
