'use strict';

let state = null;
let dragId = null;
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

function mediaImageURL(raw) {
  const value = String(raw || '').trim();
  if (!value) return '';
  if (value.startsWith('/api/media/image?')) return value;
  return `/api/media/image?url=${encodeURIComponent(value)}`;
}

function avatarHTML(user) {
  const initial = escapeHtml((user?.username || '?').slice(0, 1));
  if (user?.avatar) {
    const src = escapeHtml(mediaImageURL(user.avatar));
    return `<span class="avatar">${initial}<img src="${src}" alt="" onerror="this.remove()"></span>`;
  }
  return `<span class="avatar">${initial}</span>`;
}

function giftHTML(user) {
  if (!user?.priority) return '';
  const icon = user.giftIcon ? `<span class="gift-icon">◆<img src="${escapeHtml(mediaImageURL(user.giftIcon))}" alt="" onerror="this.remove()"></span>` : '<span>◆</span>';
  const amount = Number(user.giftBattery || 0).toLocaleString('zh-CN', {maximumFractionDigits:2});
  return `<span class="gift-badge" title="${escapeHtml(user.giftName || '礼物')} · ${amount}电池">${icon}<b>${escapeHtml(user.giftName || '礼物')}</b><em>${amount}电池</em></span>`;
}

function render(next) {
  state = next;
  const status = state.connectionStatus || 'disconnected';
  $('miniStatus').className = `status-pill ${status}`;
  const labels = {connected:'已连接',connecting:'正在连接',reconnecting:'正在重连',error:'连接失败',disconnected:'未连接'};
  $('miniStatusText').textContent = labels[status] || status;
  $('queueCount').textContent = `${state.queue.length} 人`;
  $('pauseBtn').textContent = state.paused ? '继续排队' : '暂停排队';
  $('nextBtn').disabled = state.queue.length === 0;
  $('skipBtn').disabled = state.queue.length < 2;
  renderCurrent();
  renderQueue();
}

function renderCurrent() {
  const user = state.queue[0];
  if (!user) {
    $('currentCard').innerHTML = `<div class="current-copy"><small>当前用户</small><strong>${escapeHtml(state.config.overlay.emptyText || '排队空闲中')}</strong></div>`;
    return;
  }
  $('currentCard').innerHTML = `${avatarHTML(user)}<div class="current-copy"><small>当前用户 · 第 1 位</small><strong>${escapeHtml(user.username)}</strong>${giftHTML(user)}</div>`;
}

function clearDropIndicators() {
  document.querySelectorAll('.queue-item.drop-before,.queue-item.drop-after').forEach(node => node.classList.remove('drop-before','drop-after'));
}

async function submitQueueOrder(ids) {
  try { await api('/api/queue/reorder', {body:{ids}}); } catch (err) { toast(err.message); }
}

async function moveQueueUser(id, delta) {
  const ids = state.queue.map(user => user.id);
  const from = ids.indexOf(id);
  const to = from + delta;
  if (from < 0 || to < 0 || to >= ids.length) return;
  [ids[from], ids[to]] = [ids[to], ids[from]];
  await submitQueueOrder(ids);
}

