'use strict';

let state = null;
let dirty = false;
const $ = id => document.getElementById(id);
const escapeHtml = value => String(value ?? '').replace(/[&<>'"]/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[ch]));

async function api(path, options = {}) {
  const response = await fetch(path, {
    method: options.method || 'POST',
    headers: {'Content-Type': 'application/json'},
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
  return data;
}
function toast(message) {
  const node = $('toast');
  node.textContent = message;
  node.classList.add('show');
  clearTimeout(node._timer);
  node._timer = setTimeout(() => node.classList.remove('show'), 1600);
}
function setDirty(value) {
  dirty = value;
  const node = $('miniDraftStatus');
  node.textContent = dirty ? '有未应用修改' : '已同步';
  node.className = `draft-status ${dirty ? 'dirty' : 'synced'}`;
  $('miniApplyTextBtn').disabled = !dirty;
  $('miniDiscardTextBtn').disabled = !dirty;
}
function fillText(force = false) {
  const o = state?.config?.overlay || {};
  const values = {miniEmptyText:o.emptyText || '', miniQueueEmptyText:o.queueEmptyText || '', miniInfoText:o.infoText || ''};
  for (const [id, value] of Object.entries(values)) {
    const node = $(id);
    if (force || (!dirty && document.activeElement !== node)) node.value = value;
  }
}
function render(next) {
  state = next;
  const status = state.connectionStatus || 'disconnected';
  $('miniStatus').className = `status-pill ${status}`;
  const labels = {connected:'已连接',connecting:'正在连接',reconnecting:'正在重连',error:'连接失败',disconnected:'未连接'};
  $('miniStatusText').textContent = labels[status] || status;
  $('miniQueueCount').textContent = `${state.queue.length} 人`;
  $('miniPauseBtn').textContent = state.paused ? '继续排队' : '暂停排队';
  $('miniNextBtn').disabled = state.queue.length === 0;
  $('miniSkipBtn').disabled = state.queue.length < 2;
  renderQueue();
  fillText(false);
  setDirty(dirty);
}
function renderQueue() {
  const list = $('miniQueueList');
  if (!state.queue.length) {
    list.innerHTML = '<div class="empty">当前没有排队用户</div>';
    return;
  }
  list.innerHTML = state.queue.map((user, index) => `
    <div class="queue-item${user.priority ? ' priority' : ''}" data-id="${escapeHtml(user.id)}">
      <div class="position">${String(index + 1).padStart(2, '0')}</div>
      <div class="queue-user"><div class="queue-name"><strong>${escapeHtml(user.username)}</strong><small>${user.manual ? '手动添加' : `UID ${escapeHtml(user.uid)}`}</small></div></div>
      <div class="queue-actions">
        <button class="btn small move-btn" data-up="${escapeHtml(user.id)}" ${index === 0 ? 'disabled' : ''}>↑</button>
        <button class="btn small move-btn" data-down="${escapeHtml(user.id)}" ${index === state.queue.length - 1 ? 'disabled' : ''}>↓</button>
        <button class="btn small danger" data-remove="${escapeHtml(user.id)}">移除</button>
      </div>
    </div>`).join('');
  list.querySelectorAll('[data-remove]').forEach(btn => btn.addEventListener('click', () => api('/api/queue/remove', {body:{id:btn.dataset.remove}}).catch(err => toast(err.message))));
  list.querySelectorAll('[data-up]').forEach(btn => btn.addEventListener('click', () => moveUser(btn.dataset.up, -1)));
  list.querySelectorAll('[data-down]').forEach(btn => btn.addEventListener('click', () => moveUser(btn.dataset.down, 1)));
}
async function moveUser(id, delta) {
  const ids = state.queue.map(user => user.id);
  const from = ids.indexOf(id);
  const to = from + delta;
  if (from < 0 || to < 0 || to >= ids.length) return;
  [ids[from], ids[to]] = [ids[to], ids[from]];
  await api('/api/queue/reorder', {body:{ids}}).catch(err => toast(err.message));
}
function collectTextConfig() {
  const cfg = JSON.parse(JSON.stringify(state.config));
  cfg.overlay.emptyText = $('miniEmptyText').value.trim() || '排队空闲中';
  cfg.overlay.queueEmptyText = $('miniQueueEmptyText').value.trim() || '空';
  cfg.overlay.infoText = $('miniInfoText').value;
  return cfg;
}
async function init() {
  render(await fetch('/api/state').then(r => r.json()));
  const source = new EventSource('/events');
  source.onmessage = event => render(JSON.parse(event.data));
  ['miniEmptyText','miniQueueEmptyText','miniInfoText'].forEach(id => $(id).addEventListener('input', () => setDirty(true)));
  $('miniNextBtn').addEventListener('click', () => api('/api/queue/next').catch(err => toast(err.message)));
  $('miniSkipBtn').addEventListener('click', () => api('/api/queue/skip').catch(err => toast(err.message)));
  $('miniPauseBtn').addEventListener('click', () => api('/api/queue/pause', {body:{paused:!state.paused}}).catch(err => toast(err.message)));
  $('miniClearBtn').addEventListener('click', async () => { if (confirm('清空当前队列？')) await api('/api/queue/clear'); });
  $('miniManualBtn').addEventListener('click', async () => {
    const username = $('miniManualName').value.trim();
    if (!username) return;
    try { await api('/api/queue/manual', {body:{username}}); $('miniManualName').value = ''; } catch (err) { toast(err.message); }
  });
  $('miniManualName').addEventListener('keydown', event => { if (event.key === 'Enter') $('miniManualBtn').click(); });
  $('miniApplyTextBtn').addEventListener('click', async () => {
    try { const result = await api('/api/config', {body:collectTextConfig()}); setDirty(false); render(result); toast('文字已应用'); } catch (err) { toast(err.message); }
  });
  $('miniDiscardTextBtn').addEventListener('click', () => { setDirty(false); fillText(true); toast('已放弃修改'); });
  setDirty(false);
}
init().catch(err => toast(err.message));
