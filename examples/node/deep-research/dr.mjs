// Deep Research, Node.js から直接 SDK を叩く版。LM Studio (OpenAI 互換 HTTP) を LLM に使う。
// 使い方: PROMPT="..." node dr.mjs

import { Agent, tool } from '@ai-agent/browser';

const ENDPOINT = process.env.SLLM_ENDPOINT || 'http://localhost:8080/v1/chat/completions';
const API_KEY = process.env.SLLM_API_KEY || 'sk-gemma4';
const MODEL = process.env.SLLM_MODEL || 'gemma-4-E2B-it-Q4_K_M';

const PROMPT =
  process.env.PROMPT ||
  'Briefly research TypeScript and current trending tech topics. Use search_wikipedia (with disambiguated query) and hn_top_stories. Then write a structured report.';

// --- HTTP Completer (OpenAI 互換) -----------------------------------------
class HttpCompleter {
  ready = true;
  constructor({ endpoint, apiKey, model, temperature }) {
    this.endpoint = endpoint;
    this.apiKey = apiKey;
    this.model = model;
    this.defaultTemp = temperature;
  }

  toPayload(req, stream) {
    const out = {
      model: this.model,
      messages: req.messages.map((m) => ({
        role: m.role,
        content: m.content,
        ...(m.name ? { name: m.name } : {}),
        ...(m.tool_call_id ? { tool_call_id: m.tool_call_id } : {}),
        ...(m.tool_calls ? { tool_calls: m.tool_calls } : {}),
      })),
      stream,
    };
    if (req.temperature !== undefined) out.temperature = req.temperature;
    else if (this.defaultTemp !== undefined) out.temperature = this.defaultTemp;
    if (req.max_tokens !== undefined) out.max_tokens = req.max_tokens;
    if (req.response_format) {
      // LM Studio / llama-server は schema 付き json_schema 形式に対応
      const rf = req.response_format;
      // LM Studio (llama-server) は json_schema 型をサポートしていないため
      // type: "json_object" のみを送る。enum 強制はプロンプト主導に頼る。
      out.response_format = { type: 'json_object' };
    }
    return out;
  }

  async chatCompletion(req) {
    const r = await fetch(this.endpoint, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...(this.apiKey ? { Authorization: `Bearer ${this.apiKey}` } : {}),
      },
      body: JSON.stringify(this.toPayload(req, false)),
    });
    if (!r.ok) {
      const body = await r.text();
      throw new Error(`HTTP ${r.status}: ${body.slice(0, 500)}`);
    }
    const resp = await r.json();
    // Gemma 4 の internal thinking block (<|channel>thought ... <channel|>) を除去。
    // LM Studio の chat template が除去しきれずに残るケースがある。
    for (const choice of resp.choices || []) {
      if (typeof choice.message?.content === 'string') {
        choice.message.content = stripThinking(choice.message.content);
      }
    }
    return resp;
  }
}

function stripThinking(text) {
  let out = text;
  // Gemma の channel トークン
  out = out.replace(/<\|?channel\|?>\s*thought[\s\S]*?<\/?channel\|?>/gi, '');
  out = out.replace(/<\|?thinking\|?>[\s\S]*?<\/?thinking\|?>/gi, '');
  // Gemma の tool calling テンプレートが漏れたケース
  out = out.replace(/<\|?tool_call\|?>[\s\S]*?<\/?tool_call\|?>/gi, '');
  out = out.replace(/<\|?tool_response\|?>[\s\S]*?<\/?tool_response\|?>/gi, '');
  // Llama 系の <think>...</think>
  out = out.replace(/<think>[\s\S]*?<\/think>/gi, '');
  // 単発のチャットテンプレート閉じ忘れ ("<|channel|>" 単体など) — 1 行ずつスペース化
  out = out.replace(/<\|[^>]*\|>/g, '');
  out = out.replace(/<[a-z_]+\|>/g, '');
  return out.trim();
}

// --- ツール群 (URL 不要のものに絞る) ---------------------------------------

