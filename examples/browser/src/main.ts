import './style.css';

// ─────────────────────────────────────────────────────────
// 型定義
// ─────────────────────────────────────────────────────────

interface AgentConfig {
  endpoint:     string;
  apiKey:       string;
  model:        string;
  binaryPath:   string;
  systemPrompt: string;
  maxTurns:     number;
}

interface AgentInfo {
  id:     string;
  name:   string;
  config: AgentConfig;
  tools:  string[];
  busy:   boolean;
}

interface ChatMessage {
  role:    'user' | 'assistant' | 'system' | 'error';
  content: string;
  meta?:   string;
}

// WebSocket メッセージ型
type ServerMsg =
  | { type: 'init';          agents: AgentInfo[] }
  | { type: 'agent.created'; id: string; name: string; config: AgentConfig; tools: string[] }
  | { type: 'agent.deleted'; id: string }
  | { type: 'run.start';     agentId: string; runId: string }
  | { type: 'stream.delta';  agentId: string; runId: string; text: string; turn: number }
  | { type: 'run.done';      agentId: string; runId: string; result: { response: string; reason: string; turns: number } }
  | { type: 'run.error';     agentId: string; runId: string; message: string }
  | { type: 'agent.aborted'; id: string }
  | { type: 'mcp.registered'; agentId: string; tools: string[]; allTools: string[] }
  | { type: 'error';         message: string };

// ─────────────────────────────────────────────────────────
// アプリ状態
// ─────────────────────────────────────────────────────────

const state = {
  agents:      new Map<string, AgentInfo>(),
  activeId:    null as string | null,
  histories:   new Map<string, ChatMessage[]>(),
  streaming:   new Map<string, { runId: string; el: HTMLElement }>(),
  ws:          null as WebSocket | null,
  reconnectTimer: null as ReturnType<typeof setTimeout> | null,
};

// ─────────────────────────────────────────────────────────
// DOM ヘルパー
// ─────────────────────────────────────────────────────────

function $<T extends Element>(sel: string): T {
  const el = document.querySelector<T>(sel);
  if (!el) throw new Error(`element not found: ${sel}`);
  return el;
}

function $$(sel: string): NodeListOf<Element> {
  return document.querySelectorAll(sel);
}

// ─────────────────────────────────────────────────────────
// WebSocket 接続
// ─────────────────────────────────────────────────────────

function connectWS() {
  const wsUrl = `ws://${location.host}/ws`;
  setConnStatus('connecting');

  const ws = new WebSocket(wsUrl);
  state.ws = ws;

  ws.onopen = () => {
    setConnStatus('connected');
    if (state.reconnectTimer) {
      clearTimeout(state.reconnectTimer);
      state.reconnectTimer = null;
    }
  };

  ws.onclose = () => {
    setConnStatus('disconnected');
    state.ws = null;
    state.reconnectTimer = setTimeout(connectWS, 3000);
  };

  ws.onerror = () => {
    ws.close();
  };

  ws.onmessage = (ev) => {
    let msg: ServerMsg;
    try {
      msg = JSON.parse(ev.data as string) as ServerMsg;
    } catch {
      return;
    }
    handleServerMessage(msg);
  };
}

function send(msg: object) {
  if (state.ws?.readyState === WebSocket.OPEN) {
    state.ws.send(JSON.stringify(msg));
  }
}

// ─────────────────────────────────────────────────────────
// サーバーメッセージ処理
// ─────────────────────────────────────────────────────────

