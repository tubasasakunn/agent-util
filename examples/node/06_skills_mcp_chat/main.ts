/**
 * AI チャット REPL — スキル・ツール・MCP を手軽に追加できる出発点。
 *
 * ① スキル  : .agents/skills/<name>/SKILL.md を追加するだけ（自動ロード）
 * ② ツール  : agent.addTool({ name, description, parameters, handler }) で追加
 * ③ MCP     : addMcpServer(agent, { command, args }) で MCP サーバーを接続
 *
 * 実行例:
 *   npm start
 *   SLLM_ENDPOINT=http://localhost:8080/v1/chat/completions npm start
 *   MCP_COMMAND="npx -y @modelcontextprotocol/server-filesystem ." npm start
 */

import { createInterface } from 'node:readline';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { Agent, addMcpServer, loadSkillsFromDir } from './agent.ts';

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT  = resolve(HERE, '../../..');

async function main(): Promise<void> {
  const agent = new Agent();

  // ① スキル: .agents/skills/ と .claude/skills/ を自動スキャン ──────────
  let skillCount = 0;
  for (const dir of [
    resolve(ROOT, '.agents/skills'),
    resolve(ROOT, '.claude/skills'),
  ]) {
    skillCount += loadSkillsFromDir(agent, dir);
  }
  if (skillCount > 0) process.stderr.write(`[skills] ${skillCount} loaded\n`);

  // ② カスタムツール（好きなものを追加） ───────────────────────────────────
  agent.addTool({
    name: 'get_current_time',
    description: 'Return the current date and time in ISO 8601 format.',
    parameters: { type: 'object', properties: {} },
    readOnly: true,
    handler: () => new Date().toISOString(),
  });

  // ③ MCP（環境変数 MCP_COMMAND でサーバーを起動） ─────────────────────────
  //   例: MCP_COMMAND="npx -y @modelcontextprotocol/server-filesystem ." npm start
  const closers: Array<() => Promise<void>> = [];
  if (process.env.MCP_COMMAND) {
    const [cmd, ...args] = process.env.MCP_COMMAND.trim().split(/\s+/);
    try {
      closers.push(await addMcpServer(agent, { command: cmd, args }));
    } catch (err) {
      process.stderr.write(`[mcp] failed to connect: ${err}\n`);
    }
  }

  // ─────────────────────────────────────────────────────────────────────────
  const endpoint = process.env.SLLM_ENDPOINT ?? 'http://localhost:8080/v1/chat/completions';
  process.stderr.write(`\nai-agent chat  skills=${skillCount}  tools=${agent.toolCount}  endpoint=${endpoint}\n`);
  process.stderr.write('Commands: /clear (履歴クリア) | exit\n\n');

  // REPL
  const rl = createInterface({ input: process.stdin, output: process.stderr });
  process.stderr.write('> ');

  try {
    for await (const line of rl) {
      const input = line.trim();
      if (!input) { process.stderr.write('> '); continue; }

      if (input === 'exit' || input === 'quit') break;

      if (input === '/clear') {
        agent.clearHistory();
        process.stderr.write('history cleared\n> ');
        continue;
      }

      process.stderr.write('…\r');
      try {
        const response = await agent.run(input);
        process.stderr.write('  \r');
        console.log(response);
        console.log();
      } catch (err) {
        process.stderr.write(`error: ${err}\n`);
      }
      process.stderr.write('> ');
    }
  } finally {
    rl.close();
    await Promise.all(closers.map((fn) => fn()));
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