function renderQueue() {
  const list = $('queueList');
  if (!state.queue.length) {
    list.innerHTML = '<div class="empty">当前没有排队用户</div>';
    return;
  }
  list.innerHTML = state.queue.map((user, index) => `
    <div class="queue-item${user.priority ? ' priority' : ''}" draggable="true" data-id="${escapeHtml(user.id)}">
      <span class="drag-handle" title="拖动调整顺序" aria-hidden="true">≡</span>
      <div class="position">${String(index + 1).padStart(2, '0')}</div>
      <div class="queue-user">${avatarHTML(user)}<div class="queue-name"><strong>${escapeHtml(user.username)}</strong><small>${user.manual ? '手动添加' : `UID ${escapeHtml(user.uid)}`}</small>${giftHTML(user)}</div></div>
      <div class="queue-actions">
        <button class="btn small move-btn" data-move-up="${escapeHtml(user.id)}" ${index === 0 ? 'disabled' : ''} title="上移一位">↑</button>
        <button class="btn small move-btn" data-move-down="${escapeHtml(user.id)}" ${index === state.queue.length - 1 ? 'disabled' : ''} title="下移一位">↓</button>
        <button class="btn small danger" data-remove="${escapeHtml(user.id)}">移除</button>
      </div>
    </div>`).join('');

  list.querySelectorAll('.queue-item').forEach(node => {
    node.addEventListener('dragstart', event => {
      dragId = node.dataset.id;
      node.classList.add('dragging');
      event.dataTransfer.effectAllowed = 'move';
      event.dataTransfer.setData('text/plain', dragId);
    });
    node.addEventListener('dragend', () => {
      node.classList.remove('dragging');
      dragId = null;
      clearDropIndicators();
    });
    node.addEventListener('dragover', event => {
      event.preventDefault();
      if (!dragId || dragId === node.dataset.id) return;
      clearDropIndicators();
      const rect = node.getBoundingClientRect();
      node.classList.add(event.clientY < rect.top + rect.height / 2 ? 'drop-before' : 'drop-after');
      event.dataTransfer.dropEffect = 'move';
    });
    node.addEventListener('dragleave', event => {
      if (!node.contains(event.relatedTarget)) node.classList.remove('drop-before','drop-after');
    });
    node.addEventListener('drop', async event => {
      event.preventDefault();
      const targetId = node.dataset.id;
      if (!dragId || dragId === targetId) return clearDropIndicators();
      const after = node.classList.contains('drop-after');
      const ids = state.queue.map(user => user.id);
      const from = ids.indexOf(dragId);
      if (from < 0) return clearDropIndicators();
      ids.splice(from, 1);
      const target = ids.indexOf(targetId);
      ids.splice(target + (after ? 1 : 0), 0, dragId);
      clearDropIndicators();
      await submitQueueOrder(ids);
    });
  });
  list.querySelectorAll('[data-move-up]').forEach(btn => btn.addEventListener('click', () => moveQueueUser(btn.dataset.moveUp, -1)));
  list.querySelectorAll('[data-move-down]').forEach(btn => btn.addEventListener('click', () => moveQueueUser(btn.dataset.moveDown, 1)));
  list.querySelectorAll('[data-remove]').forEach(btn => btn.addEventListener('click', async () => {
    try { await api('/api/queue/remove', {body:{id:btn.dataset.remove}}); } catch (err) { toast(err.message); }
  }));
}

async function init() {
  render(await fetch('/api/state').then(r => r.json()));
  const source = new EventSource('/events');
  source.onmessage = event => render(JSON.parse(event.data));
  source.onerror = () => toast('简易控制页与本机服务的事件流已中断，浏览器会自动尝试恢复');
  $('nextBtn').addEventListener('click', () => api('/api/queue/next').catch(err => toast(err.message)));
  $('skipBtn').addEventListener('click', () => api('/api/queue/skip').catch(err => toast(err.message)));
  $('pauseBtn').addEventListener('click', () => api('/api/queue/pause', {body:{paused:!state.paused}}).catch(err => toast(err.message)));
  $('clearBtn').addEventListener('click', async () => { if (confirm('清空当前队列？')) await api('/api/queue/clear'); });
  $('endBtn').addEventListener('click', async () => { if (confirm('结束本场并清空队列？')) await api('/api/session/end'); });
  $('manualBtn').addEventListener('click', async () => {
    const username = $('manualName').value.trim();
    if (!username) return;
    try { await api('/api/queue/manual', {body:{username}}); $('manualName').value = ''; } catch (err) { toast(err.message); }
  });
  $('manualName').addEventListener('keydown', event => { if (event.key === 'Enter') $('manualBtn').click(); });
}

init().catch(err => toast(err.message));