function handleServerMessage(msg: ServerMsg) {
  switch (msg.type) {
    case 'init':
      for (const a of msg.agents) {
        state.agents.set(a.id, a);
        if (!state.histories.has(a.id)) state.histories.set(a.id, []);
      }
      renderAgentList();
      break;

    case 'agent.created':
      state.agents.set(msg.id, {
        id: msg.id, name: msg.name,
        config: msg.config, tools: msg.tools, busy: false,
      });
      state.histories.set(msg.id, []);
      renderAgentList();
      selectAgent(msg.id);
      break;

    case 'agent.deleted':
      state.agents.delete(msg.id);
      state.histories.delete(msg.id);
      state.streaming.delete(msg.id);
      if (state.activeId === msg.id) {
        state.activeId = null;
        showWelcome();
      }
      renderAgentList();
      break;

    case 'run.start': {
      const agent = state.agents.get(msg.agentId);
      if (agent) agent.busy = true;
      if (state.activeId === msg.agentId) updateBusyUI(true);
      renderAgentList();
      break;
    }

    case 'stream.delta': {
      if (state.activeId !== msg.agentId) break;
      const streaming = state.streaming.get(msg.agentId);
      if (streaming && streaming.runId === msg.runId) {
        streaming.el.textContent += msg.text;
        scrollChatToBottom();
      } else {
        // 新しいストリーミングメッセージ開始
        const el = appendChatBubble(msg.agentId, 'assistant', '');
        el.classList.add('streaming-cursor');
        state.streaming.set(msg.agentId, { runId: msg.runId, el });
      }
      break;
    }

    case 'run.done': {
      const agent = state.agents.get(msg.agentId);
      if (agent) agent.busy = false;

      // ストリーミング中だった場合、カーソルを外してメッセージ確定
      const streaming = state.streaming.get(msg.agentId);
      if (streaming) {
        streaming.el.classList.remove('streaming-cursor');
        // ストリーミングで出力済みの場合は content が空でないはず
        if (!streaming.el.textContent) {
          streaming.el.textContent = msg.result.response;
        }
        // meta 情報追加
        const meta = document.createElement('div');
        meta.className = 'meta';
        meta.textContent = `turns: ${msg.result.turns} · reason: ${msg.result.reason}`;
        streaming.el.parentElement!.appendChild(meta);
        state.streaming.delete(msg.agentId);
      } else if (state.activeId === msg.agentId) {
        // ストリーミングなしで完了
        const hist = state.histories.get(msg.agentId) ?? [];
        hist.push({ role: 'assistant', content: msg.result.response, meta: `turns: ${msg.result.turns}` });
        state.histories.set(msg.agentId, hist);
        if (state.activeId === msg.agentId) rerenderChatLog();
      }

      if (state.activeId === msg.agentId) updateBusyUI(false);
      renderAgentList();
      break;
    }

    case 'run.error': {
      const agent = state.agents.get(msg.agentId);
      if (agent) agent.busy = false;
      state.streaming.delete(msg.agentId);

      const hist = state.histories.get(msg.agentId) ?? [];
      hist.push({ role: 'error', content: `エラー: ${msg.message}` });
      state.histories.set(msg.agentId, hist);

      if (state.activeId === msg.agentId) {
        rerenderChatLog();
        updateBusyUI(false);
      }
      renderAgentList();
      break;
    }

    case 'mcp.registered': {
      const agent = state.agents.get(msg.agentId);
      if (agent) agent.tools = msg.allTools;
      if (state.activeId === msg.agentId) renderToolsList(msg.agentId);
      // システムメッセージ追加
      const hist = state.histories.get(msg.agentId) ?? [];
      hist.push({ role: 'system', content: `MCP 登録完了: ${msg.tools.join(', ')}` });
      state.histories.set(msg.agentId, hist);
      if (state.activeId === msg.agentId) rerenderChatLog();
      break;
    }

    case 'error':
      console.error('[ws error]', msg.message);
      if (state.activeId) {
        const hist = state.histories.get(state.activeId) ?? [];
        hist.push({ role: 'error', content: msg.message });
        state.histories.set(state.activeId, hist);
        rerenderChatLog();
      }
      break;
  }
}

// ─────────────────────────────────────────────────────────
// UI 更新
// ─────────────────────────────────────────────────────────

function setConnStatus(s: 'connected' | 'disconnected' | 'connecting') {
  const el = $<HTMLDivElement>('#conn-status');
  el.className = `conn-status ${s}`;
  el.querySelector('.conn-label')!.textContent =
    s === 'connected' ? '接続済み' :
    s === 'connecting' ? '接続中...' : '未接続';
}

function renderAgentList() {
  const ul = $<HTMLUListElement>('#agent-list');
  ul.innerHTML = '';

  if (state.agents.size === 0) {
    const li = document.createElement('li');
    li.className = 'agent-item';
    li.style.color = 'var(--text-muted)';
    li.style.fontSize = '12px';
    li.style.cursor = 'default';
    li.textContent = 'エージェントがありません';
    ul.appendChild(li);
    return;
  }

  for (const [id, agent] of state.agents) {
    const li = document.createElement('li');
    li.className = `agent-item${id === state.activeId ? ' active' : ''}${agent.busy ? ' busy' : ''}`;
    li.dataset.agentId = id;

    const dot  = document.createElement('span');
    dot.className = 'agent-dot';

    const name = document.createElement('span');
    name.className = 'agent-item-name';
    name.textContent = agent.name;

    li.append(dot, name);
    li.addEventListener('click', () => selectAgent(id));
    ul.appendChild(li);
  }
}

function selectAgent(id: string) {
  const agent = state.agents.get(id);
  if (!agent) return;

  state.activeId = id;
  renderAgentList();
  showAgentView(agent);
}

function showWelcome() {
  $<HTMLDivElement>('#welcome').classList.remove('hidden');
  $<HTMLDivElement>('#agent-view').classList.add('hidden');
}

