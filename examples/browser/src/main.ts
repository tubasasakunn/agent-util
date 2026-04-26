/**
 * Browser demo for @ai-agent/browser.
 *
 * Loads a WebLLM model in the page, registers a useful preset toolkit
 * (echo, calculator, fetch_url, fetch_markdown, extract_html,
 * search_wikipedia, get_current_time), wires the agent to a chat UI and
 * a side panel that shows router decisions, guard verdicts, tool calls,
 * and verifier outcomes in real time.
 */

import { Agent, tool, type LoopEvent } from '@ai-agent/browser';
import { WebLLMCompleter } from '@ai-agent/browser/llm';
// @ts-ignore — turndown ships its own types but vite resolves them at build time
import TurndownService from 'turndown';

// 公開 CORS proxy。デモ用、信頼性に注意。直接 fetch して CORS で失敗したら proxy 経由で再試行する。
const CORS_PROXY = 'https://corsproxy.io/?url=';

// WebLLM の prebuilt 一覧に未収録のモデル（Gemma 4 E2B など）を appConfig 経由で読み込むための設定。
// HuggingFace コミュニティが MLC 形式で公開しているリポジトリを指す。
const CUSTOM_APP_CONFIGS: Record<string, Record<string, unknown>> = {
  'gemma-4-E2B-it-q4f16_1-MLC': {
    model_list: [
      {
        model: 'https://huggingface.co/welcoma/gemma-4-E2B-it-q4f16_1-MLC',
        model_id: 'gemma-4-E2B-it-q4f16_1-MLC',
        model_lib:
          'https://huggingface.co/welcoma/gemma-4-E2B-it-q4f16_1-MLC/resolve/main/libs/gemma-4-E2B-it-q4f16_1-MLC-webgpu.wasm',
        required_features: ['shader-f16'],
        // Gemma 4 は sliding window attention を使う。WebLLM は context_window_size と
        // sliding_window_size の両方を正にできないので context_window_size を -1 に上書きする。
        overrides: {
          // sliding window を無効化し full attention のみに（Gemma 4 E2B のハイブリッド層を簡略）。
          // welcoma の mlc-chat-config.json は両方正で出荷されており WebLLM 0.2.82 では reject される。
          // context_window_size を 4096 のままで sliding を無効化することでロード可能になる。
          sliding_window_size: -1,
        },
      },
    ],
  },
};

async function fetchTextWithCorsFallback(url: string, maxBytes = 8192): Promise<string> {
  const tryFetch = async (target: string): Promise<string> => {
    const res = await fetch(target, { redirect: 'follow' });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const text = await res.text();
    return text.length > maxBytes ? text.slice(0, maxBytes) + '\n... [truncated]' : text;
  };
  try {
    return await tryFetch(url);
  } catch (e) {
    return await tryFetch(CORS_PROXY + encodeURIComponent(url));
  }
}

const $ = <T extends HTMLElement>(sel: string): T => {
  const el = document.querySelector<T>(sel);
  if (!el) throw new Error(`missing element: ${sel}`);
  return el;
};

// --- DOM handles ----------------------------------------------------------

const modelSelect = $<HTMLSelectElement>('#model-select');
const loadBtn = $<HTMLButtonElement>('#load-btn');
const deepResearchToggle = $<HTMLInputElement>('#deep-research-toggle');
const statusEl = $<HTMLDivElement>('#status');
const progress = $<HTMLProgressElement>('#progress');
const chat = $<HTMLDivElement>('#chat');
const inputForm = $<HTMLFormElement>('#input-form');
const promptEl = $<HTMLTextAreaElement>('#prompt');
const sendBtn = $<HTMLButtonElement>('#send-btn');
const toolList = $<HTMLUListElement>('#tool-list');
const guardList = $<HTMLUListElement>('#guard-list');
const trace = $<HTMLOListElement>('#trace');

// --- Tools ----------------------------------------------------------------

