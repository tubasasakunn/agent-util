/**
 * agent.ts — ルーター + ツール実行 + スキルローダー + MCP アダプタ。
 * main.ts からインポートして使う。このファイルは基本的に変更不要。
 */

import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

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
  // SLLMがコードブロックでJSONを包む場合がある (```json ... ```)
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
// Agent
// ---------------------------------------------------------------------------

export class Agent {
  private readonly tools     = new Map<string, ToolDef>();
  private readonly skillNames = new Set<string>();
  private readonly history: Msg[] = [];
  private sysPrompt = 'You are a helpful assistant.';

  setSystemPrompt(prompt: string): this {
    this.sysPrompt = prompt;
    return this;
  }

  addTool(def: ToolDef): this {
    this.tools.set(def.name, def);
    return this;
  }

  /**
   * スキルをツールとして登録する。
   */
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

  get toolCount(): number { return this.tools.size; }

  clearHistory(): void { this.history.length = 0; }

  // --- router プロンプト ---------------------------------------------------

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

  // --- ループ防止: 同一ターン内で同じツール+引数が成功済みなら再呼び出しをブロック ---

  private toolAlreadyCalled(name: string, args: Record<string, unknown>): boolean {
    const sig = `${name}:${JSON.stringify(args, Object.keys(args).sort())}`;

    // パス1: 最後のユーザーメッセージ以降の成功ツール結果 ID を収集
    const successIDs = new Set<string>();
    for (let i = this.history.length - 1; i >= 0; i--) {
      const m = this.history[i];
      if (m.role === 'user') break;
      if (m.role === 'tool' && !m.content.startsWith('Error:')) {
        successIDs.add(m.tool_call_id);
      }
    }

    // パス2: 成功 ID に一致する呼び出しシグネチャを検索
    for (let i = this.history.length - 1; i >= 0; i--) {
      const m = this.history[i];
      if (m.role === 'user') break;
      if (m.role === 'assistant' && m.tool_calls) {
        for (const tc of m.tool_calls) {
          const tcSig = `${tc.function.name}:${tc.function.arguments}`;
          if (tcSig === sig && successIDs.has(tc.id)) return true;
        }
      }
    }
    return false;
  }

  // --- メインループ --------------------------------------------------------

  async run(input: string, maxTurns = 10): Promise<string> {
    this.history.push({ role: 'user', content: input });
    const sys: Msg = { role: 'system', content: this.sysPrompt };

    for (let turn = 0; turn < maxTurns; turn++) {
      // ツールなし → 直接応答
      if (this.tools.size === 0) {
        const reply = await llmCall([sys, ...this.history]);
        this.history.push({ role: 'assistant', content: reply });
        return reply;
      }

      // Router ステップ（JSON mode）
      const rSys: Msg = { role: 'system', content: this.buildRouterPrompt() };
      const raw = await llmCall([rSys, ...this.history], true);

      let rr: RouterResult;
      try { rr = JSON.parse(raw) as RouterResult; }
      catch { rr = { tool: 'none', arguments: {} }; }

      const def = this.tools.get(rr.tool);
      const skip =
        rr.tool === 'none' ||
        !def ||
        this.toolAlreadyCalled(rr.tool, rr.arguments ?? {});

      if (skip) {
        if (rr.reasoning) process.stderr.write(`[router] none — ${rr.reasoning.slice(0, 70)}\n`);
        const reply = await llmCall([sys, ...this.history]);
        this.history.push({ role: 'assistant', content: reply });
        return reply;
      }

      process.stderr.write(`[router] ${rr.tool}(${JSON.stringify(rr.arguments ?? {})})\n`);

      // ツール実行
      const callId = `c${Date.now().toString(36)}`;
      this.history.push({
        role: 'assistant',
        content: '',
        tool_calls: [{ id: callId, type: 'function', function: { name: rr.tool, arguments: JSON.stringify(rr.arguments ?? {}) } }],
      });

      let result: string;
      try {
        result = String(await def.handler(rr.arguments ?? {}));
      } catch (err) {
        result = `Error: ${err instanceof Error ? err.message : String(err)}`;
      }
      process.stderr.write(`[tool]   ${result.slice(0, 80).replace(/\n/g, ' ')}\n`);
      this.history.push({ role: 'tool', content: result, tool_call_id: callId });
    }

    return '(max turns reached)';
  }
}

// ---------------------------------------------------------------------------
// スキルローダー（SKILL.md ファイルベース）
// ---------------------------------------------------------------------------

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

/**
 * dir 以下の SKILL.md を走査して agent にスキルとして登録する。
 * @returns 登録したスキル数
 */
export function loadSkillsFromDir(agent: Agent, dir: string): number {
  if (!existsSync(dir)) return 0;
  let count = 0;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const skillMD = join(dir, entry.name, 'SKILL.md');
    if (!existsSync(skillMD)) continue;
    const meta = parseFrontmatter(readFileSync(skillMD, 'utf8'));
    if (!meta) continue;
    const skillDir = join(dir, entry.name);
    agent.addSkill(meta.name, meta.description, () => {
      try {
        const fresh = parseFrontmatter(readFileSync(skillMD, 'utf8'));
        return `${fresh?.body ?? meta.body}\n\nSkill directory: ${skillDir}`;
      } catch {
        return `${meta.body}\n\nSkill directory: ${skillDir}`;
      }
    });
    count++;
  }
  return count;
}

// ---------------------------------------------------------------------------
// MCP アダプタ（@modelcontextprotocol/sdk が必要）
// ---------------------------------------------------------------------------

export interface McpConfig {
  /** 起動コマンド (例: "npx") */
  command: string;
  /** コマンド引数 (例: ["-y", "@modelcontextprotocol/server-filesystem", "."]) */
  args?: string[];
  /** 追加環境変数 */
  env?: Record<string, string>;
}

/**
 * MCP サーバーを起動してツールを agent に登録する。
 * @returns サーバーを停止するクリーンアップ関数
 */
export async function addMcpServer(agent: Agent, cfg: McpConfig): Promise<() => Promise<void>> {
  const { Client } = await import('@modelcontextprotocol/sdk/client/index.js');
  const { StdioClientTransport } = await import('@modelcontextprotocol/sdk/client/stdio.js');

  const transport = new StdioClientTransport({
    command: cfg.command,
    args: cfg.args ?? [],
    env: cfg.env,
  });
  const client = new Client({ name: 'ai-agent', version: '0.1.0' });
  await client.connect(transport);

  const { tools } = await client.listTools();
  for (const t of tools) {
    agent.addTool({
      name: t.name,
      description: t.description ?? t.name,
      parameters: t.inputSchema as Record<string, unknown>,
      readOnly: false,
      handler: async (args) => {
        const res = await client.callTool({ name: t.name, arguments: args });
        return (res.content as Array<{ type: string; text?: string }>)
          .map((c) => (c.type === 'text' ? (c.text ?? '') : JSON.stringify(c)))
          .join('\n');
      },
    });
  }
  process.stderr.write(`[mcp] ${tools.length} tool(s) from: ${cfg.command} ${cfg.args?.join(' ') ?? ''}\n`);
  return () => client.close();
}
