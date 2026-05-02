/**
 * AI チャット API サーバー
 *
 * エンドポイント:
 *   POST /api/chat              — AI と会話（セッション維持）
 *   GET  /api/skills            — スキル一覧
 *   POST /api/skills            — スキル追加/更新
 *   PUT  /api/skills/:name      — スキル更新
 *   DELETE /api/skills/:name    — スキル削除
 *   GET  /api/mcp               — MCP サーバー一覧
 *   POST /api/mcp               — MCP サーバー接続
 *   DELETE /api/mcp/:name       — MCP サーバー切断
 *   GET  /api/tools             — 登録済みツール一覧
 *   DELETE /api/sessions/:id    — セッション削除
 *
 * 実行: npm start
 * 環境変数: SLLM_ENDPOINT, SLLM_API_KEY, PORT (default 3000)
 */

import { serve } from '@hono/node-server';
import { Hono } from 'hono';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { McpRegistry, SessionManager, SkillStore } from './agent.ts';

const HERE  = dirname(fileURLToPath(import.meta.url));
const ROOT  = resolve(HERE, '../../..');
const PORT  = Number(process.env.PORT ?? '3000');

const skillsDir  = resolve(ROOT, '.agents/skills');
const store      = new SkillStore(skillsDir);
const mcpReg     = new McpRegistry();
const sessions   = new SessionManager(store, mcpReg);

const app = new Hono();

// ── /api/chat ─────────────────────────────────────────────────────────────
app.post('/api/chat', async (c) => {
  const body = await c.req.json<{ message: string; session_id?: string }>();
  const message = body?.message?.trim();
  if (!message) return c.json({ error: 'message is required' }, 400);

  const sessionId = body.session_id ?? `s_${Date.now().toString(36)}`;
  const agent = sessions.getOrCreate(sessionId);

  try {
    const result = await agent.run(message);
    return c.json({ response: result.response, turns: result.turns, session_id: sessionId });
  } catch (err) {
    return c.json({ error: String(err) }, 500);
  }
});

// ── /api/skills ───────────────────────────────────────────────────────────
app.get('/api/skills', (c) => {
  return c.json({ skills: store.list() });
});

app.post('/api/skills', async (c) => {
  const body = await c.req.json<{ name?: string; description?: string; instructions?: string }>();
  if (!body?.name || !body?.description || !body?.instructions) {
    return c.json({ error: 'name, description, instructions are required' }, 400);
  }
  if (!/^[a-z0-9-]+$/.test(body.name)) {
    return c.json({ error: 'name must match [a-z0-9-]+' }, 400);
  }
  const exists = !!store.get(body.name);
  store.upsert({ name: body.name, description: body.description, instructions: body.instructions });
  sessions.rebuildAll();
  return c.json({ skill: store.get(body.name) }, exists ? 200 : 201);
});

app.put('/api/skills/:name', async (c) => {
  const name = c.req.param('name');
  if (!store.get(name)) return c.json({ error: 'not found' }, 404);
  const body = await c.req.json<{ description?: string; instructions?: string }>();
  const current = store.get(name)!;
  store.upsert({
    name,
    description: body?.description ?? current.description,
    instructions: body?.instructions ?? current.instructions,
  });
  sessions.rebuildAll();
  return c.json({ skill: store.get(name) });
});

app.delete('/api/skills/:name', (c) => {
  const name = c.req.param('name');
  if (!store.delete(name)) return c.json({ error: 'not found' }, 404);
  sessions.rebuildAll();
  return c.json({ deleted: name });
});

// ── /api/mcp ──────────────────────────────────────────────────────────────
app.get('/api/mcp', (c) => {
  return c.json({ servers: mcpReg.list() });
});

app.post('/api/mcp', async (c) => {
  const body = await c.req.json<{ name?: string; command?: string; args?: string[] }>();
  if (!body?.name || !body?.command) {
    return c.json({ error: 'name and command are required' }, 400);
  }
  try {
    const entry = await mcpReg.connect(body.name, body.command, body.args ?? []);
    sessions.rebuildAll();
    return c.json({ server: entry }, 201);
  } catch (err) {
    return c.json({ error: String(err) }, 500);
  }
});

app.delete('/api/mcp/:name', async (c) => {
  const name = c.req.param('name');
  if (!(await mcpReg.disconnect(name))) return c.json({ error: 'not found' }, 404);
  sessions.rebuildAll();
  return c.json({ disconnected: name });
});

// ── /api/tools ────────────────────────────────────────────────────────────
app.get('/api/tools', (c) => {
  // 新規エージェントのツール一覧を返す（セッション非依存）
  const tmp = sessions.getOrCreate('__preview__');
  const names = tmp.toolNames();
  return c.json({ tools: names });
});

// ── /api/sessions ─────────────────────────────────────────────────────────
app.delete('/api/sessions/:id', (c) => {
  const id = c.req.param('id');
  if (!sessions.delete(id)) return c.json({ error: 'not found' }, 404);
  return c.json({ deleted: id });
});

// ── 起動 ─────────────────────────────────────────────────────────────────
const endpoint = process.env.SLLM_ENDPOINT ?? 'http://localhost:8080/v1/chat/completions';
console.log(`AI API server — port ${PORT}`);
console.log(`LLM endpoint: ${endpoint}`);
console.log(`Skills dir:   ${skillsDir}`);
console.log(`Skills loaded: ${store.list().length}`);
console.log();

serve({ fetch: app.fetch, port: PORT }, () => {
  console.log(`Listening on http://localhost:${PORT}`);
});