function showAgentView(agent: AgentInfo) {
  $<HTMLDivElement>('#welcome').classList.add('hidden');
  const view = $<HTMLDivElement>('#agent-view');
  view.classList.remove('hidden');

  $<HTMLSpanElement>('#agent-name-display').textContent = agent.name;
  updateBusyUI(agent.busy);

  // 設定タブにデータを反映
  $<HTMLInputElement>('#cfg-endpoint').value     = agent.config.endpoint;
  $<HTMLInputElement>('#cfg-api-key').value      = agent.config.apiKey;
  $<HTMLInputElement>('#cfg-model').value        = agent.config.model;
  $<HTMLInputElement>('#cfg-binary').value       = agent.config.binaryPath;
  $<HTMLTextAreaElement>('#cfg-system-prompt').value = agent.config.systemPrompt;
  $<HTMLInputElement>('#cfg-max-turns').value    = String(agent.config.maxTurns);

  rerenderChatLog();
  renderToolsList(agent.id);

  // チャットタブを表示
  switchTab('chat');
}

function updateBusyUI(busy: boolean) {
  const badge    = $<HTMLSpanElement>('#agent-status-badge');
  const btnSend  = $<HTMLButtonElement>('#btn-send');
  const btnAbort = $<HTMLButtonElement>('#btn-abort');

  badge.textContent = busy ? '実行中' : '待機中';
  badge.className   = `status-badge ${busy ? 'busy' : 'idle'}`;
  btnSend.disabled  = busy;
  btnAbort.classList.toggle('hidden', !busy);
}

function switchTab(name: string) {
  $$('.tab').forEach((t) => t.classList.remove('active'));
  $$('.tab-content').forEach((c) => {
    c.classList.toggle('hidden', !(c as HTMLElement).id.endsWith(name));
    if ((c as HTMLElement).id === `tab-${name}`) c.classList.add('active');
  });
  document.querySelector<HTMLButtonElement>(`.tab[data-tab="${name}"]`)
    ?.classList.add('active');
}

// ─────────────────────────────────────────────────────────
// チャットログ
// ─────────────────────────────────────────────────────────

function rerenderChatLog() {
  const log  = $<HTMLDivElement>('#chat-log');
  const hist = state.histories.get(state.activeId ?? '') ?? [];

  log.innerHTML = '';
  for (const msg of hist) {
    const role = msg.role === 'assistant' ? 'assistant' : msg.role === 'user' ? 'user' : msg.role === 'error' ? 'error-msg' : 'system-msg';
    const div = document.createElement('div');
    div.className = `message ${role}`;
    div.textContent = msg.content;
    if (msg.meta) {
      const m = document.createElement('div');
      m.className = 'meta';
      m.textContent = msg.meta;
      div.appendChild(m);
    }
    log.appendChild(div);
  }
  scrollChatToBottom();
}

function appendChatBubble(agentId: string, role: 'user' | 'assistant', content: string): HTMLDivElement {
  const log = $<HTMLDivElement>('#chat-log');
  const div = document.createElement('div');
  div.className = `message ${role}`;
  div.textContent = content;
  log.appendChild(div);
  scrollChatToBottom();

  // 履歴にも記録（user のみ。assistant はストリーミング完了後に確定）
  if (role === 'user') {
    const hist = state.histories.get(agentId) ?? [];
    hist.push({ role: 'user', content });
    state.histories.set(agentId, hist);
  }

  return div;
}

function scrollChatToBottom() {
  const log = $<HTMLDivElement>('#chat-log');
  log.scrollTop = log.scrollHeight;
}

function renderToolsList(agentId: string) {
  const agent = state.agents.get(agentId);
  const ul    = $<HTMLUListElement>('#tools-list');
  ul.innerHTML = '';

  if (!agent || agent.tools.length === 0) {
    const li = document.createElement('li');
    li.className = 'tools-empty';
    li.textContent = 'まだツールが登録されていません';
    ul.appendChild(li);
    return;
  }

  for (const name of agent.tools) {
    const li = document.createElement('li');
    li.textContent = name;
    ul.appendChild(li);
  }
}

// ─────────────────────────────────────────────────────────
// チャット送信
// ─────────────────────────────────────────────────────────

function sendMessage() {
  const input = $<HTMLTextAreaElement>('#chat-input');
  const prompt = input.value.trim();
  if (!prompt || !state.activeId) return;

  const agent = state.agents.get(state.activeId);
  if (!agent || agent.busy) return;

  input.value = '';
  appendChatBubble(state.activeId, 'user', prompt);

  send({
    type:    'agent.run',
    agentId: state.activeId,
    prompt,
  });
}

// ─────────────────────────────────────────────────────────
// エージェント作成モーダル
// ─────────────────────────────────────────────────────────

function openCreateModal() {
  $<HTMLDivElement>('#modal-create').classList.remove('hidden');
  $<HTMLInputElement>('#new-name').focus();
}

