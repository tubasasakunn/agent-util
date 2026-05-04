/**
 * ai-agent Studio — バックエンドサーバー
 *
 * Express + WebSocket でフロントエンドからのリクエストを受け付け、
 * JS SDK を使ってエージェントプロセスを管理する。
 *
 * 起動: tsx server.ts
 * デフォルト: http://localhost:4000 / ws://localhost:4000/ws
 */

import express from 'express';
import { createServer } from 'http';
import { WebSocketServer, type WebSocket } from 'ws';
import { Agent } from '@ai-agent/sdk';
import { randomUUID } from 'crypto';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PORT = 4000;

// ─────────────────────────────────────────────
// 型定義
// ─────────────────────────────────────────────

interface AgentConfig {
  endpoint: string;
  apiKey: string;
  model: string;
  binaryPath: string;
  systemPrompt: string;
  maxTurns: number;
}

interface StoredAgent {
  agent: Agent;
  name: string;
  config: AgentConfig;
  tools: string[];       // 登録済みツール名（MCP経由）
  runningRunId: string | null;
}

// ─────────────────────────────────────────────
// サーバー初期化
// ─────────────────────────────────────────────

const app = express();
app.use(express.json());

// 本番ビルドの静的配信
const distDir = path.join(__dirname, 'dist');
app.use(express.static(distDir));

const httpServer = createServer(app);
const wss = new WebSocketServer({ server: httpServer, path: '/ws' });

const agentMap = new Map<string, StoredAgent>();

// ─────────────────────────────────────────────
// WebSocket 通信
// ─────────────────────────────────────────────

function send(ws: WebSocket, msg: object): void {
  if (ws.readyState === ws.OPEN) {
    ws.send(JSON.stringify(msg));
  }
}

wss.on('connection', (ws) => {
  console.log('[ws] client connected');

  // 接続時に現在のエージェント一覧を送信
  const agents = Array.from(agentMap.entries()).map(([id, a]) => ({
    id,
    name: a.name,
    config: a.config,
    tools: a.tools,
    busy: a.runningRunId !== null,
  }));
  send(ws, { type: 'init', agents });

  ws.on('message', async (data) => {
    let msg: Record<string, unknown>;
    try {
      msg = JSON.parse(data.toString()) as Record<string, unknown>;
    } catch {
      send(ws, { type: 'error', message: 'invalid JSON' });
      return;
    }
    try {
      await handleMessage(ws, msg);
    } catch (err) {
      send(ws, {
        type: 'error',
        message: err instanceof Error ? err.message : String(err),
      });
    }
  });

  ws.on('close', () => {
    console.log('[ws] client disconnected');
  });
});

// ─────────────────────────────────────────────
// メッセージハンドラ
// ─────────────────────────────────────────────