const calcTool = tool({
  name: 'calculator',
  description: 'Evaluate a basic arithmetic expression with + - * / and parens.',
  parameters: {
    type: 'object',
    properties: { expression: { type: 'string' } },
    required: ['expression'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: ({ expression }) => {
    const expr = String(expression ?? '');
    if (!/^[\d\s+\-*/().]+$/.test(expr)) {
      return { content: 'Error: only digits and + - * / ( ) are allowed.', is_error: true };
    }
    return String(new Function(`return (${expr});`)());
  },
});

const searchWikipediaTool = tool({
  name: 'search_wikipedia',
  description:
    'Search Wikipedia, return summaries of top 3 results. Required: query. For ambiguous topics, disambiguate (e.g. "Rust (programming language)").',
  parameters: {
    type: 'object',
    properties: {
      query: { type: 'string' },
      lang: { type: 'string', description: '"en" or "ja" (default "en")' },
    },
    required: ['query'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: async ({ query, lang }) => {
    if (!query || typeof query !== 'string' || !query.trim()) {
      return { content: 'search_wikipedia: query is required', is_error: true };
    }
    const language = lang || 'en';
    try {
      const sr = await fetch(
        `https://${language}.wikipedia.org/w/api.php?action=opensearch&format=json&origin=*&limit=3&search=${encodeURIComponent(query)}`,
      );
      const data = await sr.json();
      const titles = data[1] || [];
      if (titles.length === 0) return 'No results.';
      const summaries = [];
      for (const title of titles) {
        try {
          const sm = await fetch(
            `https://${language}.wikipedia.org/api/rest_v1/page/summary/${encodeURIComponent(title)}`,
          );
          const j = await sm.json();
          summaries.push(`### ${j.title}\n${j.extract || '(no extract)'}`);
        } catch {
          summaries.push(`### ${title}\n(summary fetch failed)`);
        }
      }
      return summaries.join('\n\n');
    } catch (err) {
      return { content: `search failed: ${err.message}`, is_error: true };
    }
  },
});

const hnTopStoriesTool = tool({
  name: 'hn_top_stories',
  description:
    'Fetch the current top N (default 5, max 10) Hacker News stories with titles, URLs, scores, comments. No URL parameter needed.',
  parameters: {
    type: 'object',
    properties: { count: { type: 'number' } },
    additionalProperties: false,
  },
  readOnly: true,
  handler: async ({ count }) => {
    const n = typeof count === 'number' ? Math.min(Math.max(count, 1), 10) : 5;
    try {
      const ids = await (await fetch('https://hacker-news.firebaseio.com/v0/topstories.json')).json();
      const items = await Promise.all(
        ids.slice(0, n).map(async (id) => {
          const r = await fetch(`https://hacker-news.firebaseio.com/v0/item/${id}.json`);
          return await r.json();
        }),
      );
      return items
        .map((it, i) => {
          const url = it.url ? `\n   ${it.url}` : '';
          return `${i + 1}. ${it.title}${url}\n   (${it.score} points, ${it.descendants ?? 0} comments)`;
        })
        .join('\n');
    } catch (err) {
      return { content: `hn_top_stories failed: ${err.message}`, is_error: true };
    }
  },
});

const currentTimeTool = tool({
  name: 'get_current_time',
  description: 'Return current ISO time and a localized human-readable form. Optional IANA timezone.',
  parameters: {
    type: 'object',
    properties: { timezone: { type: 'string' } },
    additionalProperties: false,
  },
  readOnly: true,
  handler: ({ timezone }) => {
    const tz = timezone || 'UTC';
    const now = new Date();
    try {
      const fmt = new Intl.DateTimeFormat('ja-JP', { dateStyle: 'full', timeStyle: 'long', timeZone: tz });
      return `ISO: ${now.toISOString()}\n${tz}: ${fmt.format(now)}`;
    } catch {
      return { content: `bad timezone "${tz}"`, is_error: true };
    }
  },
});

// --- Deep Research configure ---------------------------------------------

const DEEP_RESEARCH_PROMPT = `You are a research agent doing deep, multi-source investigation.

HARD RULES:
- You MUST call AT LEAST 2 DIFFERENT tools (different tool names) before selecting "none".
- On turn 1 you MUST call a tool, never "none".
- ALWAYS provide all REQUIRED arguments. NEVER call a tool with empty {} arguments.
- For search_wikipedia, "query" is required. For ambiguous topics, disambiguate (e.g. "Rust (programming language)").
- For hn_top_stories, no arguments needed.
- If a tool returns the wrong topic, refine the query — do NOT retry with the exact same arguments.
- After 2+ distinct tool calls, select "none" and write a structured report.

When you write the FINAL ANSWER (plain Markdown prose, NOT JSON):

## Summary
<2-3 line overview synthesized from tool outputs>

## Key facts
- fact 1 (source: tool name)
- fact 2 (source)
- fact 3 (source)

## Answer
<final concise answer>

CRITICAL OUTPUT RULES for the final answer:
- Write Markdown PROSE only. Do NOT output JSON, JS objects, or "[tool_result ...]" markers.
- Do NOT make up tool results. Use only what the tools actually returned in this conversation.
- Do NOT invent URLs like "example.com/...".
- If a tool returned "No results." or empty content, state that explicitly — DO NOT invent facts to fill the gap.
- Distinguish between facts FROM tool output and your own general knowledge. If you must fall back to general knowledge, write "(general knowledge, not from tool output)".
- Cite sources by tool name in parentheses, e.g. "(source: search_wikipedia)".`;

// --- main -----------------------------------------------------------------

const llm = new HttpCompleter({ endpoint: ENDPOINT, apiKey: API_KEY, model: MODEL, temperature: 0.3 });

console.log(`# Deep Research`);
console.log(`Endpoint: ${ENDPOINT}`);
console.log(`Model:    ${MODEL}`);
console.log(`Prompt:   ${PROMPT}`);
console.log('');

const agent = new Agent({ llm });
agent.registerTools(calcTool, searchWikipediaTool, hnTopStoriesTool, currentTimeTool);

await agent.configure({
  max_turns: 12,
  system_prompt: DEEP_RESEARCH_PROMPT,
  min_tool_kinds: 2,
  verify: { verifiers: ['non_empty'], max_consecutive_failures: 8 },
});

const startedAt = Date.now();
let turnCount = 0;
const toolNames = new Set();

const result = await agent.run(PROMPT, {
  onEvent: (ev) => {
    if (ev.kind === 'turn_start') {
      turnCount = ev.turn;
      process.stderr.write(`\n--- turn ${ev.turn} ---\n`);
    } else if (ev.kind === 'router') {
      process.stderr.write(`router: ${ev.decision.tool} (${ev.decision.reasoning?.slice(0, 80) || ''})\n`);
    } else if (ev.kind === 'tool_call') {
      process.stderr.write(`tool:   ${ev.name}(${JSON.stringify(ev.args)})\n`);
    } else if (ev.kind === 'tool_result') {
      const preview = String(ev.result.content || '').slice(0, 100).replace(/\n/g, ' ');
      process.stderr.write(`result: ${ev.name} → ${preview}${preview.length >= 100 ? '...' : ''}\n`);
      if (!ev.result.is_error) toolNames.add(ev.name);
    }
  },
});

const elapsed = ((Date.now() - startedAt) / 1000).toFixed(1);

console.log('\n=== FINAL RESPONSE ===');
console.log(result.response);
console.log('');
console.log(`Turns: ${result.turns}, reason: ${result.reason}, elapsed: ${elapsed}s`);
console.log(`Tools used (${toolNames.size}): ${[...toolNames].join(', ')}`);
console.log(`Tokens: prompt=${result.usage.prompt_tokens}, completion=${result.usage.completion_tokens}, total=${result.usage.total_tokens}`);
