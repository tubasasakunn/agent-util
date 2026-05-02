/**
 * agent.ts — 06_skills_mcp_chat/agent.ts を流用し、
 * 動的スキル CRUD とセッション管理を追加したもの。
 */

import { existsSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';

// ---------------------------------------------------------------------------
// 型定義
// ---------------------------------------------------------------------------

export type ToolHandler = (args: Record<string, unknown>) => string | Promise<string>;

export interface ToolDef {
  name: string;
  description: string;
  parameters?: Record<string, unknown>;
  readOnly?: boolean;
  handler: ToolHandler;
}

export interface SkillInfo {
  name: string;
  description: string;
  source: 'file' | 'inline';
  path?: string;
}

type Msg =
  | { role: 'system'; content: string }
  | { role: 'user'; content: string }
  | { role: 'assistant'; content: string; tool_calls?: ToolCall[] }
  | { role: 'tool'; content: string; tool_call_id: string };

interface ToolCall {
  id: string;
  type: 'function';
  function: { name: string; arguments: string };
}

interface RouterResult {
  tool: string;
  arguments: Record<string, unknown>;
  reasoning?: string;
}

// ---------------------------------------------------------------------------
// LLM 呼び出し
// ---------------------------------------------------------------------------

const ENDPOINT = process.env.SLLM_ENDPOINT ?? 'http://localhost:8080/v1/chat/completions';
const API_KEY  = process.env.SLLM_API_KEY  ?? 'sk-gemma4';
const MODEL    = process.env.SLLM_MODEL    ?? 'gemma-4-E2B-it-Q4_K_M';

function stripThinking(text: string): string {
  return text
    .replace(/<\|?channel\|?>\s*thought[\s\S]*?<\/?channel\|?>/gi, '')
    .replace(/<\|?thinking\|?>[\s\S]*?<\/?thinking\|?>/gi, '')
    .replace(/<\|?tool_call\|?>[\s\S]*?<\/?tool_call\|?>/gi, '')
    .replace(/<\|[^>]*\|>/g, '')
    .trim();
}

function extractJson(text: string): string {
  const m = text.match(/```(?:json)?\s*([\s\S]*?)```/);
  return m ? m[1].trim() : text;
}

async function llmCall(msgs: Msg[], jsonMode = false): Promise<string> {
  const r = await fetch(ENDPOINT, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(API_KEY ? { Authorization: `Bearer ${API_KEY}` } : {}),
    },
    body: JSON.stringify({
      model: MODEL,
      messages: msgs,
      temperature: jsonMode ? 0.1 : 0.7,
      ...(jsonMode ? { response_format: { type: 'json_object' } } : {}),
    }),
  });
  if (!r.ok) throw new Error(`HTTP ${r.status}: ${await r.text()}`);
  const data = (await r.json()) as { choices: Array<{ message: { content: string } }> };
  const content = stripThinking(data.choices[0]?.message?.content ?? '');
  return jsonMode ? extractJson(content) : content;
}

// ---------------------------------------------------------------------------
// Agent (stateful, セッションごとにインスタンス化)
// ---------------------------------------------------------------------------

export class Agent {
  private readonly tools     = new Map<string, ToolDef>();
  private readonly skillNames = new Set<string>();
  private readonly history: Msg[] = [];
  private sysPrompt = 'You are a helpful assistant.';

  setSystemPrompt(prompt: string): this { this.sysPrompt = prompt; return this; }

  addTool(def: ToolDef): this { this.tools.set(def.name, def); return this; }

  removeTool(name: string): boolean {
    this.skillNames.delete(name);
    return this.tools.delete(name);
  }

  addSkill(name: string, description: string, activate: () => string | Promise<string>): this {
    this.skillNames.add(name);
    return this.addTool({
      name,
      description,
      parameters: { type: 'object', properties: {} },
      readOnly: true,
      handler: async () => {
        const content = await activate();
        return (
          `<skill_content name="${name}">\n${content}\n\n` +
          `Instructions loaded. Follow these to respond. Select tool="none" next.\n</skill_content>`
        );
      },
    });
  }

  toolNames(): string[] { return Array.from(this.tools.keys()); }
  get toolCount(): number { return this.tools.size; }
  clearHistory(): void { this.history.length = 0; }