async function handleMessage(
  ws: WebSocket,
  msg: Record<string, unknown>,
): Promise<void> {
  switch (msg.type) {
    // ─── エージェント作成 ───────────────────
    case 'agent.create': {
      const config: AgentConfig = {
        endpoint: String(msg.endpoint ?? 'http://localhost:8080/v1'),
        apiKey:   String(msg.apiKey   ?? 'sk-gemma4'),
        model:    String(msg.model    ?? ''),
        binaryPath: String(msg.binaryPath ?? 'agent'),
        systemPrompt: String(msg.systemPrompt ?? 'You are a helpful assistant.'),
        maxTurns: Number(msg.maxTurns ?? 10),
      };

      const agent = new Agent({
        binaryPath: config.binaryPath,
        env: {
          SLLM_ENDPOINT: config.endpoint,
          SLLM_API_KEY:  config.apiKey,
          ...(config.model ? { SLLM_MODEL: config.model } : {}),
        },
        stderr: 'pipe',
      });

      await agent.start();
      await agent.configure({
        system_prompt: config.systemPrompt,
        max_turns: config.maxTurns,
        streaming: { enabled: true },
      });

      const id = randomUUID().slice(0, 8);
      const name = String(msg.name ?? `Agent-${id}`);

      agentMap.set(id, {
        agent,
        name,
        config,
        tools: [],
        runningRunId: null,
      });

      console.log(`[agent] created: ${id} (${name})`);
      send(ws, {
        type: 'agent.created',
        id,
        name,
        config,
        tools: [],
      });
      break;
    }

    // ─── エージェント削除 ───────────────────
    case 'agent.delete': {
      const id = String(msg.agentId ?? '');
      const stored = agentMap.get(id);
      if (!stored) {
        send(ws, { type: 'error', message: `agent not found: ${id}` });
        return;
      }
      await stored.agent.close();
      agentMap.delete(id);
      console.log(`[agent] deleted: ${id}`);
      send(ws, { type: 'agent.deleted', id });
      break;
    }

    // ─── エージェント実行 ───────────────────
    case 'agent.run': {
      const agentId = String(msg.agentId ?? '');
      const stored = agentMap.get(agentId);
      if (!stored) {
        send(ws, { type: 'error', message: `agent not found: ${agentId}` });
        return;
      }
      if (stored.runningRunId !== null) {
        send(ws, { type: 'error', message: `agent ${agentId} is already running` });
        return;
      }

      const runId = randomUUID().slice(0, 8);
      stored.runningRunId = runId;
      send(ws, { type: 'run.start', agentId, runId });

      try {
        const result = await stored.agent.run(String(msg.prompt ?? ''), {
          onDelta: (text, turn) => {
            send(ws, { type: 'stream.delta', agentId, runId, text, turn });
          },
        });
        send(ws, { type: 'run.done', agentId, runId, result });
        console.log(`[agent] run done: ${agentId} (turns=${result.turns})`);
      } catch (err) {
        send(ws, {
          type: 'run.error',
          agentId,
          runId,
          message: err instanceof Error ? err.message : String(err),
        });
      } finally {
        stored.runningRunId = null;
      }
      break;
    }

    // ─── 実行中断 ───────────────────────────
    case 'agent.abort': {
      const id = String(msg.agentId ?? '');
      const stored = agentMap.get(id);
      if (!stored) return;
      await stored.agent.abort();
      send(ws, { type: 'agent.aborted', id });
      break;
    }

    // ─── MCP 登録 ───────────────────────────
    case 'agent.mcp.register': {
      const agentId = String(msg.agentId ?? '');
      const stored = agentMap.get(agentId);
      if (!stored) {
        send(ws, { type: 'error', message: `agent not found: ${agentId}` });
        return;
      }
      const tools = await stored.agent.registerMCP({
        command:   msg.command   ? String(msg.command)   : undefined,
        args:      Array.isArray(msg.args) ? msg.args as string[] : undefined,
        env:       msg.env       ? msg.env as Record<string, string> : undefined,
        transport: (msg.transport as 'stdio' | 'sse') ?? 'stdio',
        url:       msg.url       ? String(msg.url)       : undefined,
      });
      stored.tools.push(...tools);
      console.log(`[mcp] registered for ${agentId}: ${tools.join(', ')}`);
      send(ws, { type: 'mcp.registered', agentId, tools, allTools: stored.tools });
      break;
    }

    default:
      send(ws, { type: 'error', message: `unknown message type: ${String(msg.type)}` });
  }
}

// ─────────────────────────────────────────────
// 起動
// ─────────────────────────────────────────────

httpServer.listen(PORT, () => {
  console.log(`\n🤖 ai-agent Studio`);
  console.log(`   Server:    http://localhost:${PORT}`);
  console.log(`   WebSocket: ws://localhost:${PORT}/ws`);
  console.log(`   UI (dev):  http://localhost:5173\n`);
});

// プロセス終了時にエージェントをクリーンアップ
async function cleanup() {
  console.log('\n[shutdown] closing agents...');
  await Promise.all(
    Array.from(agentMap.values()).map((a) => a.agent.close().catch(() => undefined)),
  );
  process.exit(0);
}
process.on('SIGINT', cleanup);
process.on('SIGTERM', cleanup);