const echoTool = tool<{ message: string }>({
  name: 'echo',
  description: 'Repeat the input message back, prefixed with "echo:".',
  parameters: {
    type: 'object',
    properties: { message: { type: 'string', description: 'Text to echo' } },
    required: ['message'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: ({ message }) => `echo: ${String(message ?? '')}`,
});

const calcTool = tool<{ expression: string }>({
  name: 'calculator',
  description:
    'Evaluate a basic arithmetic expression involving + - * / and parentheses.',
  parameters: {
    type: 'object',
    properties: {
      expression: { type: 'string', description: 'e.g. "(3 + 4) * 5"' },
    },
    required: ['expression'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: ({ expression }) => {
    const expr = String(expression ?? '');
    if (!/^[\d\s+\-*/().]+$/.test(expr)) {
      return { content: 'Error: only digits and + - * / ( ) are allowed.', is_error: true };
    }
    try {
      // Restricted parser: regex above guarantees no identifiers.
      // eslint-disable-next-line @typescript-eslint/no-implied-eval, no-new-func
      const fn = new Function(`return (${expr});`);
      const value = fn();
      return String(value);
    } catch (err) {
      return { content: `Error: ${(err as Error).message}`, is_error: true };
    }
  },
});

const fetchTool = tool<{ url: string }>({
  name: 'fetch_url',
  description:
    'Fetch a URL and return raw text/HTML (up to 8KB). Auto-falls back to CORS proxy if direct fetch is blocked.',
  parameters: {
    type: 'object',
    properties: { url: { type: 'string', description: 'Absolute URL to fetch' } },
    required: ['url'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: async ({ url }) => {
    try {
      return await fetchTextWithCorsFallback(String(url), 8192);
    } catch (err) {
      return { content: `fetch failed: ${(err as Error).message}`, is_error: true };
    }
  },
});

const turndown = new TurndownService({ headingStyle: 'atx' });

const fetchMarkdownTool = tool<{ url: string }>({
  name: 'fetch_markdown',
  description:
    'Fetch a URL, convert its HTML to Markdown, and return up to 8KB. Best for reading articles/blog posts; strips scripts and styles.',
  parameters: {
    type: 'object',
    properties: { url: { type: 'string' } },
    required: ['url'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: async ({ url }) => {
    try {
      const html = await fetchTextWithCorsFallback(String(url), 64 * 1024);
      // <script> と <style> をざっくり除去してから markdown 化
      const cleaned = html
        .replace(/<script[\s\S]*?<\/script>/gi, '')
        .replace(/<style[\s\S]*?<\/style>/gi, '');
      const md = (turndown.turndown(cleaned) as string).trim();
      return md.length > 8192 ? md.slice(0, 8192) + '\n... [truncated]' : md;
    } catch (err) {
      return { content: `convert failed: ${(err as Error).message}`, is_error: true };
    }
  },
});

const extractHtmlTool = tool<{ url: string; selector: string; limit?: number }>({
  name: 'extract_html',
  description:
    'Fetch a URL and extract elements matching a CSS selector. Returns text content of up to N elements (default 10). Useful for scraping titles, links, list items.',
  parameters: {
    type: 'object',
    properties: {
      url: { type: 'string' },
      selector: { type: 'string', description: 'CSS selector, e.g. "h2", "a.title", "article h3"' },
      limit: { type: 'number', description: 'Max elements (default 10)' },
    },
    required: ['url', 'selector'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: async ({ url, selector, limit }) => {
    try {
      const html = await fetchTextWithCorsFallback(String(url), 256 * 1024);
      const doc = new DOMParser().parseFromString(html, 'text/html');
      const els = Array.from(doc.querySelectorAll(String(selector)));
      const max = typeof limit === 'number' && limit > 0 ? limit : 10;
      const picked = els.slice(0, max).map((el, i) => {
        const t = (el.textContent || '').trim().replace(/\s+/g, ' ');
        const href = el.getAttribute('href') || el.querySelector('a')?.getAttribute('href') || '';
        return `${i + 1}. ${t}${href ? ` [${href}]` : ''}`;
      });
      if (picked.length === 0) return `No elements matched "${selector}".`;
      return `Matched ${els.length} (showing ${picked.length}):\n` + picked.join('\n');
    } catch (err) {
      return { content: `extract failed: ${(err as Error).message}`, is_error: true };
    }
  },
});

const searchWikipediaTool = tool<{ query: string; lang?: string }>({
  name: 'search_wikipedia',
  description:
    'Search Wikipedia and return summaries of the top 3 results. Specify lang ("en" default, "ja" for Japanese).',
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
      return {
        content: 'search_wikipedia: required argument "query" is missing or empty. Provide a search term.',
        is_error: true,
      };
    }
    const language = (lang as string) || 'en';
    try {
      const searchUrl = `https://${language}.wikipedia.org/w/api.php?action=opensearch&format=json&origin=*&limit=3&search=${encodeURIComponent(String(query))}`;
      const sr = await fetch(searchUrl);
      const data = (await sr.json()) as [string, string[], string[], string[]];
      const titles = data[1] || [];
      const summaries: string[] = [];
      for (const title of titles) {
        const sumUrl = `https://${language}.wikipedia.org/api/rest_v1/page/summary/${encodeURIComponent(title)}`;
        try {
          const sm = await fetch(sumUrl);
          const j = await sm.json();
          summaries.push(`### ${j.title}\n${j.extract || '(no extract)'}`);
        } catch {
          summaries.push(`### ${title}\n(summary fetch failed)`);
        }
      }
      return summaries.length > 0 ? summaries.join('\n\n') : 'No results.';
    } catch (err) {
      return { content: `search failed: ${(err as Error).message}`, is_error: true };
    }
  },
});

const fetchJsonTool = tool<{ url: string; path?: string }>({
  name: 'fetch_json',
  description:
    'Fetch a JSON API endpoint and return parsed JSON. Many SPAs (Reddit, Hacker News, GitHub) expose JSON endpoints — use these instead of scraping HTML. Optional `path` extracts a sub-tree by dot-path (e.g. "data.children.0.data.title").',
  parameters: {
    type: 'object',
    properties: {
      url: { type: 'string', description: 'Absolute URL of a JSON endpoint' },
      path: { type: 'string', description: 'Dot-separated path into the JSON tree' },
    },
    required: ['url'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: async ({ url, path }) => {
    try {
      const txt = await fetchTextWithCorsFallback(String(url), 256 * 1024);
      const data = JSON.parse(txt) as unknown;
      let picked: unknown = data;
      if (typeof path === 'string' && path.length > 0) {
        for (const key of String(path).split('.')) {
          if (picked == null) break;
          const idx = /^\d+$/.test(key) ? Number(key) : key;
          picked = (picked as Record<string | number, unknown>)[idx as never];
        }
      }
      // path で undefined に到達した場合は明示的にエラーを返す（JSON.stringify(undefined) = undefined のため）
      if (picked === undefined) {
        return {
          content: `fetch_json: path "${path ?? ''}" did not resolve to a value`,
          is_error: true,
        };
      }
      const out = JSON.stringify(picked, null, 2);
      return out.length > 8192 ? out.slice(0, 8192) + '\n... [truncated]' : out;
    } catch (err) {
      return { content: `fetch_json failed: ${(err as Error).message}`, is_error: true };
    }
  },
});

const hnTopStoriesTool = tool<{ count?: number }>({
  name: 'hn_top_stories',
  description:
    'Fetch the current top N Hacker News stories (default 5, max 10) with title, URL, score, and comment count. No URL parameter needed — call this for "current tech news" / "trending HN" / "what is popular on Hacker News" questions.',
  parameters: {
    type: 'object',
    properties: { count: { type: 'number', description: 'How many stories (1-10)' } },
    additionalProperties: false,
  },
  readOnly: true,
  handler: async ({ count }) => {
    const n = typeof count === 'number' ? Math.min(Math.max(count, 1), 10) : 5;
    try {
      const idsRes = await fetch('https://hacker-news.firebaseio.com/v0/topstories.json');
      const ids = (await idsRes.json()) as number[];
      const top = ids.slice(0, n);
      const items = await Promise.all(
        top.map(async (id) => {
          const r = await fetch(`https://hacker-news.firebaseio.com/v0/item/${id}.json`);
          return (await r.json()) as Record<string, unknown>;
        }),
      );
      return items
        .map((it, i) => {
          const title = String(it.title ?? '(no title)');
          const url = it.url ? `\n   ${it.url}` : '';
          const score = it.score ?? '?';
          const comments = it.descendants ?? 0;
          return `${i + 1}. ${title}${url}\n   (${score} points, ${comments} comments)`;
        })
        .join('\n');
    } catch (err) {
      return { content: `hn_top_stories failed: ${(err as Error).message}`, is_error: true };
    }
  },
});

const fetchRssTool = tool<{ url: string; limit?: number }>({
  name: 'fetch_rss',
  description:
    'Fetch and parse an RSS or Atom feed. Returns the title, link, and summary of up to N items (default 10). Useful for blogs and news sites that expose feeds.',
  parameters: {
    type: 'object',
    properties: {
      url: { type: 'string', description: 'Absolute URL of an RSS/Atom feed' },
      limit: { type: 'number', description: 'Max items to return (default 10)' },
    },
    required: ['url'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: async ({ url, limit }) => {
    try {
      const xml = await fetchTextWithCorsFallback(String(url), 256 * 1024);
      const doc = new DOMParser().parseFromString(xml, 'application/xml');
      // RSS 2.0 と Atom の両方に対応
      const items = Array.from(doc.querySelectorAll('item, entry'));
      const max = typeof limit === 'number' && limit > 0 ? limit : 10;
      const out = items.slice(0, max).map((it, i) => {
        const title = it.querySelector('title')?.textContent?.trim() || '(no title)';
        const link =
          it.querySelector('link')?.textContent?.trim() ||
          it.querySelector('link')?.getAttribute('href') ||
          '';
        const desc =
          it.querySelector('description, summary, content')?.textContent?.trim().slice(0, 200) ||
          '';
        return `${i + 1}. ${title}\n   ${link}\n   ${desc}`;
      });
      if (out.length === 0) return 'No items found in feed.';
      return `Feed has ${items.length} items (showing ${out.length}):\n\n` + out.join('\n\n');
    } catch (err) {
      return { content: `fetch_rss failed: ${(err as Error).message}`, is_error: true };
    }
  },
});

const renderInIframeTool = tool<{ url: string; selector?: string; waitMs?: number }>({
  name: 'render_in_iframe',
  description:
    'Load a URL in a hidden iframe, wait for JS to render, then extract DOM. Works only for sites that allow iframe embedding (no X-Frame-Options: DENY). Use this for SPAs that need JS to populate content. `selector` defaults to "body", `waitMs` defaults to 3000.',
  parameters: {
    type: 'object',
    properties: {
      url: { type: 'string' },
      selector: { type: 'string', description: 'CSS selector to extract (default "body")' },
      waitMs: { type: 'number', description: 'ms to wait for JS rendering (default 3000)' },
    },
    required: ['url'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: async ({ url, selector, waitMs }) => {
    return new Promise<string | { content: string; is_error: true }>((resolve) => {
      const iframe = document.createElement('iframe');
      iframe.style.display = 'none';
      iframe.sandbox.add('allow-scripts', 'allow-same-origin');
      const cleanup = () => {
        try { document.body.removeChild(iframe); } catch { /* ignore */ }
      };
      const wait = typeof waitMs === 'number' ? waitMs : 3000;
      const timeoutId = setTimeout(() => {
        cleanup();
        resolve({ content: 'iframe load/render timed out (15s)', is_error: true });
      }, 15000);

      iframe.onload = () => {
        setTimeout(() => {
          clearTimeout(timeoutId);
          try {
            const doc = iframe.contentDocument;
            if (!doc) {
              cleanup();
              resolve({ content: 'iframe has no contentDocument (X-Frame-Options blocked)', is_error: true });
              return;
            }
            const sel = (selector as string) || 'body';
            const el = doc.querySelector(sel);
            const text = (el?.textContent || '').replace(/\s+/g, ' ').trim();
            const out = text.length > 8192 ? text.slice(0, 8192) + '\n... [truncated]' : text;
            cleanup();
            resolve(out || `(empty match for "${sel}")`);
          } catch (err) {
            cleanup();
            resolve({ content: `iframe access denied: ${(err as Error).message}`, is_error: true });
          }
        }, wait);
      };
      iframe.onerror = () => {
        clearTimeout(timeoutId);
        cleanup();
        resolve({ content: 'iframe failed to load', is_error: true });
      };
      iframe.src = String(url);
      document.body.appendChild(iframe);
    });
  },
});

const currentTimeTool = tool<{ timezone?: string }>({
  name: 'get_current_time',
  description: 'Return the current date/time in ISO format and a human-readable form. Optionally specify an IANA timezone (e.g. "Asia/Tokyo").',
  parameters: {
    type: 'object',
    properties: {
      timezone: { type: 'string', description: 'IANA tz, e.g. "Asia/Tokyo", "UTC"' },
    },
    additionalProperties: false,
  },
  readOnly: true,
  handler: ({ timezone }) => {
    const now = new Date();
    const tz = (timezone as string) || 'UTC';
    try {
      const fmt = new Intl.DateTimeFormat('ja-JP', {
        dateStyle: 'full',
        timeStyle: 'long',
        timeZone: tz,
      });
      return `ISO: ${now.toISOString()}\n${tz}: ${fmt.format(now)}`;
    } catch (err) {
      return { content: `bad timezone "${tz}": ${(err as Error).message}`, is_error: true };
    }
  },
});

// --- Render helpers -------------------------------------------------------

const ROLE_LABEL: Record<string, string> = { user: 'You', assistant: 'Agent' };

function addBubble(role: 'user' | 'agent' | 'system', text = ''): HTMLDivElement {
  const div = document.createElement('div');
  div.className = `bubble ${role}`;
  div.textContent = text;
  chat.appendChild(div);
  chat.scrollTop = chat.scrollHeight;
  return div;
}

function setStatus(html: string): void {
  statusEl.innerHTML = html;
}

function describe(event: LoopEvent): { cls: string; html: string } {
  switch (event.kind) {
    case 'turn_start':
      return { cls: 'turn', html: `<span class="tag">turn</span> ${event.turn}` };
    case 'router':
      return {
        cls: 'router',
        html: `<span class="tag">router</span> picked <strong>${escape(event.decision.tool)}</strong>${
          event.decision.reasoning
            ? ` <code>${escape(event.decision.reasoning)}</code>`
            : ''
        }`,
      };
    case 'router_error':
      return {
        cls: 'router deny',
        html: `<span class="tag">router</span> error: ${escape(event.error)}`,
      };
    case 'tool_call':
      return {
        cls: 'tool_call',
        html: `<span class="tag">tool</span> ${escape(event.name)}<code>${escape(JSON.stringify(event.args))}</code>`,
      };
    case 'tool_result': {
      const content = event.result.content || '';
      const trimmed = content.length > 200 ? content.slice(0, 200) + '...' : content;
      return {
        cls: 'tool_result',
        html: `<span class="tag">result</span> ${escape(event.name)}<code>${escape(trimmed)}</code>`,
      };
    }
    case 'guard':
      return {
        cls: `guard ${event.result.decision === 'deny' ? 'deny' : ''}`.trim(),
        html: `<span class="tag">guard</span> ${escape(event.stage)}/${escape(event.name)}: ${escape(event.result.decision)}${
          event.result.reason ? ` <code>${escape(event.result.reason)}</code>` : ''
        }`,
      };
    case 'permission':
      return {
        cls: `permission ${event.decision}`,
        html: `<span class="tag">perm</span> ${escape(event.tool)}: ${escape(event.decision)} <code>${escape(event.reason)}</code>`,
      };
    case 'verify':
      return {
        cls: `verify ${event.passed ? '' : 'fail'}`.trim(),
        html: `<span class="tag">verify</span> ${escape(event.tool)}: ${
          event.passed ? 'pass' : 'fail'
        }${event.summary ? ` <code>${escape(event.summary)}</code>` : ''}`,
      };
    case 'end':
      return {
        cls: 'turn',
        html: `<span class="tag">end</span> ${escape(event.result.reason)} (${event.result.turns} turns)`,
      };
    default:
      return { cls: '', html: escape(JSON.stringify(event)) };
  }
}

function escape(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

function pushTrace(event: LoopEvent): void {
  if (event.kind === 'delta') return;
  const { cls, html } = describe(event);
  const li = document.createElement('li');
  li.className = cls;
  li.innerHTML = html;
  trace.appendChild(li);
  trace.scrollTop = trace.scrollHeight;
}

// --- Init -----------------------------------------------------------------

let agent: Agent | null = null;
let llm: WebLLMCompleter | null = null;

function renderToolList(names: string[]): void {
  toolList.innerHTML = '';
  for (const n of names) {
    const li = document.createElement('li');
    li.textContent = n;
    toolList.appendChild(li);
  }
}

function renderGuardList(): void {
  const guards = ['input/prompt_injection', 'tool_call/dangerous_shell', 'output/secret_leak'];
  guardList.innerHTML = '';
  for (const g of guards) {
    const li = document.createElement('li');
    li.textContent = g;
    guardList.appendChild(li);
  }
}

renderToolList([
  'echo',
  'calculator',
  'fetch_url',
  'fetch_markdown',
  'extract_html',
  'fetch_json',
  'hn_top_stories',
  'fetch_rss',
  'render_in_iframe',
  'search_wikipedia',
  'get_current_time',
]);
renderGuardList();

// --- Load button ---------------------------------------------------------

loadBtn.addEventListener('click', async () => {
  const model = modelSelect.value;
  loadBtn.disabled = true;
  modelSelect.disabled = true;
  progress.hidden = false;
  setStatus(`Loading <code>${escape(model)}</code>...`);

  const customCfg = CUSTOM_APP_CONFIGS[model];
  llm = new WebLLMCompleter({
    model,
    temperature: 0.4,
    ...(customCfg ? { engineConfig: { appConfig: customCfg } } : {}),
  });

  try {
    await llm.load((report) => {
      progress.value = report.progress ?? 0;
      setStatus(`Loading <code>${escape(model)}</code>: ${escape(report.text)}`);
    });
  } catch (err) {
    setStatus(`Failed to load model: ${escape((err as Error).message)}`);
    loadBtn.disabled = false;
    modelSelect.disabled = false;
    progress.hidden = true;
    return;
  }

  progress.hidden = true;
  setStatus(`Model loaded — ready. Ask the agent something.`);

  await rebuildAgent();

  promptEl.disabled = false;
  sendBtn.disabled = false;
  promptEl.focus();
});

const DEEP_RESEARCH_SYSTEM_PROMPT = `You are a research agent doing deep, multi-source investigation.

HARD RULES — these override every other instruction:
- You MUST call AT LEAST 2 DIFFERENT tools (different tool names) before you may select "none".
- On turn 1 you MUST call a tool, never "none".
- On turn 2 you MUST call a DIFFERENT tool than turn 1.
- Only after 2+ distinct tool calls may you select "none" and write the final report.
- ALWAYS provide all REQUIRED arguments per the tool schema. NEVER call a tool with empty {} arguments.
- For search_wikipedia, the "query" string is required (e.g. {"query":"TypeScript"}).
- For hn_top_stories, no arguments are required (just call it).

Step-by-step procedure:
1. Turn 1 — background: call search_wikipedia (or fetch_url for a primary source).
2. Turn 2 — fresh data: call fetch_json (Hacker News, Reddit, GitHub APIs) or fetch_rss for current info.
3. Turn 3+ — optional deeper dives if facts are still unclear.
4. Once you have 2+ sources, select "none" and produce a structured report:
   ## Summary
   <2-3 line overview>
   ## Key facts
   - fact 1 (source)
   - fact 2 (source)
   - fact 3 (source)
   ## Answer
   <final concise answer>

Tool guide:
- search_wikipedia: encyclopedic background ("What is X?", history, definitions)
- hn_top_stories: trending tech news from Hacker News (NO url parameter needed — just call it)
- fetch_json: other JSON APIs (GitHub, Reddit .json suffix). Use ONLY with URLs explicitly given by the user.
- fetch_url / fetch_markdown: primary docs, blogs, official sites — only with URLs the user gave.
- fetch_rss: news feeds — only with URLs the user gave.
- render_in_iframe: SPA pages where fetch_url returns empty.
- get_current_time: when "today" / "now" matters.

CRITICAL: Never invent URLs. If you do not know an exact URL, use search_wikipedia or hn_top_stories instead.
Be precise. Cite sources by tool name. Never invent facts.`;

async function rebuildAgent(): Promise<void> {
  if (!llm) return;
  agent = new Agent({ llm });
  const deep = deepResearchToggle.checked;
  // Deep research 時は URL を引数に取るツールを除外（小型モデルが URL を幻覚するのを防ぐ）。
  const tools = deep
    ? [echoTool, calcTool, searchWikipediaTool, hnTopStoriesTool, currentTimeTool]
    : [
        echoTool,
        calcTool,
        fetchTool,
        fetchMarkdownTool,
        extractHtmlTool,
        fetchJsonTool,
        hnTopStoriesTool,
        fetchRssTool,
        renderInIframeTool,
        searchWikipediaTool,
        currentTimeTool,
      ];
  agent.registerTools(...(tools as Parameters<typeof agent.registerTools>));
  renderToolList(tools.map((t) => t.name));
  await applyAgentConfig();
}

async function applyAgentConfig(): Promise<void> {
  if (!agent) return;
  const deep = deepResearchToggle.checked;
  await agent.configure({
    max_turns: deep ? 12 : 6,
    streaming: { enabled: true },
    ...(deep ? { system_prompt: DEEP_RESEARCH_SYSTEM_PROMPT } : {}),
    min_tool_kinds: deep ? 2 : 0,
    guards: {
      input: ['prompt_injection'],
      tool_call: ['dangerous_shell'],
      output: ['secret_leak'],
    },
    verify: {
      verifiers: ['non_empty'],
      max_consecutive_failures: deep ? 8 : 3,
    },
  });
}

deepResearchToggle.addEventListener('change', async () => {
  if (!agent) return;
  const enabled = deepResearchToggle.checked;
  await rebuildAgent();
  setStatus(
    enabled
      ? 'Deep Research mode ON — agent will gather from multiple sources.'
      : 'Deep Research mode OFF — quick single-tool replies.',
  );
});

// --- Submit handler ------------------------------------------------------

inputForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  if (!agent) return;
  const prompt = promptEl.value.trim();
  if (!prompt) return;

  promptEl.value = '';
  promptEl.disabled = true;
  sendBtn.disabled = true;
  addBubble('user', prompt);
  const agentBubble = addBubble('agent', '');

  const startedAt = performance.now();
  try {
    for await (const ev of agent.runStream(prompt)) {
      if (ev.kind === 'delta') {
        agentBubble.textContent += ev.text;
        chat.scrollTop = chat.scrollHeight;
      } else if (ev.kind === 'event') {
        pushTrace(ev.event);
      } else if (ev.kind === 'end') {
        pushTrace(ev);
        if (!agentBubble.textContent && ev.result.response) {
          agentBubble.textContent = ev.result.response;
        }
        const elapsed = ((performance.now() - startedAt) / 1000).toFixed(1);
        setStatus(
          `Done in ${elapsed}s — ${escape(ev.result.reason)} (${ev.result.turns} turns).`,
        );
      }
    }
  } catch (err) {
    addBubble('system', `Error: ${(err as Error).message}`);
  } finally {
    promptEl.disabled = false;
    sendBtn.disabled = false;
    promptEl.focus();
  }
});

console.log(`${ROLE_LABEL.user} starts ai-agent browser demo.`);