  private buildRouterPrompt(): string {
    let s = this.sysPrompt + '\n\n## Available Tools\n\n';
    for (const t of this.tools.values()) {
      s += `### ${t.name}\n${t.description}\n`;
      if (t.parameters) s += `Parameters: \`${JSON.stringify(t.parameters)}\`\n`;
      s += '\n';
    }
    s += 'Respond with ONLY valid JSON (no extra text):\n';
    s += '{"tool": "<name or \\"none\\">", "arguments": {}, "reasoning": "<why>"}\n';
    s += 'Use "none" to respond directly to the user without calling any tool.';
    return s;
  }

  private toolAlreadyCalled(name: string, args: Record<string, unknown>): boolean {
    const sig = `${name}:${JSON.stringify(args, Object.keys(args).sort())}`;
    const successIDs = new Set<string>();
    for (let i = this.history.length - 1; i >= 0; i--) {
      const m = this.history[i];
      if (m.role === 'user') break;
      if (m.role === 'tool' && !m.content.startsWith('Error:')) successIDs.add(m.tool_call_id);
    }
    for (let i = this.history.length - 1; i >= 0; i--) {
      const m = this.history[i];
      if (m.role === 'user') break;
      if (m.role === 'assistant' && m.tool_calls) {
        for (const tc of m.tool_calls) {
          if (`${tc.function.name}:${tc.function.arguments}` === sig && successIDs.has(tc.id)) return true;
        }
      }
    }
    return false;
  }

  async run(input: string, maxTurns = 10): Promise<{ response: string; turns: number }> {
    this.history.push({ role: 'user', content: input });
    const sys: Msg = { role: 'system', content: this.sysPrompt };

    for (let turn = 0; turn < maxTurns; turn++) {
      if (this.tools.size === 0) {
        const reply = await llmCall([sys, ...this.history]);
        this.history.push({ role: 'assistant', content: reply });
        return { response: reply, turns: turn + 1 };
      }

      const rSys: Msg = { role: 'system', content: this.buildRouterPrompt() };
      const raw = await llmCall([rSys, ...this.history], true);
      let rr: RouterResult;
      try { rr = JSON.parse(raw) as RouterResult; }
      catch { rr = { tool: 'none', arguments: {} }; }

      const def = this.tools.get(rr.tool);
      if (rr.tool === 'none' || !def || this.toolAlreadyCalled(rr.tool, rr.arguments ?? {})) {
        const reply = await llmCall([sys, ...this.history]);
        this.history.push({ role: 'assistant', content: reply });
        return { response: reply, turns: turn + 1 };
      }

      const callId = `c${Date.now().toString(36)}`;
      this.history.push({
        role: 'assistant',
        content: '',
        tool_calls: [{ id: callId, type: 'function', function: { name: rr.tool, arguments: JSON.stringify(rr.arguments ?? {}) } }],
      });

      let result: string;
      try { result = String(await def.handler(rr.arguments ?? {})); }
      catch (err) { result = `Error: ${err instanceof Error ? err.message : String(err)}`; }
      this.history.push({ role: 'tool', content: result, tool_call_id: callId });
    }
    return { response: '(max turns reached)', turns: maxTurns };
  }
}

// ---------------------------------------------------------------------------
// SkillStore — インメモリ + ファイルシステムに永続化
// ---------------------------------------------------------------------------

export interface SkillDef {
  name: string;
  description: string;
  instructions: string;
}

