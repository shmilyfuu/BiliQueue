'use strict';

let state = null;
let saveTimer = null;
let dragId = null;
let mockCounter = 1;
let qrPollTimer = null;
let qrKey = null;
let fontFiles = [];
let textDraftDirty = false;

const deferredTextIds = ['emptyText', 'queueEmptyText', 'infoText'];

const $ = id => document.getElementById(id);
const escapeHtml = value => String(value ?? '').replace(/[&<>'"]/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[ch]));

const displayVersion = value => String(value || '').replace(/-test\d+$/i, '');
const DEFAULT_OVERLAY = {
  height:120,fontSize:24,currentFontSize:24,currentTextColor:'#ffffff',currentTextOpacity:1,currentTextStrokeWidth:0,currentTextStrokeColor:'#000000',currentFontFile:'',currentFontWeight:600,currentTextAlign:'left',currentBadgeText:'当前',currentBadgeTextColor:'#ffffff',currentBadgeBackground:'#6577ed',currentBadgeOpacity:.92,currentBadgeFontSize:11,currentBadgeRadius:8,currentBadgeOffsetX:-6,currentBadgeOffsetY:-6,
  queueFontSize:24,queueTextColor:'#ffffff',queueTextOpacity:1,queueTextStrokeWidth:0,queueTextStrokeColor:'#000000',queueFontFile:'',queueFontWeight:500,
  infoFontSize:18,infoTextColor:'#ffffff',infoTextOpacity:1,infoTextStrokeWidth:0,infoTextStrokeColor:'#000000',infoFontFile:'',infoFontWeight:500,infoTextAlign:'left',
  speed:40,effectInterval:4,effectDuration:.42,background:'#000000',gradientTopOpacity:.45,gradientBottomOpacity:.45,gradientStart:0,gradientEnd:100,avatarSize:32,currentAvatarSize:32,queueAvatarSize:32,currentAvatarNameGap:12,queueAvatarNameGap:10,
  currentBackground:'#ffffff',currentBackgroundOpacity:.07,queueBackground:'#000000',queueBackgroundOpacity:0,infoBackground:'#ffffff',infoBackgroundOpacity:.05,radius:16,
  showAvatar:true,showCount:true,showRules:true,showGiftIcon:true,
  scrollMode:'continuous',shortAlign:'center',currentWidth:300,currentSidePadding:20,queueWidth:1220,infoWidth:400,
  queueLineGap:8,queueItemGap:22,queuePageSize:5,infoLineGap:4,doubleLineEnabled:true,
  infoText:'弹幕发送“排队”加入\n达到礼物门槛可进入优先队列',emptyText:'排队空闲中',queueEmptyText:'空'
};

const RESET_GROUPS = {
  banner: ['height','radius','currentWidth','queueWidth','infoWidth','background','gradientTopOpacity','gradientBottomOpacity','gradientStart','gradientEnd'],
  queueStyle: ['scrollMode','shortAlign','speed','effectInterval','effectDuration','doubleLineEnabled','queueLineGap','queueItemGap','queuePageSize','showAvatar','showGiftIcon'],
  currentArea: ['currentFontSize','currentTextColor','currentTextOpacity','currentTextStrokeWidth','currentTextStrokeColor','currentFontFile','currentFontWeight','currentTextAlign','currentSidePadding','currentAvatarSize','currentAvatarNameGap','currentBadgeText','currentBadgeTextColor','currentBadgeBackground','currentBadgeOpacity','currentBadgeFontSize','currentBadgeRadius','currentBadgeOffsetX','currentBadgeOffsetY','currentBackground','currentBackgroundOpacity'],
  queueArea: ['queueFontSize','queueTextColor','queueTextOpacity','queueTextStrokeWidth','queueTextStrokeColor','queueFontFile','queueFontWeight','queueAvatarSize','queueAvatarNameGap','queueBackground','queueBackgroundOpacity'],
  infoArea: ['infoFontSize','infoTextColor','infoTextOpacity','infoTextStrokeWidth','infoTextStrokeColor','infoFontFile','infoFontWeight','infoTextAlign','infoLineGap','infoBackground','infoBackgroundOpacity'],
};
RESET_GROUPS.textArea = [...RESET_GROUPS.currentArea, ...RESET_GROUPS.queueArea, ...RESET_GROUPS.infoArea];

function parseListenAddress(value) {
  const raw = String(value || '').trim().replace(/^https?:\/\//i, '').replace(/\/+$/, '');
  const fallbackHost = location.hostname || '127.0.0.1';
  const fallbackPort = location.port || '18303';
  if (!raw) return {host:fallbackHost, port:fallbackPort};
  if (/^\d+$/.test(raw)) return {host:'127.0.0.1', port:raw};
  if (raw.startsWith(':')) return {host:'127.0.0.1', port:raw.slice(1) || fallbackPort};
  const idx = raw.lastIndexOf(':');
  if (idx > -1 && idx < raw.length - 1) return {host:raw.slice(0, idx) || '127.0.0.1', port:raw.slice(idx + 1)};
  return {host:raw || fallbackHost, port:fallbackPort};
}

function composeListenAddress() {
  const host = ($('listenHost')?.value || '').trim() || '127.0.0.1';
  const port = Number(($('listenPort')?.value || '').trim());
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('端口需要是 1 到 65535 之间的数字');
  return `${host}:${port}`;
}

const numericPairs = {
  height: 'heightValue',
  currentFontSize: 'currentFontSizeValue',
  queueFontSize: 'queueFontSizeValue',
  infoFontSize: 'infoFontSizeValue',
  speed: 'speedValue',
  effectInterval: 'effectIntervalValue',
  effectDuration: 'effectDurationValue',
  gradientTopOpacity: 'gradientTopOpacityValue',
  gradientBottomOpacity: 'gradientBottomOpacityValue',
  gradientStart: 'gradientStartValue',
  gradientEnd: 'gradientEndValue',
  currentAvatarSize: 'currentAvatarSizeValue',
  queueAvatarSize: 'queueAvatarSizeValue',
  currentAvatarNameGap: 'currentAvatarNameGapValue',
  currentBadgeOpacity: 'currentBadgeOpacityValue',
  currentBadgeFontSize: 'currentBadgeFontSizeValue',
  currentBadgeRadius: 'currentBadgeRadiusValue',
  currentBadgeOffsetX: 'currentBadgeOffsetXValue',
  currentBadgeOffsetY: 'currentBadgeOffsetYValue',
  queueAvatarNameGap: 'queueAvatarNameGapValue',
  currentTextOpacity: 'currentTextOpacityValue',
  currentTextStrokeWidth: 'currentTextStrokeWidthValue',
  queueTextOpacity: 'queueTextOpacityValue',
  queueTextStrokeWidth: 'queueTextStrokeWidthValue',
  infoTextOpacity: 'infoTextOpacityValue',
  infoTextStrokeWidth: 'infoTextStrokeWidthValue',
  currentBackgroundOpacity: 'currentBackgroundOpacityValue',
  queueBackgroundOpacity: 'queueBackgroundOpacityValue',
  infoBackgroundOpacity: 'infoBackgroundOpacityValue',
  radius: 'radiusValue',
  currentWidth: 'currentWidthValue',
  currentSidePadding: 'currentSidePaddingValue',
  queueWidth: 'queueWidthValue',
  infoWidth: 'infoWidthValue',
  queueLineGap: 'queueLineGapValue',
  queueItemGap: 'queueItemGapValue',
  queuePageSize: 'queuePageSizeValue',
  infoLineGap: 'infoLineGapValue',
};

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


function mediaImageURL(raw) {
  const value = String(raw || '').trim();
  if (!value) return '';
  if (value.startsWith('/api/media/image?')) return value;
  return `/api/media/image?url=${encodeURIComponent(value)}`;
}

function fillFontSelect(id, selected) {
  const select = $(id);
  if (!select) return;
  const value = String(selected || '');
  const options = ['<option value="">默认字体</option>'];
  for (const font of fontFiles) {
    const label = `${font.label || font.file} · ${String(font.file).split('.').pop().toUpperCase()}`;
    options.push(`<option value="${escapeHtml(font.file)}">${escapeHtml(label)}</option>`);
  }
  if (value && !fontFiles.some(font => font.file === value)) {
    options.push(`<option value="${escapeHtml(value)}">${escapeHtml(value)}（文件不存在）</option>`);
  }
  select.innerHTML = options.join('');
  select.value = value;
}

async function loadFontOptions(announce = false) {
  try {
    const result = await api('/api/fonts', {method:'GET'});
    fontFiles = Array.isArray(result.fonts) ? result.fonts : [];
    const o = state?.config?.overlay || {};
    fillFontSelect('currentFontFile', o.currentFontFile);
    fillFontSelect('queueFontFile', o.queueFontFile);
    fillFontSelect('infoFontFile', o.infoFontFile);
    $('fontStatus').textContent = fontFiles.length ? `已读取 ${fontFiles.length} 个字体文件` : 'fonts 文件夹中暂无字体文件';
    if (announce) toast(fontFiles.length ? `已刷新 ${fontFiles.length} 个字体` : '未发现字体文件');
  } catch (err) {
    $('fontStatus').textContent = `字体读取失败：${err.message}`;
    if (announce) toast(err.message);
  }
}


function initCollapsibles() {
  document.querySelectorAll('[data-collapsible]').forEach(root => {
    const header = root.querySelector(':scope > .card-header, :scope > .section-header');
    const body = root.querySelector(':scope > .card-body, :scope > .section-body');
    if (!header || !body || header.dataset.collapseBound === 'true') return;
    header.dataset.collapseBound = 'true';
    const openByDefault = root.dataset.defaultOpen === 'true';
    const toggle = document.createElement('button');
    toggle.type = 'button';
    toggle.className = 'collapse-toggle';
    const resetBtn = header.querySelector(':scope > .reset-group-btn');
    if (resetBtn) {
      const actions = document.createElement('div');
      actions.className = 'collapse-actions';
      header.appendChild(actions);
      actions.appendChild(resetBtn);
      actions.appendChild(toggle);
    } else {
      header.appendChild(toggle);
    }

    const setOpen = open => {
      root.classList.toggle('is-collapsed', !open);
      toggle.textContent = open ? '收起' : '展开';
      toggle.setAttribute('aria-expanded', String(open));
      requestAnimationFrame(syncTopCardHeights);
    };
    setOpen(openByDefault);

    const shouldIgnore = target => Boolean(target.closest('button, input, select, textarea, a'));
    header.addEventListener('click', event => {
      if (shouldIgnore(event.target)) return;
      setOpen(root.classList.contains('is-collapsed'));
    });
    toggle.addEventListener('click', event => {
      event.stopPropagation();
      setOpen(root.classList.contains('is-collapsed'));
    });
  });
}

function toast(message) {
  const node = $('toast');
  node.textContent = message;
  node.classList.add('show');
  clearTimeout(node._timer);
  node._timer = setTimeout(() => node.classList.remove('show'), 1800);
}

function updateTextDraftStatus() {
  const node = $('textDraftStatus');
  if (!node) return;
  node.textContent = textDraftDirty ? '有未应用修改' : '已同步';
  node.className = `draft-status ${textDraftDirty ? 'dirty' : 'synced'}`;
  const applyBtn = $('applyTextBtn');
  const discardBtn = $('discardTextBtn');
  if (applyBtn) applyBtn.disabled = !textDraftDirty;
  if (discardBtn) discardBtn.disabled = !textDraftDirty;
}

function markTextDraftDirty() {
  textDraftDirty = true;
  updateTextDraftStatus();
}

function syncTopCardHeights() {
  const left = document.querySelector('.connection-card');
  const right = document.querySelector('.service-card');
  if (!left || !right || window.matchMedia('(max-width: 1120px)').matches) {
    if (left) left.style.minHeight = '';
    if (right) right.style.minHeight = '';
    return;
  }
  left.style.minHeight = '';
  right.style.minHeight = '';
  const h = Math.max(left.getBoundingClientRect().height, right.getBoundingClientRect().height);
  if (h > 0) {
    left.style.minHeight = `${Math.ceil(h)}px`;
    right.style.minHeight = `${Math.ceil(h)}px`;
  }
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

function render(nextState) {
  const firstRender = !state;
  state = nextState;
  const cfg = state.config;
  const status = state.connectionStatus || 'disconnected';
  $('statusPill').className = `status-pill ${status}`;
  const labels = {connected:'已连接',connecting:'正在连接',reconnecting:'正在重连',error:'连接失败',disconnected:'未连接'};
  $('statusText').textContent = labels[status] || status;
  $('connectionDetail').textContent = state.connectionDetail || '—';
  $('realRoomId').textContent = state.resolvedRoomId || '—';
  $('anchorName').textContent = state.anchorName ? `${state.anchorName}${state.anchorUid ? ` · UID ${state.anchorUid}` : ''}` : (state.anchorUid ? `UID ${state.anchorUid}` : '—');
  $('roomTitle').textContent = state.roomTitle || '—';
  if ($('appVersion')) $('appVersion').textContent = `v${displayVersion(state.version)}`;
  const loggedIn = state.loginStatus === 'logged_in';
  $('loginText').textContent = loggedIn ? (state.loginName || `UID ${state.loginUid}`) : '尚未登录';
  $('loginDetail').textContent = state.loginDetail || (loggedIn ? '登录凭证保存在本机。' : '连接弹幕前需要扫码登录，凭证只保存在本机。');
  $('logoutBtn').disabled = !loggedIn;
  $('connectBtn').disabled = !loggedIn;
  $('queueCount').textContent = `${state.queue.length} 人`;
  $('pauseBtn').textContent = state.paused ? '继续排队' : '暂停排队';
  const listenParts = parseListenAddress(cfg.listenAddress || location.host);
  if ($('listenHost') && (firstRender || document.activeElement !== $('listenHost'))) $('listenHost').value = listenParts.host;
  if ($('listenPort') && (firstRender || document.activeElement !== $('listenPort'))) $('listenPort').value = listenParts.port;
  $('nextBtn').disabled = state.queue.length === 0;
  $('skipBtn').disabled = state.queue.length < 2;

  if (firstRender || document.activeElement !== $('roomId')) $('roomId').value = cfg.roomId || '';
  renderCurrent();
  renderQueue();
  renderLogs();
  fillSettings(cfg, firstRender);
  requestAnimationFrame(syncTopCardHeights);
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

function renderLogs() {
  const msg = state.lastMessage;
  $('lastMessage').textContent = msg ? `最近弹幕：${msg.username}：${msg.text}` : '尚未收到弹幕';
  const gift = state.lastGift;
  if (!gift) {
    $('lastGift').textContent = '尚未收到礼物';
    return;
  }
  const amount = Number(gift.battery || 0).toLocaleString('zh-CN', {maximumFractionDigits:2});
  const eligible = gift.coinType === 'gold' && gift.battery >= state.config.giftPriority.thresholdBattery && state.config.giftPriority.enabled;
  const queuedPriority = state.queue.some(user => Number(user.uid) === Number(gift.uid) && user.priority);
  const result = eligible ? (queuedPriority ? '，已触发插队' : '，送礼用户未在队列中，仅记录') : '';
  $('lastGift').textContent = `最近礼物：${gift.username} 送出 ${gift.giftName} × ${gift.num}，约 ${amount} 电池${result}`;
}

function setPair(id, value, force = false) {
  const range = $(id);
  const number = $(numericPairs[id]);
  if (!range || !number) return;
  if (force || (document.activeElement !== range && document.activeElement !== number)) {
    range.value = value;
    number.value = value;
  }
}

function fillSettings(cfg, force) {
  for (const id of ['joinCommand','cancelCommand','clearCommand','nextCommand','maxQueue']) {
    if (force || document.activeElement !== $(id)) $(id).value = cfg[id];
  }
  if (force || document.activeElement !== $('giftThresholdBattery')) $('giftThresholdBattery').value = cfg.giftPriority?.thresholdBattery ?? 100;
  if ($('queueEnabled')) $('queueEnabled').value = cfg.queueEnabled === false ? 'false' : 'true';
  $('giftPriorityEnabled').checked = Boolean(cfg.giftPriority?.enabled);
  $('giftSortByValue').checked = Boolean(cfg.giftPriority?.sortByValue);

  const o = cfg.overlay;
  setPair('height', o.height, force);
  setPair('currentFontSize', o.currentFontSize, force);
  setPair('queueFontSize', o.queueFontSize, force);
  setPair('infoFontSize', o.infoFontSize, force);
  setPair('speed', o.speed, force);
  setPair('effectInterval', o.effectInterval ?? 4, force);
  setPair('effectDuration', o.effectDuration ?? 0.42, force);
  setPair('gradientTopOpacity', Math.round(o.gradientTopOpacity * 100), force);
  setPair('gradientBottomOpacity', Math.round(o.gradientBottomOpacity * 100), force);
  setPair('gradientStart', o.gradientStart ?? Math.max(0, 100 - (o.gradientRange ?? 100)), force);
  setPair('gradientEnd', o.gradientEnd ?? 100, force);
  setPair('currentAvatarSize', o.currentAvatarSize ?? o.avatarSize ?? 32, force);
  setPair('queueAvatarSize', o.queueAvatarSize ?? o.avatarSize ?? 32, force);
  setPair('currentAvatarNameGap', o.currentAvatarNameGap ?? 12, force);
  setPair('currentBadgeOpacity', Math.round((o.currentBadgeOpacity ?? 0.92) * 100), force);
  setPair('currentBadgeFontSize', o.currentBadgeFontSize ?? 11, force);
  setPair('currentBadgeRadius', o.currentBadgeRadius ?? 8, force);
  setPair('currentBadgeOffsetX', o.currentBadgeOffsetX ?? -6, force);
  setPair('currentBadgeOffsetY', o.currentBadgeOffsetY ?? -6, force);
  setPair('queueAvatarNameGap', o.queueAvatarNameGap ?? 10, force);
  setPair('currentTextOpacity', Math.round(o.currentTextOpacity * 100), force);
  setPair('currentTextStrokeWidth', o.currentTextStrokeWidth ?? 0, force);
  setPair('queueTextOpacity', Math.round(o.queueTextOpacity * 100), force);
  setPair('queueTextStrokeWidth', o.queueTextStrokeWidth ?? 0, force);
  setPair('infoTextOpacity', Math.round(o.infoTextOpacity * 100), force);
  setPair('infoTextStrokeWidth', o.infoTextStrokeWidth ?? 0, force);
  setPair('currentBackgroundOpacity', Math.round(o.currentBackgroundOpacity * 100), force);
  setPair('queueBackgroundOpacity', Math.round(o.queueBackgroundOpacity * 100), force);
  setPair('infoBackgroundOpacity', Math.round(o.infoBackgroundOpacity * 100), force);
  setPair('radius', o.radius, force);
  setPair('currentWidth', o.currentWidth, force);
  setPair('currentSidePadding', o.currentSidePadding ?? 20, force);
  setPair('queueWidth', o.queueWidth, force);
  setPair('infoWidth', o.infoWidth, force);
  setPair('queueLineGap', o.queueLineGap, force);
  setPair('queueItemGap', o.queueItemGap ?? 22, force);
  setPair('queuePageSize', o.queuePageSize ?? o.queueSecondPageSize ?? 5, force);
  setPair('infoLineGap', o.infoLineGap, force);

  const plain = {
    background:o.background,
    scrollMode:o.scrollMode,
    shortAlign:o.shortAlign,
    currentTextColor:o.currentTextColor,
    currentTextStrokeColor:o.currentTextStrokeColor || '#000000',
    currentFontFile:o.currentFontFile,
    currentFontWeight:o.currentFontWeight,
    currentTextAlign:o.currentTextAlign,
    currentBadgeText:o.currentBadgeText || '当前',
    currentBadgeTextColor:o.currentBadgeTextColor || '#ffffff',
    currentBadgeBackground:o.currentBadgeBackground || '#6577ed',
    queueTextColor:o.queueTextColor,
    queueTextStrokeColor:o.queueTextStrokeColor || '#000000',
    queueFontFile:o.queueFontFile,
    queueFontWeight:o.queueFontWeight,
    infoTextColor:o.infoTextColor,
    infoTextStrokeColor:o.infoTextStrokeColor || '#000000',
    infoFontFile:o.infoFontFile,
    infoFontWeight:o.infoFontWeight,
    infoTextAlign:o.infoTextAlign,
    currentBackground:o.currentBackground,
    queueBackground:o.queueBackground,
    infoBackground:o.infoBackground,
  };
  for (const [id, value] of Object.entries(plain)) {
    const node = $(id);
    if (force || document.activeElement !== node) node.value = value ?? '';
  }

  const textValues = {
    emptyText:o.emptyText,
    queueEmptyText:o.queueEmptyText,
    infoText:o.infoText,
  };
  for (const [id, value] of Object.entries(textValues)) {
    const node = $(id);
    if (force || (!textDraftDirty && document.activeElement !== node)) node.value = value ?? '';
  }
  updateTextDraftStatus();

  if (fontFiles.length || o.currentFontFile || o.queueFontFile || o.infoFontFile) {
    if (force || document.activeElement !== $('currentFontFile')) fillFontSelect('currentFontFile', o.currentFontFile);
    if (force || document.activeElement !== $('queueFontFile')) fillFontSelect('queueFontFile', o.queueFontFile);
    if (force || document.activeElement !== $('infoFontFile')) fillFontSelect('infoFontFile', o.infoFontFile);
  }
  $('showAvatar').checked = Boolean(o.showAvatar);
  $('showCount').checked = Boolean(o.showCount);
  $('showRules').checked = Boolean(o.showRules);
  $('showGiftIcon').checked = Boolean(o.showGiftIcon);
  if ($('doubleLineEnabled')) $('doubleLineEnabled').checked = o.doubleLineEnabled !== false;
  updateSizeHint();
}

function clampNumber(node) {
  let value = Number(node.value);
  if (!Number.isFinite(value)) value = Number(node.min || 0);
  if (node.min !== '') value = Math.max(Number(node.min), value);
  if (node.max !== '') value = Math.min(Number(node.max), value);
  return value;
}

function collectConfig(options = {}) {
  const includeTextDrafts = Boolean(options.includeTextDrafts);
  const currentOverlay = state?.config?.overlay || {};
  return {
    schemaVersion: 11,
    listenAddress: state?.config?.listenAddress || location.host,
    roomId: state?.config?.roomId || $('roomId').value.trim(),
    queueEnabled: $('queueEnabled')?.value !== 'false',
    joinCommand: $('joinCommand').value.trim() || '排队',
    cancelCommand: $('cancelCommand').value.trim() || '取消排队',
    clearCommand: $('clearCommand').value.trim() || '清空队列',
    nextCommand: $('nextCommand').value.trim() || '下一位',
    maxQueue: Number($('maxQueue').value) || 100,
    giftPriority: {
      enabled: $('giftPriorityEnabled').checked,
      thresholdBattery: Number($('giftThresholdBattery').value) || 100,
      sortByValue: $('giftSortByValue').checked,
    },
    overlay: {
      height: Number($('height').value),
      fontSize: Number($('queueFontSize').value),
      currentFontSize: Number($('currentFontSize').value),
      currentTextColor: $('currentTextColor').value,
      currentTextOpacity: Number($('currentTextOpacity').value) / 100,
      currentTextStrokeWidth: Number($('currentTextStrokeWidth').value || 0),
      currentTextStrokeColor: $('currentTextStrokeColor').value,
      currentFontFile: $('currentFontFile').value,
      currentFontWeight: Number($('currentFontWeight').value),
      currentTextAlign: $('currentTextAlign').value,
      currentBadgeText: $('currentBadgeText').value.trim() || '当前',
      currentBadgeTextColor: $('currentBadgeTextColor').value,
      currentBadgeBackground: $('currentBadgeBackground').value,
      currentBadgeOpacity: Number($('currentBadgeOpacity').value) / 100,
      currentBadgeFontSize: Number($('currentBadgeFontSize').value),
      currentBadgeRadius: Number($('currentBadgeRadius').value),
      currentBadgeOffsetX: Number($('currentBadgeOffsetX').value),
      currentBadgeOffsetY: Number($('currentBadgeOffsetY').value),
      queueFontSize: Number($('queueFontSize').value),
      queueTextColor: $('queueTextColor').value,
      queueTextOpacity: Number($('queueTextOpacity').value) / 100,
      queueTextStrokeWidth: Number($('queueTextStrokeWidth').value || 0),
      queueTextStrokeColor: $('queueTextStrokeColor').value,
      queueFontFile: $('queueFontFile').value,
      queueFontWeight: Number($('queueFontWeight').value),
      infoFontSize: Number($('infoFontSize').value),
      infoTextColor: $('infoTextColor').value,
      infoTextOpacity: Number($('infoTextOpacity').value) / 100,
      infoTextStrokeWidth: Number($('infoTextStrokeWidth').value || 0),
      infoTextStrokeColor: $('infoTextStrokeColor').value,
      infoFontFile: $('infoFontFile').value,
      infoFontWeight: Number($('infoFontWeight').value),
      infoTextAlign: $('infoTextAlign').value,
      speed: Number($('speed').value),
      effectInterval: Number($('effectInterval').value),
      effectDuration: Number($('effectDuration').value),
      background: $('background').value,
      gradientTopOpacity: Number($('gradientTopOpacity').value) / 100,
      gradientBottomOpacity: Number($('gradientBottomOpacity').value) / 100,
      gradientStart: Number($('gradientStart').value),
      gradientEnd: Number($('gradientEnd').value),
      avatarSize: Number($('queueAvatarSize').value),
      currentAvatarSize: Number($('currentAvatarSize').value),
      queueAvatarSize: Number($('queueAvatarSize').value),
      currentAvatarNameGap: Number($('currentAvatarNameGap').value),
      queueAvatarNameGap: Number($('queueAvatarNameGap').value),
      currentBackground: $('currentBackground').value,
      currentBackgroundOpacity: Number($('currentBackgroundOpacity').value) / 100,
      queueBackground: $('queueBackground').value,
      queueBackgroundOpacity: Number($('queueBackgroundOpacity').value) / 100,
      infoBackground: $('infoBackground').value,
      infoBackgroundOpacity: Number($('infoBackgroundOpacity').value) / 100,
      radius: Number($('radius').value),
      showAvatar: $('showAvatar').checked,
      showCount: $('showCount').checked,
      showRules: $('showRules').checked,
      showGiftIcon: $('showGiftIcon').checked,
      scrollMode: $('scrollMode').value,
      shortAlign: $('shortAlign').value,
      currentWidth: Number($('currentWidth').value),
      currentSidePadding: Number($('currentSidePadding').value),
      queueWidth: Number($('queueWidth').value),
      infoWidth: Number($('infoWidth').value),
      queueLineGap: Number($('queueLineGap').value),
      queueItemGap: Number($('queueItemGap').value),
      queuePageSize: Number($('queuePageSize').value),
      infoLineGap: Number($('infoLineGap').value),
      doubleLineEnabled: $('doubleLineEnabled').checked,
      infoText: includeTextDrafts ? $('infoText').value : (currentOverlay.infoText ?? $('infoText').value),
      emptyText: includeTextDrafts ? ($('emptyText').value.trim() || '排队空闲中') : ((currentOverlay.emptyText ?? $('emptyText').value.trim()) || '排队空闲中'),
      queueEmptyText: includeTextDrafts ? ($('queueEmptyText').value.trim() || '空') : ((currentOverlay.queueEmptyText ?? $('queueEmptyText').value.trim()) || '空'),
    },
  };
}

function updateSizeHint() {
  const width = Number($('currentWidth').value || 0) + Number($('queueWidth').value || 0) + Number($('infoWidth').value || 0);
  const height = Number($('height').value || 0);
  $('obsSizeHint').textContent = `建议浏览器源尺寸：${width} × ${height}，FPS 30。三个区域宽度之和即横条总宽度。`;
}

function scheduleSave() {
  updateSizeHint();
  clearTimeout(saveTimer);
  saveTimer = setTimeout(async () => {
    try { await api('/api/config', {body:collectConfig({includeTextDrafts:false})}); } catch (err) { toast(err.message); }
  }, 180);
}

function bindNumericPairs() {
  for (const [rangeId, numberId] of Object.entries(numericPairs)) {
    const range = $(rangeId);
    const number = $(numberId);
    if (!range || !number) continue;
    addStepperButtons(number, range);
    range.addEventListener('input', () => {
      number.value = range.value;
      scheduleSave();
    });
    number.addEventListener('input', () => {
      if (number.value === '' || number.value === '-') return;
      const value = clampNumber(number);
      range.value = value;
      scheduleSave();
    });
    number.addEventListener('change', () => {
      const value = clampNumber(number);
      number.value = value;
      range.value = value;
      scheduleSave();
    });
  }
}

function stepNumber(number, range, direction) {
  const step = Number(number.step || range.step || 1) || 1;
  const precision = String(step).includes('.') ? String(step).split('.')[1].length : 0;
  const current = Number(number.value || number.min || 0) || 0;
  number.value = (current + direction * step).toFixed(precision);
  const value = clampNumber(number);
  number.value = value;
  range.value = value;
  scheduleSave();
}

function addStepperButtons(number, range) {
  const box = number.closest('.value-box');
  if (!box || box.querySelector('.stepper')) return;
  const stepper = document.createElement('div');
  stepper.className = 'stepper';
  stepper.innerHTML = '<button type="button" data-step="1">▲</button><button type="button" data-step="-1">▼</button>';
  box.appendChild(stepper);
  stepper.querySelectorAll('button').forEach(btn => {
    let timer = null;
    const run = () => stepNumber(number, range, Number(btn.dataset.step));
    btn.addEventListener('click', run);
    btn.addEventListener('mousedown', () => { timer = setInterval(run, 140); });
    window.addEventListener('mouseup', () => { if (timer) clearInterval(timer); timer = null; });
  });
}

function drawQRCode(text) {
  const canvas = $('qrCanvas');
  const ctx = canvas.getContext('2d');
  const qr = new QRCodeModel(0, QRErrorCorrectLevel.M);
  qr.addData(text);
  qr.make();
  const count = qr.getModuleCount();
  const quiet = 4;
  const total = count + quiet * 2;
  const size = canvas.width;
  const cell = Math.floor(size / total);
  const used = cell * total;
  const offset = Math.floor((size - used) / 2);
  ctx.fillStyle = '#fff';
  ctx.fillRect(0, 0, size, size);
  ctx.fillStyle = '#000';
  for (let row = 0; row < count; row++) {
    for (let col = 0; col < count; col++) {
      if (qr.isDark(row, col)) ctx.fillRect(offset + (col + quiet) * cell, offset + (row + quiet) * cell, cell, cell);
    }
  }
}

function stopQrPolling() {
  if (qrPollTimer) clearInterval(qrPollTimer);
  qrPollTimer = null;
  qrKey = null;
}

async function pollQrLogin() {
  if (!qrKey) return;
  try {
    const result = await api('/api/auth/qrcode/poll', {body:{key:qrKey}});
    $('qrStatus').textContent = result.message || '等待扫码';
    if (result.status === 'success') {
      stopQrPolling();
      $('qrModal').classList.add('hidden');
      toast('B站登录成功');
    } else if (result.status === 'expired' || result.status === 'error') {
      stopQrPolling();
    }
  } catch (err) {
    $('qrStatus').textContent = err.message;
  }
}

async function startQrLogin() {
  stopQrPolling();
  $('qrModal').classList.remove('hidden');
  $('qrStatus').textContent = '正在生成二维码…';
  try {
    const result = await api('/api/auth/qrcode/start');
    qrKey = result.key;
    drawQRCode(result.url);
    $('qrStatus').textContent = '请使用哔哩哔哩手机客户端扫码并确认。';
    qrPollTimer = setInterval(pollQrLogin, 1800);
    pollQrLogin();
  } catch (err) {
    $('qrStatus').textContent = err.message;
  }
}

async function exportConfig() {
  const response = await fetch('/api/config/export');
  if (!response.ok) throw new Error(`导出失败：HTTP ${response.status}`);
  const blob = await response.blob();
  const disposition = response.headers.get('Content-Disposition') || '';
  const match = disposition.match(/filename="?([^";]+)"?/i);
  const filename = match?.[1] || `BiliQueue-config-${new Date().toISOString().slice(0,10)}.json`;
  const link = document.createElement('a');
  link.href = URL.createObjectURL(blob);
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(link.href);
}

async function importConfigFile(file) {
  if (!file) return;
  if (file.size > 2 * 1024 * 1024) throw new Error('配置文件不能超过 2MB');
  let parsed;
  try {
    parsed = JSON.parse(await file.text());
  } catch {
    throw new Error('配置文件不是有效的 JSON');
  }
  const result = await api('/api/config/import', {body: parsed});
  textDraftDirty = false;
  if (result.state) render(result.state);
  updateTextDraftStatus();
  toast(`配置已导入，旧配置已备份为 ${result.backupFile || '备份文件'}`);
}

async function changeListenAddress() {
  let listenAddress;
  try {
    listenAddress = composeListenAddress();
  } catch (err) {
    return toast(err.message);
  }
  if (!confirm(`将本机服务切换到 ${listenAddress}？控制台会跳转到新地址。`)) return;
  try {
    const result = await api('/api/server/listen', {body:{listenAddress}});
    const next = result.controlUrl || `${location.protocol}//${listenAddress}/control`;
    toast('端口已修改，正在跳转');
    setTimeout(() => { location.href = next; }, 700);
  } catch (err) {
    toast(err.message);
  }
}

async function init() {
  initCollapsibles();
  window.addEventListener('resize', syncTopCardHeights);
  $('obsUrl').textContent = `${location.origin}/overlay`;
  const initial = await fetch('/api/state').then(r => r.json());
  render(initial);
  bindNumericPairs();
  await loadFontOptions();

  const source = new EventSource('/events');
  source.onmessage = event => render(JSON.parse(event.data));
  source.onerror = () => $('connectionDetail').textContent = '控制台与本机服务的事件流已中断，浏览器会自动尝试恢复';

  $('loginBtn').addEventListener('click', startQrLogin);
  $('refreshQrBtn').addEventListener('click', startQrLogin);
  $('closeQrBtn').addEventListener('click', () => { stopQrPolling(); $('qrModal').classList.add('hidden'); });
  $('qrModal').addEventListener('click', event => { if (event.target === $('qrModal')) { stopQrPolling(); $('qrModal').classList.add('hidden'); } });
  $('logoutBtn').addEventListener('click', async () => { if (confirm('退出当前 B 站登录？')) await api('/api/auth/logout'); });
  $('exportConfigBtn').addEventListener('click', () => exportConfig().then(() => toast('配置已导出')).catch(err => toast(err.message)));
  $('importConfigBtn').addEventListener('click', () => $('importConfigFile').click());
  $('importConfigFile').addEventListener('change', async event => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    if (!confirm('导入配置会覆盖当前设置，现有设置会先自动备份。继续吗？')) return;
    try { await importConfigFile(file); } catch (err) { toast(err.message); }
  });

  $('connectBtn').addEventListener('click', async () => {
    try { await api('/api/connect', {body:{roomId:$('roomId').value.trim()}}); } catch (err) { toast(err.message); }
  });
  $('disconnectBtn').addEventListener('click', () => api('/api/disconnect').catch(err => toast(err.message)));
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

  $('mockJoinBtn').addEventListener('click', async () => {
    const uid = Date.now() + mockCounter;
    const username = `测试用户${String(mockCounter++).padStart(2,'0')}`;
    await api('/api/debug/message', {body:{uid,username,text:state.config.joinCommand}});
  });
  $('mockCancelBtn').addEventListener('click', async () => {
    const user = state.queue[state.queue.length - 1];
    if (!user) return toast('队列为空');
    await api('/api/debug/message', {body:{uid:user.uid,username:user.username,text:state.config.cancelCommand}});
  });
  $('mockGiftBtn').addEventListener('click', async () => {
    const ordinary = state.queue.slice(1).reverse().find(user => !user.priority);
    const uid = ordinary?.uid || Date.now() + mockCounter;
    const username = ordinary?.username || `礼物用户${String(mockCounter++).padStart(2,'0')}`;
    await api('/api/debug/gift', {body:{uid,username,giftName:'测试礼物',battery:state.config.giftPriority.thresholdBattery}});
  });

  const settingIds = ['queueEnabled','joinCommand','cancelCommand','clearCommand','nextCommand','maxQueue','giftThresholdBattery','giftPriorityEnabled','giftSortByValue','background','currentBackground','queueBackground','infoBackground','scrollMode','shortAlign','currentTextColor','currentTextStrokeColor','currentFontFile','currentFontWeight','currentTextAlign','currentBadgeText','currentBadgeTextColor','currentBadgeBackground','currentBadgeOffsetX','currentBadgeOffsetY','queueTextColor','queueTextStrokeColor','queueFontFile','queueFontWeight','infoTextColor','infoTextStrokeColor','infoFontFile','infoFontWeight','infoTextAlign','showAvatar','showCount','showRules','showGiftIcon','doubleLineEnabled'];
  settingIds.forEach(id => $(id).addEventListener('input', scheduleSave));
  settingIds.forEach(id => $(id).addEventListener('change', scheduleSave));
  deferredTextIds.forEach(id => $(id).addEventListener('input', markTextDraftDirty));
  $('applyTextBtn').addEventListener('click', async () => {
    clearTimeout(saveTimer);
    try {
      const result = await api('/api/config', {body:collectConfig({includeTextDrafts:true})});
      textDraftDirty = false;
      if (result) render(result);
      updateTextDraftStatus();
      toast('文字已应用到横条');
    } catch (err) {
      toast(err.message);
    }
  });
  $('discardTextBtn').addEventListener('click', () => {
    textDraftDirty = false;
    if (state?.config) fillSettings(state.config, true);
    updateTextDraftStatus();
    toast('已放弃未应用文字修改');
  });
  updateTextDraftStatus();
  $('refreshFontsBtn').addEventListener('click', async () => {
    await loadFontOptions(true);
    $('previewFrame').contentWindow.location.reload();
  });

  async function resetOverlayGroup(groupName, label) {
    const keys = RESET_GROUPS[groupName] || [];
    const cfg = collectConfig({includeTextDrafts:false});
    cfg.overlay = {...cfg.overlay};
    for (const key of keys) {
      if (Object.prototype.hasOwnProperty.call(DEFAULT_OVERLAY, key)) cfg.overlay[key] = DEFAULT_OVERLAY[key];
    }
    try {
      const result = await api('/api/config', {body:cfg});
      if (result) render(result);
      toast(`已恢复${label}默认值`);
    } catch (err) {
      toast(err.message);
    }
  }
  const resetBindings = [
    ['resetBannerStyleBtn','banner','大小与样式'],
    ['resetQueueStyleBtn','queueStyle','队列调整与样式'],
    ['resetTextAreaBtn','textArea','文字区域样式'],
    ['resetCurrentAreaBtn','currentArea','当前区域'],
    ['resetQueueAreaBtn','queueArea','队列区域'],
    ['resetInfoAreaBtn','infoArea','说明区域'],
  ];
  resetBindings.forEach(([id, group, label]) => {
    const btn = $(id);
    if (btn) btn.addEventListener('click', event => {
      event.stopPropagation();
      resetOverlayGroup(group, label);
    });
  });
  $('copyUrlBtn').addEventListener('click', async () => { await navigator.clipboard.writeText($('obsUrl').textContent); toast('地址已复制'); });
  $('openOverlayBtn').addEventListener('click', () => window.open('/overlay','_blank'));
  $('openMiniControlBtn').addEventListener('click', () => window.open('/mini-control','_blank'));
  $('changeListenBtn').addEventListener('click', changeListenAddress);
}

init().catch(err => toast(err.message));