function closeCreateModal() {
  $<HTMLDivElement>('#modal-create').classList.add('hidden');
}

function createAgent() {
  const name         = $<HTMLInputElement>('#new-name').value.trim() || 'My Agent';
  const endpoint     = $<HTMLInputElement>('#new-endpoint').value.trim();
  const apiKey       = $<HTMLInputElement>('#new-api-key').value.trim();
  const model        = $<HTMLInputElement>('#new-model').value.trim();
  const binaryPath   = $<HTMLInputElement>('#new-binary').value.trim();
  const systemPrompt = $<HTMLTextAreaElement>('#new-system-prompt').value.trim();
  const maxTurns     = parseInt($<HTMLInputElement>('#new-max-turns').value, 10);

  send({
    type: 'agent.create',
    name,
    endpoint:     endpoint     || 'http://localhost:8080/v1',
    apiKey:       apiKey       || 'sk-gemma4',
    model,
    binaryPath:   binaryPath   || 'agent',
    systemPrompt: systemPrompt || 'You are a helpful assistant.',
    maxTurns:     isNaN(maxTurns) ? 10 : maxTurns,
  });
  closeCreateModal();
}

// ─────────────────────────────────────────────────────────
// MCP 登録
// ─────────────────────────────────────────────────────────

function registerMCP() {
  if (!state.activeId) return;
  const transport = $<HTMLSelectElement>('#mcp-transport').value as 'stdio' | 'sse';

  let payload: Record<string, unknown> = {
    type:      'agent.mcp.register',
    agentId:   state.activeId,
    transport,
  };

  if (transport === 'stdio') {
    const cmd  = $<HTMLInputElement>('#mcp-command').value.trim();
    const args = $<HTMLInputElement>('#mcp-args').value.trim();
    if (!cmd) { alert('コマンドを入力してください'); return; }
    payload.command = cmd;
    if (args) payload.args = args.split(/\s+/).filter(Boolean);
  } else {
    const url = $<HTMLInputElement>('#mcp-url').value.trim();
    if (!url) { alert('URL を入力してください'); return; }
    payload.url = url;
  }

  send(payload);
}

// ─────────────────────────────────────────────────────────
// イベントバインド
// ─────────────────────────────────────────────────────────

function initEventListeners() {
  // 新規エージェントボタン
  $<HTMLButtonElement>('#btn-new-agent').addEventListener('click', openCreateModal);
  $<HTMLButtonElement>('#btn-welcome-new').addEventListener('click', openCreateModal);

  // モーダル
  $<HTMLButtonElement>('#modal-close').addEventListener('click',      closeCreateModal);
  $<HTMLButtonElement>('#modal-cancel').addEventListener('click',     closeCreateModal);
  $<HTMLButtonElement>('#modal-create-btn').addEventListener('click', createAgent);
  $<HTMLDivElement>('.modal-backdrop').addEventListener('click',      closeCreateModal);

  // Enterでも作成
  $<HTMLDivElement>('#modal-create').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) createAgent();
    if (e.key === 'Escape') closeCreateModal();
  });

  // タブ切り替え
  $$('.tab').forEach((tab) => {
    tab.addEventListener('click', () => {
      const t = (tab as HTMLElement).dataset['tab'];
      if (t) switchTab(t);
    });
  });

  // チャット送信
  $<HTMLButtonElement>('#btn-send').addEventListener('click', sendMessage);
  $<HTMLTextAreaElement>('#chat-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && e.ctrlKey) {
      e.preventDefault();
      sendMessage();
    }
  });

  // 中断
  $<HTMLButtonElement>('#btn-abort').addEventListener('click', () => {
    if (state.activeId) send({ type: 'agent.abort', agentId: state.activeId });
  });

  // エージェント削除
  $<HTMLButtonElement>('#btn-delete-agent').addEventListener('click', () => {
    if (!state.activeId) return;
    const agent = state.agents.get(state.activeId);
    if (!agent) return;
    if (!confirm(`「${agent.name}」を削除しますか？`)) return;
    send({ type: 'agent.delete', agentId: state.activeId });
  });

  // MCP transport 切り替え
  $<HTMLSelectElement>('#mcp-transport').addEventListener('change', (e) => {
    const t = (e.target as HTMLSelectElement).value;
    $<HTMLDivElement>('#mcp-stdio-fields').classList.toggle('hidden', t !== 'stdio');
    $<HTMLDivElement>('#mcp-sse-fields').classList.toggle('hidden',   t !== 'sse');
  });

  // MCP 登録
  $<HTMLButtonElement>('#btn-mcp-register').addEventListener('click', registerMCP);
}

// ─────────────────────────────────────────────────────────
// エントリポイント
// ─────────────────────────────────────────────────────────

function main() {
  initEventListeners();
  connectWS();
}

main();