function parseFrontmatter(src: string): { name: string; description: string; body: string } | null {
  const lines = src.split('\n');
  if (lines[0]?.trim() !== '---') return null;
  const closeIdx = lines.findIndex((l, i) => i > 0 && l.trim() === '---');
  if (closeIdx === -1) return null;
  let name = '', description = '';
  for (const line of lines.slice(1, closeIdx)) {
    const ci = line.indexOf(':');
    if (ci === -1) continue;
    const k = line.slice(0, ci).trim();
    const v = line.slice(ci + 1).trim().replace(/^["']|["']$/g, '');
    if (k === 'name') name = v;
    if (k === 'description') description = v;
  }
  if (!name || !description) return null;
  return { name, description, body: lines.slice(closeIdx + 1).join('\n').trim() };
}

export class SkillStore {
  private skills = new Map<string, SkillDef>();

  constructor(private readonly dir: string) {
    mkdirSync(dir, { recursive: true });
    this.loadFromDir();
  }

  private loadFromDir(): void {
    if (!existsSync(this.dir)) return;
    for (const entry of readdirSync(this.dir, { withFileTypes: true })) {
      if (!entry.isDirectory()) continue;
      const path = join(this.dir, entry.name, 'SKILL.md');
      if (!existsSync(path)) continue;
      const meta = parseFrontmatter(readFileSync(path, 'utf8'));
      if (!meta) continue;
      this.skills.set(meta.name, { name: meta.name, description: meta.description, instructions: meta.body });
    }
  }

  list(): SkillDef[] { return Array.from(this.skills.values()); }

  get(name: string): SkillDef | undefined { return this.skills.get(name); }

  upsert(def: SkillDef): void {
    this.skills.set(def.name, def);
    const skillDir = join(this.dir, def.name);
    mkdirSync(skillDir, { recursive: true });
    writeFileSync(
      join(skillDir, 'SKILL.md'),
      `---\nname: ${def.name}\ndescription: ${def.description}\n---\n\n${def.instructions}\n`,
      'utf8',
    );
  }

  delete(name: string): boolean {
    if (!this.skills.has(name)) return false;
    this.skills.delete(name);
    const skillDir = join(this.dir, name);
    if (existsSync(skillDir)) rmSync(skillDir, { recursive: true, force: true });
    return true;
  }
}

// ---------------------------------------------------------------------------
// McpRegistry — MCP サーバーの動的接続管理
// ---------------------------------------------------------------------------

export interface McpServerEntry {
  name: string;
  command: string;
  args: string[];
  toolNames: string[];
}

export class McpRegistry {
  private entries = new Map<string, { entry: McpServerEntry; close: () => Promise<void> }>();

  async connect(name: string, command: string, args: string[]): Promise<McpServerEntry> {
    if (this.entries.has(name)) await this.disconnect(name);

    const { Client } = await import('@modelcontextprotocol/sdk/client/index.js');
    const { StdioClientTransport } = await import('@modelcontextprotocol/sdk/client/stdio.js');

    const transport = new StdioClientTransport({ command, args });
    const client = new Client({ name: 'ai-agent', version: '0.1.0' });
    await client.connect(transport);

    const { tools } = await client.listTools();
    const entry: McpServerEntry = { name, command, args, toolNames: tools.map((t) => t.name) };
    this.entries.set(name, { entry, close: () => client.close() });
    return entry;
  }

  async disconnect(name: string): Promise<boolean> {
    const e = this.entries.get(name);
    if (!e) return false;
    await e.close();
    this.entries.delete(name);
    return true;
  }

  list(): McpServerEntry[] { return Array.from(this.entries.values()).map((e) => e.entry); }

  async closeAll(): Promise<void> {
    await Promise.all(Array.from(this.entries.values()).map((e) => e.close()));
    this.entries.clear();
  }
}

// ---------------------------------------------------------------------------
// SessionManager — セッションごとに Agent インスタンスを管理
// ---------------------------------------------------------------------------

export class SessionManager {
  private sessions = new Map<string, { agent: Agent; lastUsed: number }>();
  private readonly ttlMs = 30 * 60 * 1000; // 30分でGC

  constructor(
    private readonly store: SkillStore,
    private readonly mcpRegistry: McpRegistry,
  ) {}

  getOrCreate(sessionId: string): Agent {
    const existing = this.sessions.get(sessionId);
    if (existing) {
      existing.lastUsed = Date.now();
      return existing.agent;
    }
    const agent = this.buildAgent();
    this.sessions.set(sessionId, { agent, lastUsed: Date.now() });
    this.gc();
    return agent;
  }

  /** スキル/MCP変更後に全セッションのエージェントを再構築する */
  rebuildAll(): void {
    for (const [id, sess] of this.sessions) {
      const newAgent = this.buildAgent();
      newAgent.clearHistory();
      this.sessions.set(id, { agent: newAgent, lastUsed: sess.lastUsed });
    }
  }

  delete(sessionId: string): boolean {
    return this.sessions.delete(sessionId);
  }

  private buildAgent(): Agent {
    const agent = new Agent();
    agent.addTool({
      name: 'get_current_time',
      description: 'Return the current date and time in ISO 8601 format.',
      parameters: { type: 'object', properties: {} },
      readOnly: true,
      handler: () => new Date().toISOString(),
    });
    for (const skill of this.store.list()) {
      const s = skill;
      agent.addSkill(s.name, s.description, () => s.instructions);
    }
    return agent;
  }

  private gc(): void {
    const now = Date.now();
    for (const [id, sess] of this.sessions) {
      if (now - sess.lastUsed > this.ttlMs) this.sessions.delete(id);
    }
  }
}
