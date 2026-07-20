'use strict';

let state = null;
let dragId = null;
let nativeWindowState = null;
const $ = id => document.getElementById(id);
const escapeHtml = value => String(value ?? '').replace(/[&<>'"]/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[ch]));

async function api(path, options = {}) {
  const response = await fetch(path, {
    method: options.method || 'POST',
    headers: {'Content-Type': 'application/json'},
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    const err = new Error(data.error || `HTTP ${response.status}`);
    err.status = response.status;
    throw err;
  }
  return data;
}

function renderNativeWindowState(next) {
  nativeWindowState = next;
  const button = $('topmostBtn');
  const available = Boolean(next?.supported && next?.active);
  button.classList.toggle('hidden', !available);
  button.classList.toggle('is-active', Boolean(next?.topmost));
  button.textContent = next?.topmost ? '取消置顶' : '置顶';
  button.setAttribute('aria-pressed', next?.topmost ? 'true' : 'false');
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
  const manual = Boolean(user?.manual);
  const initial = manual ? '⭐️' : escapeHtml((user?.username || '?').slice(0, 1));
  const avatarClass = `avatar${manual ? ' manual-avatar' : ''}`;
  const guard = guardIconHTML(user);
  if (manual) return `<span class="${avatarClass}">${initial}${guard}</span>`;
  if (user?.avatar) {
    const src = escapeHtml(mediaImageURL(user.avatar));
    return `<span class="${avatarClass}">${initial}<img class="avatar-image" src="${src}" alt="" onerror="this.remove()">${guard}</span>`;
  }
  return `<span class="${avatarClass}">${initial}${guard}</span>`;
}

function guardIconHTML(user) {
  const icons = {1:['icon_governor.png','总督'],2:['icon_supervisor.png','提督'],3:['icon_captain.png','舰长']};
  const item = icons[Number(user?.guardLevel || 0)];
  if (!item) return '';
  return `<img class="guard-icon" src="/assets/${item[0]}" alt="${item[1]}" title="${item[1]}">`;
}

function giftHTML(user) {
  if (!hasGift(user)) return '';
  const amount = Number(user.giftBattery || 0).toLocaleString('zh-CN', {maximumFractionDigits:2});
  return `<span class="mini-gift-battery" title="${escapeHtml(user.giftName || '礼物')}">${amount}电池</span>`;
}

function hasGift(user) {
  return Boolean(user?.hasGift || user?.giftName || user?.giftIcon || Number(user?.giftBattery));
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
  renderQueue();
}

function clearDropIndicators() {
  document.querySelectorAll('.mini-queue-item.drop-before,.mini-queue-item.drop-after').forEach(node => node.classList.remove('drop-before','drop-after'));
}

async function submitQueueOrder(ids) {
  try { await api('/api/queue/reorder', {body:{ids}}); } catch (err) { toast(err.message); }
}

function queueDropTarget(list, clientY) {
  const items = [...list.querySelectorAll('.mini-queue-item')].filter(item => item.dataset.id !== dragId);
  if (!items.length) return null;
  const before = items.find(item => {
    const rect = item.getBoundingClientRect();
    return clientY < rect.top + rect.height / 2;
  });
  return before ? {node:before, after:false} : {node:items[items.length - 1], after:true};
}

async function dropQueueUser(list, clientY) {
  const destination = queueDropTarget(list, clientY);
  if (!dragId || !destination) return clearDropIndicators();
  const ids = state.queue.map(user => user.id);
  const from = ids.indexOf(dragId);
  if (from < 0) return clearDropIndicators();
  ids.splice(from, 1);
  const target = ids.indexOf(destination.node.dataset.id);
  ids.splice(target + (destination.after ? 1 : 0), 0, dragId);
  clearDropIndicators();
  await submitQueueOrder(ids);
}

function renderQueue() {
  const list = $('queueList');
  if (!state.queue.length) {
    list.innerHTML = '<div class="mini-empty">当前没有排队用户</div>';
    return;
  }
  list.innerHTML = state.queue.map((user, index) => `
    <div class="mini-queue-item${hasGift(user) ? ' gifted' : ''}${user.priority ? ' priority' : ''}" draggable="true" data-id="${escapeHtml(user.id)}">
      <span class="mini-drag-handle" title="拖动调整顺序" aria-hidden="true">≡</span>
      <div class="mini-position">${String(index + 1).padStart(2, '0')}</div>
      <div class="mini-queue-user">${avatarHTML(user)}<div class="mini-queue-name"><strong>${escapeHtml(user.username)}</strong></div></div>
      ${giftHTML(user)}
      <button class="btn danger mini-remove-btn" data-remove="${escapeHtml(user.id)}">移除</button>
    </div>`).join('');

  list.ondragover = event => {
    if (!dragId) return;
    event.preventDefault();
    clearDropIndicators();
    const destination = queueDropTarget(list, event.clientY);
    destination?.node.classList.add(destination.after ? 'drop-after' : 'drop-before');
    event.dataTransfer.dropEffect = 'move';
  };
  list.ondrop = event => {
    if (!dragId) return;
    event.preventDefault();
    dropQueueUser(list, event.clientY);
  };

  list.querySelectorAll('.mini-queue-item').forEach(node => {
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
  });
  list.querySelectorAll('[data-remove]').forEach(btn => btn.addEventListener('click', async () => {
    try { await api('/api/queue/remove', {body:{id:btn.dataset.remove}}); } catch (err) { toast(err.message); }
  }));
}

async function init() {
  const [initialState, initialWindowState] = await Promise.all([
    fetch('/api/state').then(r => r.json()),
    fetch('/api/window/mini-control').then(r => r.json()),
  ]);
  render(initialState);
  renderNativeWindowState(initialWindowState);
  const source = new EventSource('/events');
  source.onmessage = event => render(JSON.parse(event.data));
  source.onerror = () => toast('简易控制页与本机服务的事件流已中断，浏览器会自动尝试恢复');
  $('nextBtn').addEventListener('click', () => api('/api/queue/next').catch(err => toast(err.message)));
  $('pauseBtn').addEventListener('click', () => api('/api/queue/pause', {body:{paused:!state.paused}}).catch(err => toast(err.message)));
  $('clearBtn').addEventListener('click', async () => {
    if (!await showAppConfirm({title:'清空队列', message:'确定清空当前队列吗？此操作无法撤销。', confirmText:'清空队列', danger:true})) return;
    try { await api('/api/queue/clear'); } catch (err) { toast(err.message); }
  });
  $('manualBtn').addEventListener('click', async () => {
    const username = $('manualName').value.trim();
    if (!username) return;
    try { await api('/api/queue/manual', {body:{username}}); $('manualName').value = ''; } catch (err) { toast(err.message); }
  });
  $('manualName').addEventListener('keydown', event => { if (event.key === 'Enter') $('manualBtn').click(); });
  $('topmostBtn').addEventListener('click', async () => {
    try {
      const next = await api('/api/window/mini-control/topmost', {body:{topmost:!nativeWindowState?.topmost}});
      renderNativeWindowState(next);
    } catch (err) {
      toast(err.message);
    }
  });
}

function notifyNativeWindowReady() {
  const notify = window.__biliqueueMiniReady;
  if (typeof notify !== 'function') return;
  requestAnimationFrame(() => requestAnimationFrame(() => {
    Promise.resolve(notify()).catch(() => {});
  }));
}

init()
  .catch(err => toast(err.message))
  .finally(notifyNativeWindowReady);
