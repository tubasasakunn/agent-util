/**
 * Browser demo for @ai-agent/browser.
 *
 * Loads a WebLLM model in the page, registers two preset tools (echo,
 * calculator), wires the agent to a chat UI and a side panel that shows
 * router decisions, guard verdicts, tool calls, and verifier outcomes
 * in real time.
 */

import { Agent, tool, type LoopEvent } from '@ai-agent/browser';
import { WebLLMCompleter } from '@ai-agent/browser/llm';

const $ = <T extends HTMLElement>(sel: string): T => {
  const el = document.querySelector<T>(sel);
  if (!el) throw new Error(`missing element: ${sel}`);
  return el;
};

// --- DOM handles ----------------------------------------------------------

const modelSelect = $<HTMLSelectElement>('#model-select');
const loadBtn = $<HTMLButtonElement>('#load-btn');
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
  description: 'Fetch a URL via window.fetch and return up to 4KB of text.',
  parameters: {
    type: 'object',
    properties: { url: { type: 'string' } },
    required: ['url'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: async ({ url }) => {
    try {
      const r = await fetch(String(url));
      const txt = await r.text();
      return txt.slice(0, 4096);
    } catch (err) {
      return { content: `fetch failed: ${(err as Error).message}`, is_error: true };
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

renderToolList(['echo', 'calculator', 'fetch_url']);
renderGuardList();

// --- Load button ---------------------------------------------------------

loadBtn.addEventListener('click', async () => {
  const model = modelSelect.value;
  loadBtn.disabled = true;
  modelSelect.disabled = true;
  progress.hidden = false;
  setStatus(`Loading <code>${escape(model)}</code>...`);

  llm = new WebLLMCompleter({ model, temperature: 0.4 });

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

  agent = new Agent({ llm });
  agent.registerTools(echoTool, calcTool, fetchTool);
  await agent.configure({
    max_turns: 6,
    streaming: { enabled: true },
    guards: {
      input: ['prompt_injection'],
      tool_call: ['dangerous_shell'],
      output: ['secret_leak'],
    },
    verify: { verifiers: ['non_empty'] },
  });

  promptEl.disabled = false;
  sendBtn.disabled = false;
  promptEl.focus();
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
