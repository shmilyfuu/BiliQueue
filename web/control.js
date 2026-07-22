'use strict';

let state = null;
let saveTimer = null;
let dragId = null;
let selectedQueueUserId = null;
let mockCounter = 1;
let qrPollTimer = null;
let qrKey = null;
let fontFiles = [];
let textDraftDirty = false;
let configWriteChain = Promise.resolve();
let recordingHotkey = null;
let hotkeyValues = {openControl:'', openMiniControl:'', nextQueue:'', clearQueue:''};
let pendingHotkeys = null;
let updateCandidate = null;
let updateModalPhase = 'info';
let updateReleaseHistory = [];
let updateRecentReleases = [];
let updateInstallTarget = '';
let updateRestartMonitor = null;
let scrollGuard = null;
let scrollGuardTimer = null;

const deferredTextIds = ['emptyText', 'queueEmptyText', 'infoText'];
const deferredTextMirrors = {
  emptyText: 'currentStyleText',
  queueEmptyText: 'queueStyleText',
  infoText: 'infoStyleText',
};
const COLLAPSE_STORAGE_KEY = `biliqueue:collapse:${location.pathname}`;

const $ = id => document.getElementById(id);
const escapeHtml = value => String(value ?? '').replace(/[&<>'"]/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[ch]));

const displayVersion = value => String(value || '').replace(/-test\d+$/i, '');
const DEFAULT_OVERLAY = {
  height:50,fontSize:24,currentFontSize:24,currentTextColor:'#ffffff',currentTextOpacity:1,currentTextStrokeWidth:0,currentTextStrokeColor:'#000000',currentFontFile:'',currentFontWeight:600,currentTextAlign:'left',currentTextLineGap:0,currentBadgeText:'当前',currentBadgeTextColor:'#ffffff',currentBadgeBackground:'#6577ed',currentBadgeOpacity:.92,currentBadgeFontSize:11,currentBadgeRadius:8,currentBadgeOffsetX:-6,currentBadgeOffsetY:-6,
  queueFontSize:24,queueTextColor:'#ffffff',queueTextOpacity:1,queueTextStrokeWidth:0,queueTextStrokeColor:'#000000',queueFontFile:'',queueFontWeight:500,queueTextAlign:'left',queueTextLineGap:0,
  infoFontSize:18,infoTextColor:'#ffffff',infoTextOpacity:1,infoTextStrokeWidth:0,infoTextStrokeColor:'#000000',infoFontFile:'',infoFontWeight:500,infoTextAlign:'left',
  speed:40,effectInterval:4,effectDuration:.42,background:'#000000',gradientTopOpacity:.45,gradientBottomOpacity:.45,gradientStart:0,gradientEnd:100,avatarSize:32,currentAvatarSize:32,queueAvatarSize:32,currentAvatarNameGap:12,queueAvatarNameGap:10,
  currentBackground:'#ffffff',currentBackgroundOpacity:.07,queueBackground:'#000000',queueBackgroundOpacity:0,infoBackground:'#ffffff',infoBackgroundOpacity:.05,radius:16,
  showAvatar:true,showGuardIcon:true,showCount:true,showRules:true,showGiftIcon:true,showGiftBattery:true,giftBatterySize:14,currentEnabled:true,infoEnabled:true,
  scrollMode:'continuous',shortAlign:'center',currentWidth:300,currentSidePadding:20,queueWidth:1220,infoWidth:400,
  queueLineGap:8,queueItemGap:22,queuePageSize:5,infoLineGap:4,doubleLineEnabled:true,
  infoText:'弹幕发送“排队”加入\n达到礼物门槛可进入优先队列',emptyText:'排队空闲中',queueEmptyText:'空'
};

const RESET_GROUPS = {
  banner: ['height','radius','currentEnabled','currentWidth','queueWidth','infoEnabled','infoWidth','background','gradientTopOpacity','gradientBottomOpacity','gradientStart','gradientEnd'],
  queueStyle: ['scrollMode','shortAlign','speed','effectInterval','effectDuration','doubleLineEnabled','queueLineGap','queueItemGap','queuePageSize','giftBatterySize','showAvatar','showGuardIcon','showGiftIcon','showGiftBattery'],
  currentArea: ['currentFontSize','currentTextColor','currentTextOpacity','currentTextStrokeWidth','currentTextStrokeColor','currentFontFile','currentFontWeight','currentTextAlign','currentTextLineGap','currentSidePadding','currentAvatarSize','currentAvatarNameGap','currentBadgeText','currentBadgeTextColor','currentBadgeBackground','currentBadgeOpacity','currentBadgeFontSize','currentBadgeRadius','currentBadgeOffsetX','currentBadgeOffsetY','currentBackground','currentBackgroundOpacity'],
  queueArea: ['queueFontSize','queueTextColor','queueTextOpacity','queueTextStrokeWidth','queueTextStrokeColor','queueFontFile','queueFontWeight','queueTextAlign','queueTextLineGap','queueAvatarSize','queueAvatarNameGap','queueBackground','queueBackgroundOpacity'],
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
  currentTextLineGap: 'currentTextLineGapValue',
  queueTextOpacity: 'queueTextOpacityValue',
  queueTextStrokeWidth: 'queueTextStrokeWidthValue',
  queueTextLineGap: 'queueTextLineGapValue',
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
  giftBatterySize: 'giftBatterySizeValue',
  infoLineGap: 'infoLineGapValue',
};

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

function cancelScheduledSave() {
  clearTimeout(saveTimer);
  saveTimer = null;
}

function queueConfigWrite(operation) {
  const result = configWriteChain.catch(() => {}).then(operation);
  configWriteChain = result.catch(() => {});
  return result;
}

function documentMaxScrollY() {
  const height = Math.max(document.documentElement.scrollHeight, document.body?.scrollHeight || 0);
  return Math.max(0, height - window.innerHeight);
}

function beginScrollGuard() {
  if (scrollGuard) {
    clearTimeout(scrollGuardTimer);
    scrollGuardTimer = null;
    return;
  }
  const maxY = documentMaxScrollY();
  const bottomOffset = Math.max(0, maxY - window.scrollY);
  scrollGuard = {
    x: window.scrollX,
    y: window.scrollY,
    bottomOffset,
    stickToBottom: bottomOffset <= 2,
  };
  clearTimeout(scrollGuardTimer);
  scrollGuardTimer = null;
}

function restoreScrollGuard() {
  if (!scrollGuard) return;
  const maxY = documentMaxScrollY();
  const y = scrollGuard.stickToBottom
    ? Math.max(0, maxY - scrollGuard.bottomOffset)
    : Math.min(scrollGuard.y, maxY);
  if (window.scrollX !== scrollGuard.x || Math.abs(window.scrollY - y) > 0.5) {
    window.scrollTo(scrollGuard.x, y);
  }
}

function finishScrollGuard() {
  if (!scrollGuard) return;
  clearTimeout(scrollGuardTimer);
  scrollGuardTimer = setTimeout(() => {
    restoreScrollGuard();
    scrollGuard = null;
    scrollGuardTimer = null;
  }, 500);
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
  let saved = {};
  try { saved = JSON.parse(localStorage.getItem(COLLAPSE_STORAGE_KEY) || '{}') || {}; } catch { saved = {}; }
  document.querySelectorAll('[data-collapsible]').forEach(root => {
    const header = root.querySelector(':scope > .card-header, :scope > .section-header');
    const body = root.querySelector(':scope > .card-body, :scope > .section-body');
    if (!header || !body || header.dataset.collapseBound === 'true') return;
    header.dataset.collapseBound = 'true';
    const openByDefault = root.dataset.defaultOpen === 'true';
    const heading = header.querySelector('h1, h2, h3, h4');
    const collapseKey = root.dataset.collapseKey || root.id || heading?.textContent.trim();
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

    const setOpen = (open, persist = true) => {
      root.classList.toggle('is-collapsed', !open);
      toggle.textContent = open ? '收起' : '展开';
      toggle.setAttribute('aria-expanded', String(open));
      if (persist && collapseKey) {
        saved[collapseKey] = open;
        try { localStorage.setItem(COLLAPSE_STORAGE_KEY, JSON.stringify(saved)); } catch {}
      }
      requestAnimationFrame(syncCardHeights);
    };
    const savedOpen = collapseKey && typeof saved[collapseKey] === 'boolean' ? saved[collapseKey] : openByDefault;
    setOpen(savedOpen, false);

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

function bindDeferredTextMirrors() {
  for (const [sourceId, mirrorId] of Object.entries(deferredTextMirrors)) {
    const source = $(sourceId);
    const mirror = $(mirrorId);
    if (!source || !mirror) continue;
    source.addEventListener('input', () => { mirror.value = source.value; });
    mirror.addEventListener('input', () => {
      source.value = mirror.value;
      markTextDraftDirty();
    });
  }
}

function toast(message) {
  const node = $('toast');
  node.textContent = message;
  node.classList.add('show');
  clearTimeout(node._timer);
  node._timer = setTimeout(() => node.classList.remove('show'), 1800);
}

async function copyText(value) {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value);
      return;
    } catch {}
  }
  const scrollLeft = window.scrollX;
  const scrollTop = window.scrollY;
  const helper = document.createElement('textarea');
  helper.value = value;
  helper.setAttribute('readonly', '');
  helper.style.position = 'fixed';
  helper.style.inset = '0 auto auto 0';
  helper.style.opacity = '0';
  document.body.appendChild(helper);
  helper.focus({preventScroll:true});
  helper.select();
  let copied = false;
  try {
    copied = document.execCommand('copy');
  } finally {
    helper.remove();
    window.scrollTo(scrollLeft, scrollTop);
  }
  if (!copied) throw new Error('浏览器未允许复制，请手动选择地址复制');
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
  document.querySelectorAll('.area-apply-text, .area-discard-text').forEach(button => {
    button.disabled = !textDraftDirty;
  });
}

function markTextDraftDirty() {
  textDraftDirty = true;
  updateTextDraftStatus();
}

function syncTopCardHeights() {
  const left = document.querySelector('.connection-card');
  const right = document.querySelector('.service-card');
  if (!left || !right ||
      left.classList.contains('is-collapsed') ||
      right.classList.contains('is-collapsed') ||
      window.matchMedia('(max-width: 1120px)').matches) {
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

function measureExpandedCardHeight(card) {
  const header = card.querySelector(':scope > .card-header');
  const body = card.querySelector(':scope > .card-body');
  if (!header || !body) return card.getBoundingClientRect().height;
  if (!card.classList.contains('is-collapsed')) return card.getBoundingClientRect().height;

  const previous = {
    display: body.style.display,
    position: body.style.position,
    visibility: body.style.visibility,
    width: body.style.width,
    pointerEvents: body.style.pointerEvents,
  };
  body.style.display = 'block';
  body.style.position = 'absolute';
  body.style.visibility = 'hidden';
  body.style.width = `${card.clientWidth}px`;
  body.style.pointerEvents = 'none';
  const borderHeight = Math.max(0, card.offsetHeight - card.clientHeight);
  const height = header.getBoundingClientRect().height + body.getBoundingClientRect().height + borderHeight;
  Object.assign(body.style, previous);
  return height;
}

function syncQueueManagementHeight() {
  const queueCard = document.querySelector('.queue-management-card');
  const textCard = document.querySelector('.text-editor-card');
  const rulesCard = document.querySelector('.rules-card');
  if (!queueCard || !textCard || !rulesCard) return;

  queueCard.style.height = '';
  if (window.matchMedia('(max-width: 1120px)').matches ||
      queueCard.classList.contains('is-collapsed')) return;

  const gap = Math.max(0, parseFloat(getComputedStyle(rulesCard).marginTop) || 18);
  const height = Math.ceil(measureExpandedCardHeight(textCard) + gap + measureExpandedCardHeight(rulesCard));
  if (height > 0) queueCard.style.height = `${height}px`;
}

function syncCardHeights() {
  syncTopCardHeights();
  syncQueueManagementHeight();
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
  const icon = user.giftIcon ? `<span class="gift-icon">◆<img src="${escapeHtml(mediaImageURL(user.giftIcon))}" alt="" onerror="this.remove()"></span>` : '<span>◆</span>';
  const amount = Number(user.giftBattery || 0).toLocaleString('zh-CN', {maximumFractionDigits:2});
  return `<span class="gift-badge" title="${escapeHtml(user.giftName || '礼物')} · ${amount}电池">${icon}<b>${escapeHtml(user.giftName || '礼物')}</b><em>${amount}电池</em></span>`;
}

function hasGift(user) {
  return Boolean(user?.hasGift || user?.giftName || user?.giftIcon || Number(user?.giftBattery));
}

function renderHotkeys(config = {}, status = {}) {
  const source = pendingHotkeys || config;
  hotkeyValues = {
    openControl: source.openControl || '',
    openMiniControl: source.openMiniControl || '',
    nextQueue: source.nextQueue || '',
    clearQueue: source.clearQueue || '',
  };
  for (const key of Object.keys(hotkeyValues)) {
    const valueNode = document.querySelector(`[data-hotkey-value="${key}"]`);
    const statusNode = document.querySelector(`[data-hotkey-status="${key}"]`);
    const row = document.querySelector(`[data-hotkey-row="${key}"]`);
    const button = document.querySelector(`[data-hotkey-record="${key}"]`);
    if (valueNode && recordingHotkey !== key) valueNode.textContent = hotkeyValues[key] || '未设置';
    if (statusNode && recordingHotkey !== key) statusNode.textContent = status[key] || '未设置';
    if (row) row.classList.toggle('is-active', status[key] === '已启用');
    if (row) row.classList.toggle('has-error', Boolean(status[key]) && status[key] !== '已启用' && status[key] !== '未设置');
    if (button && recordingHotkey !== key) button.textContent = '录制快捷键';
  }
}

function hotkeyKeyFromEvent(event) {
  if (/^Key[A-Z]$/.test(event.code)) return event.code.slice(3);
  if (/^Digit[0-9]$/.test(event.code)) return event.code.slice(5);
  if (/^F(?:[1-9]|1\d|2[0-4])$/.test(event.key)) return event.key.toUpperCase();
  const keys = {
    ' ':'Space', Spacebar:'Space', Enter:'Enter', Tab:'Tab', Backspace:'Backspace', Delete:'Delete', Insert:'Insert',
    Home:'Home', End:'End', PageUp:'PageUp', PageDown:'PageDown', ArrowUp:'ArrowUp', ArrowDown:'ArrowDown',
    ArrowLeft:'ArrowLeft', ArrowRight:'ArrowRight',
  };
  return keys[event.key] || '';
}

function capturedHotkey(event) {
  const key = hotkeyKeyFromEvent(event);
  if (!key) return '';
  const parts = [];
  if (event.ctrlKey) parts.push('Ctrl');
  if (event.altKey) parts.push('Alt');
  if (event.shiftKey) parts.push('Shift');
  if (event.metaKey) parts.push('Win');
  parts.push(key);
  return parts.join('+');
}

async function saveRecordedHotkey(key, value) {
  const nextHotkeys = {...hotkeyValues, [key]:value};
  hotkeyValues = nextHotkeys;
  pendingHotkeys = nextHotkeys;
  const statusNode = document.querySelector(`[data-hotkey-status="${key}"]`);
  if (statusNode) statusNode.textContent = value ? '正在注册…' : '正在清除…';
  try {
    const result = await queueConfigWrite(() => api('/api/hotkeys', {body:nextHotkeys}));
    pendingHotkeys = null;
    render(result);
    toast(value ? '快捷键已保存' : '快捷键已清除');
  } catch (err) {
    pendingHotkeys = null;
    toast(err.message);
    if (state) renderHotkeys(state.config?.hotkeys, state.hotkeyStatus);
  }
}

function stopHotkeyRecording(value) {
  const key = recordingHotkey;
  if (!key) return;
  recordingHotkey = null;
  document.querySelector(`[data-hotkey-row="${key}"]`)?.classList.remove('is-recording');
  saveRecordedHotkey(key, value);
}

function handleHotkeyCapture(event) {
  if (!recordingHotkey || event.repeat) return;
  event.preventDefault();
  event.stopImmediatePropagation();
  if (event.key === 'Escape') {
    stopHotkeyRecording('');
    return;
  }
  if (['Control','Alt','Shift','Meta'].includes(event.key)) return;
  const value = capturedHotkey(event);
  if (!value) {
    toast('这个按键暂不支持，请换一个组合键');
    return;
  }
  stopHotkeyRecording(value);
}

function render(nextState) {
  const firstRender = !state;
  state = nextState;
  const cfg = state.config;
  const status = state.connectionStatus || 'disconnected';
	const updateStatus = state.updateStatus || {};
	if ($('checkUpdateBtn')) {
		$('checkUpdateBtn').disabled = Boolean(updateStatus.checking || updateStatus.downloading || updateStatus.installing);
		$('checkUpdateBtn').textContent = updateStatus.installing ? '正在更新' : (updateStatus.downloading ? '正在下载' : (updateStatus.checking ? '正在检查' : '检查更新'));
	}
	if (updateModalPhase === 'download-progress') renderUpdateTransferProgress(updateStatus);
  $('statusPill').className = `status-pill ${status}`;
  const labels = {connected:'已连接',connecting:'正在连接',reconnecting:'正在重连',error:'连接失败',disconnected:'未连接'};
  $('statusText').textContent = labels[status] || status;
  $('connectionDetail').textContent = state.connectionDetail || '—';
  if ($('realRoomId')) $('realRoomId').textContent = state.resolvedRoomId || '\u2014';
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
  renderHotkeys(cfg.hotkeys, state.hotkeyStatus);

  if (firstRender || document.activeElement !== $('roomId')) $('roomId').value = cfg.roomId || '';
  renderCurrent();
  renderQueue();
  renderLogs();
  fillSettings(cfg, firstRender);
  requestAnimationFrame(() => {
    syncCardHeights();
    if (scrollGuard) requestAnimationFrame(restoreScrollGuard);
  });
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

function queueDropTarget(list, itemSelector, clientY) {
  const items = [...list.querySelectorAll(itemSelector)].filter(item => item.dataset.id !== dragId);
  if (!items.length) return null;
  const before = items.find(item => {
    const rect = item.getBoundingClientRect();
    return clientY < rect.top + rect.height / 2;
  });
  return before ? {node:before, after:false} : {node:items[items.length - 1], after:true};
}

async function dropQueueUser(list, itemSelector, clientY) {
  const destination = queueDropTarget(list, itemSelector, clientY);
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
  if (selectedQueueUserId && !state.queue.some(user => user.id === selectedQueueUserId)) {
    selectedQueueUserId = null;
  }
  if (!state.queue.length) {
    list.innerHTML = '<div class="empty">\u5f53\u524d\u6ca1\u6709\u6392\u961f\u7528\u6237</div>';
    return;
  }
  list.innerHTML = state.queue.map((user, index) => `
    <div class="queue-item${hasGift(user) ? ' gifted' : ''}${user.priority ? ' priority' : ''}${user.id === selectedQueueUserId ? ' is-selected' : ''}" draggable="true" data-id="${escapeHtml(user.id)}" role="button" tabindex="0" aria-pressed="${user.id === selectedQueueUserId}">
      <span class="drag-handle" title="\u62d6\u52a8\u8c03\u6574\u987a\u5e8f" aria-hidden="true">\u2261</span>
      <div class="position">${String(index + 1).padStart(2, '0')}</div>
      <div class="queue-user">${avatarHTML(user)}<div class="queue-name"><strong>${escapeHtml(user.username)}</strong><small>${user.manual ? '\u624b\u52a8\u6dfb\u52a0' : `UID ${escapeHtml(user.uid)}`}</small></div></div>
      ${giftHTML(user)}
      <div class="queue-actions"><button class="btn small danger" data-remove="${escapeHtml(user.id)}">\u79fb\u9664</button></div>
    </div>`).join('');

  list.ondragover = event => {
    if (!dragId) return;
    event.preventDefault();
    clearDropIndicators();
    const destination = queueDropTarget(list, '.queue-item', event.clientY);
    destination?.node.classList.add(destination.after ? 'drop-after' : 'drop-before');
    event.dataTransfer.dropEffect = 'move';
  };
  list.ondrop = event => {
    if (!dragId) return;
    event.preventDefault();
    dropQueueUser(list, '.queue-item', event.clientY);
  };

  list.querySelectorAll('.queue-item').forEach(node => {
    const toggleSelected = () => {
      selectedQueueUserId = selectedQueueUserId === node.dataset.id ? null : node.dataset.id;
      renderQueue();
    };
    node.addEventListener('click', event => {
      if (event.target.closest('button,.drag-handle')) return;
      toggleSelected();
    });
    node.addEventListener('keydown', event => {
      if (event.key !== 'Enter' && event.key !== ' ') return;
      event.preventDefault();
      toggleSelected();
    });
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

function renderLogs() {
  const msg = state.lastMessage;
  $('lastMessage').textContent = msg ? `最近弹幕：${msg.username}：${msg.text}` : '尚未收到弹幕';
  const gift = state.lastGift;
  if (!gift) {
    $('lastGift').textContent = '尚未收到礼物';
    return;
  }
  const amount = Number(gift.battery || 0).toLocaleString('zh-CN', {maximumFractionDigits:2});
  const priorityEligible = gift.coinType === 'gold' && gift.battery >= state.config.giftPriority.thresholdBattery && state.config.giftPriority.enabled;
  const queueEligible = gift.coinType === 'gold' && gift.battery >= state.config.giftPriority.queueThresholdBattery && state.config.giftPriority.paidQueueEnabled;
  const queuedPriority = state.queue.some(user => Number(user.uid) === Number(gift.uid) && user.priority);
  const queued = state.queue.some(user => Number(user.uid) === Number(gift.uid));
  let result = '';
  if (priorityEligible && queuedPriority) result = '，已触发插队';
  else if (queueEligible && queued) result = '，已达到礼物排队门槛';
  else if (priorityEligible && !queued) result = '，送礼用户未在队列中，仅记录';
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
  if (force || document.activeElement !== $('giftQueueThresholdBattery')) $('giftQueueThresholdBattery').value = cfg.giftPriority?.queueThresholdBattery ?? 100;
  if (force || document.activeElement !== $('fanMedalLevel')) $('fanMedalLevel').value = cfg.eligibility?.fanMedalLevel ?? 1;
  if ($('queueEnabled')) $('queueEnabled').value = cfg.queueEnabled === false ? 'false' : 'true';
  $('paidGiftQueueEnabled').checked = Boolean(cfg.giftPriority?.paidQueueEnabled);
  $('giftPriorityEnabled').checked = Boolean(cfg.giftPriority?.enabled);
  $('giftSortByValue').checked = Boolean(cfg.giftPriority?.sortByValue);
  $('fanMedalQueueEnabled').checked = Boolean(cfg.eligibility?.fanMedalEnabled);
  $('guardQueueEnabled').checked = Boolean(cfg.eligibility?.guardEnabled);
  $('guardPriorityEnabled').checked = Boolean(cfg.eligibility?.guardPriorityEnabled);
  if ($('updateAutoCheck')) $('updateAutoCheck').checked = cfg.updates?.autoCheck !== false;

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
  setPair('currentTextLineGap', o.currentTextLineGap ?? 0, force);
  setPair('queueTextOpacity', Math.round(o.queueTextOpacity * 100), force);
  setPair('queueTextStrokeWidth', o.queueTextStrokeWidth ?? 0, force);
  setPair('queueTextLineGap', o.queueTextLineGap ?? 0, force);
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
  setPair('giftBatterySize', o.giftBatterySize ?? 14, force);
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
    queueTextAlign:o.queueTextAlign || 'left',
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
    const mirror = $(deferredTextMirrors[id]);
    if (mirror && (force || (!textDraftDirty && document.activeElement !== mirror))) mirror.value = value ?? '';
  }
  updateTextDraftStatus();
  syncTextQuickControlsFromTargets();

  if (fontFiles.length || o.currentFontFile || o.queueFontFile || o.infoFontFile) {
    if (force || document.activeElement !== $('currentFontFile')) fillFontSelect('currentFontFile', o.currentFontFile);
    if (force || document.activeElement !== $('queueFontFile')) fillFontSelect('queueFontFile', o.queueFontFile);
    if (force || document.activeElement !== $('infoFontFile')) fillFontSelect('infoFontFile', o.infoFontFile);
  }
  if ($('currentEnabled')) $('currentEnabled').checked = o.currentEnabled !== false;
  if ($('infoEnabled')) $('infoEnabled').checked = o.infoEnabled !== false;
  $('showAvatar').checked = Boolean(o.showAvatar);
  $('showGuardIcon').checked = o.showGuardIcon !== false;
  $('showCount').checked = Boolean(o.showCount);
  $('showRules').checked = Boolean(o.showRules);
  $('showGiftIcon').checked = Boolean(o.showGiftIcon);
  $('showGiftBattery').checked = o.showGiftBattery !== false;
  if ($('doubleLineEnabled')) $('doubleLineEnabled').checked = o.doubleLineEnabled !== false;
  syncQueueStyleModeControls();
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
    schemaVersion: Number(state?.config?.schemaVersion) || 16,
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
      paidQueueEnabled: $('paidGiftQueueEnabled').checked,
      queueThresholdBattery: Number($('giftQueueThresholdBattery').value) || 100,
    },
    eligibility: {
      fanMedalEnabled: $('fanMedalQueueEnabled').checked,
      fanMedalLevel: Number($('fanMedalLevel').value) || 1,
      guardEnabled: $('guardQueueEnabled').checked,
      guardPriorityEnabled: $('guardPriorityEnabled').checked,
    },
    hotkeys: {...hotkeyValues},
    updates: {...(state?.config?.updates || {autoCheck:true})},
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
      currentTextLineGap: Number($('currentTextLineGap').value),
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
      queueTextAlign: $('queueTextAlign').value,
      queueTextLineGap: Number($('queueTextLineGap').value),
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
      currentEnabled: $('currentEnabled') ? $('currentEnabled').checked : true,
      infoEnabled: $('infoEnabled') ? $('infoEnabled').checked : true,
      showAvatar: $('showAvatar').checked,
      showGuardIcon: $('showGuardIcon').checked,
      showCount: $('showCount').checked,
      showRules: $('showRules').checked,
      showGiftIcon: $('showGiftIcon').checked,
      showGiftBattery: $('showGiftBattery').checked,
      giftBatterySize: Number($('giftBatterySize').value),
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
  $('obsSizeHint').textContent = '\u5efa\u8bae\u6d4f\u89c8\u5668\u6e90\u5c3a\u5bf8\uff1a\u53cc\u884c1920 \u00d7 75\uff1b\u5355\u884c1920 \u00d7 39\uff0c\u5176\u4ed6\u5c3a\u5bf8\u5efa\u8bae\u6309\u6bd4\u4f8b\u8c03\u6574\u3002';
}

function syncQueueStyleModeControls() {
  const continuous = $('scrollMode')?.value === 'continuous';
  document.querySelectorAll('[data-queue-mode="continuous"]').forEach(node => {
    node.hidden = !continuous;
  });
  document.querySelectorAll('[data-queue-mode="effects"]').forEach(node => {
    node.hidden = continuous;
  });
}

function scheduleSave() {
  updateSizeHint();
  cancelScheduledSave();
  saveTimer = setTimeout(() => {
    saveTimer = null;
    const config = collectConfig({includeTextDrafts:false});
    const preserveScroll = Boolean(scrollGuard);
    queueConfigWrite(() => api('/api/config', {body:config}))
      .catch(err => toast(err.message))
      .finally(() => { if (preserveScroll) finishScrollGuard(); });
  }, 180);
}

function bindNumericPairs() {
  for (const [rangeId, numberId] of Object.entries(numericPairs)) {
    const range = $(rangeId);
    const number = $(numberId);
    if (!range || !number) continue;
    if (range.closest('.text-metric-quick')) continue;
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

function syncTextQuickControlsFromTargets() {
  document.querySelectorAll('.text-color-quick[data-color-target]').forEach(button => {
    const target = $(button.dataset.colorTarget);
    const swatch = button.querySelector('.color-swatch');
    if (target && swatch) swatch.style.background = target.value || '#6577ed';
  });
  document.querySelectorAll('.text-metric-quick[data-range-target]').forEach(control => {
    const target = $(control.dataset.rangeTarget);
    const targetNumber = $(numericPairs[control.dataset.rangeTarget]);
    const range = control.querySelector('.metric-range');
    const number = control.querySelector('.metric-number');
    const label = control.querySelector('.metric-label');
    if (!target || !range || !number || !label) return;
    label.dataset.label = control.dataset.label || label.textContent || '';
    ['min','max','step'].forEach(attr => {
      if (target.getAttribute(attr) !== null) {
        range.setAttribute(attr, target.getAttribute(attr));
        number.setAttribute(attr, target.getAttribute(attr));
      }
    });
    const value = target.value || targetNumber?.value || target.min || '0';
    range.value = value;
    number.value = value;
    updateTextQuickFill(control);
  });
}

function updateTextQuickFill(control) {
  const range = control.querySelector('.metric-range');
  const label = control.querySelector('.metric-label');
  if (!range || !label) return;
  const min = Number(range.min || 0);
  const max = Number(range.max || 100);
  const value = Number(range.value || min);
  const pct = max > min ? ((value - min) / (max - min)) * 100 : 0;
  label.style.setProperty('--quick-fill', Math.max(0, Math.min(100, pct)) + '%');
}

function setTextQuickMetric(control, rawValue) {
  const targetId = control.dataset.rangeTarget;
  const target = $(targetId);
  const targetNumber = $(numericPairs[targetId]);
  const range = control.querySelector('.metric-range');
  const number = control.querySelector('.metric-number');
  if (!target || !range || !number) return;
  let value = Number(rawValue);
  if (!Number.isFinite(value)) value = Number(target.min || 0);
  if (target.min !== '') value = Math.max(Number(target.min), value);
  if (target.max !== '') value = Math.min(Number(target.max), value);
  const step = Number(target.step || 1) || 1;
  const precision = String(step).includes('.') ? String(step).split('.')[1].length : 0;
  const output = precision ? value.toFixed(precision) : String(Math.round(value));
  target.value = output;
  if (targetNumber) targetNumber.value = output;
  range.value = output;
  number.value = output;
  updateTextQuickFill(control);
  syncTextQuickControlsFromTargets();
  scheduleSave();
}

function openTextQuickColorPicker(button, target) {
  const swatch = button.querySelector('.color-swatch') || button;
  const scrollLeft = window.scrollX;
  const scrollTop = window.scrollY;
  const restoreScroll = () => {
    requestAnimationFrame(() => {
      if (window.scrollX !== scrollLeft || window.scrollY !== scrollTop) window.scrollTo(scrollLeft, scrollTop);
    });
  };
  let proxy = document.querySelector('.text-quick-color-proxy');
  if (!proxy) {
    proxy = document.createElement('input');
    proxy.type = 'color';
    proxy.className = 'text-quick-color-proxy';
    proxy.tabIndex = -1;
    document.body.prepend(proxy);
  }
  const rect = swatch.getBoundingClientRect();
  proxy.style.left = Math.max(0, Math.min(window.innerWidth - 24, Math.round(rect.left))) + 'px';
  proxy.style.top = Math.max(0, Math.min(window.innerHeight - 24, Math.round(rect.top))) + 'px';
  proxy.value = target.value || '#6577ed';
  proxy.oninput = () => {
    target.value = proxy.value;
    target.dispatchEvent(new Event('input', {bubbles:true}));
    restoreScroll();
  };
  proxy.onchange = () => {
    target.value = proxy.value;
    target.dispatchEvent(new Event('change', {bubbles:true}));
    restoreScroll();
  };
  proxy.onblur = restoreScroll;
  proxy.focus({preventScroll:true});
  if (typeof proxy.showPicker === 'function') proxy.showPicker();
  else proxy.click();
  restoreScroll();
}

function bindTextQuickControls() {
  document.querySelectorAll('.text-color-quick[data-color-target]').forEach(button => {
    const target = $(button.dataset.colorTarget);
    if (!target) return;
    button.addEventListener('click', event => {
      event.preventDefault();
      openTextQuickColorPicker(button, target);
    });
    target.addEventListener('input', syncTextQuickControlsFromTargets);
    target.addEventListener('change', syncTextQuickControlsFromTargets);
  });

  document.querySelectorAll('.text-metric-quick[data-range-target]').forEach(control => {
    const target = $(control.dataset.rangeTarget);
    const range = control.querySelector('.metric-range');
    const number = control.querySelector('.metric-number');
    const label = control.querySelector('.metric-label');
    if (!target || !range || !number || !label) return;
    const stepper = document.createElement('div');
    stepper.className = 'quick-stepper';
    stepper.innerHTML = '<button type="button" data-step="1">&#9650;</button><button type="button" data-step="-1">&#9660;</button>';
    control.appendChild(stepper);
    const commitNumber = () => {
      if (number.value === '' || number.value === '-') {
        syncTextQuickControlsFromTargets();
        return;
      }
      setTextQuickMetric(control, number.value);
    };
    number.addEventListener('focus', () => number.select());
    number.addEventListener('click', () => number.select());
    number.addEventListener('input', () => {
      range.value = number.value;
      updateTextQuickFill(control);
    });
    number.addEventListener('change', commitNumber);
    number.addEventListener('keydown', event => {
      if (event.key === 'Enter') {
        event.preventDefault();
        commitNumber();
        number.select();
      }
    });

    const valueFromPointer = event => {
      const rect = label.getBoundingClientRect();
      const min = Number(target.min || 0);
      const max = Number(target.max || 100);
      const ratio = rect.width > 0 ? Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)) : 0;
      const step = Number(target.step || 1) || 1;
      const raw = min + (max - min) * ratio;
      return Math.round(raw / step) * step;
    };
    label.addEventListener('pointerdown', event => {
      event.preventDefault();
      label.setPointerCapture?.(event.pointerId);
      setTextQuickMetric(control, valueFromPointer(event));
      const move = moveEvent => setTextQuickMetric(control, valueFromPointer(moveEvent));
      const up = upEvent => {
        label.releasePointerCapture?.(upEvent.pointerId);
        window.removeEventListener('pointermove', move);
        window.removeEventListener('pointerup', up);
      };
      window.addEventListener('pointermove', move);
      window.addEventListener('pointerup', up);
    });
    stepper.querySelectorAll('button').forEach(button => {
      let timer = null;
      const run = () => {
        beginScrollGuard();
        const step = Number(target.step || 1) || 1;
        const current = Number(number.value || target.value || target.min || 0) || 0;
        setTextQuickMetric(control, current + Number(button.dataset.step) * step);
        requestAnimationFrame(restoreScrollGuard);
      };
      button.addEventListener('click', event => {
        event.preventDefault();
        run();
      });
      button.addEventListener('mousedown', event => {
        event.preventDefault();
        timer = setInterval(run, 140);
      });
      window.addEventListener('mouseup', () => { if (timer) clearInterval(timer); timer = null; });
    });
    target.addEventListener('input', syncTextQuickControlsFromTargets);
    target.addEventListener('change', syncTextQuickControlsFromTargets);
  });
  syncTextQuickControlsFromTargets();
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
  cancelScheduledSave();
  if (file.size > 2 * 1024 * 1024) throw new Error('配置文件不能超过 2MB');
  let parsed;
  try {
    parsed = JSON.parse(await file.text());
  } catch {
    throw new Error('配置文件不是有效的 JSON');
  }
  const result = await queueConfigWrite(() => api('/api/config/import', {body: parsed}));
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
  if (!await showAppConfirm({
    title:'修改服务地址',
    message:`将本机服务切换到 ${listenAddress}？\n应用后控制台会跳转到新地址。`,
    confirmText:'应用并跳转',
  })) return;
  try {
    cancelScheduledSave();
    const result = await queueConfigWrite(() => api('/api/server/listen', {body:{listenAddress}}));
    const next = result.controlUrl || `${location.protocol}//${listenAddress}/control`;
    toast('端口已修改，正在跳转');
    setTimeout(() => { location.href = next; }, 700);
  } catch (err) {
    toast(err.message);
  }
}

function formatUpdateBytes(value) {
  const bytes = Math.max(0, Number(value) || 0);
  if (bytes < 1024) return `${Math.round(bytes)} B`;
  const units = ['KB', 'MB', 'GB'];
  let size = bytes / 1024;
  let unit = units[0];
  for (let index = 1; index < units.length && size >= 1024; index += 1) {
    size /= 1024;
    unit = units[index];
  }
  return `${size >= 100 ? size.toFixed(0) : size.toFixed(1)} ${unit}`;
}

function setUpdateProgressValue(percent = null) {
  const track = $('updateProgressTrack');
  const bar = $('updateProgressBar');
  const hasPercent = Number.isFinite(percent);
  track.classList.toggle('indeterminate', !hasPercent);
  bar.style.width = hasPercent ? `${Math.max(0, Math.min(100, percent))}%` : '';
  $('updateProgressPercent').textContent = hasPercent ? `${Math.round(percent)}%` : '';
}

function showUpdateProgressPanel(mode) {
  $('updateModalBody').classList.add('hidden');
  $('updateProgressPanel').classList.remove('hidden');
  const downloadMode = mode === 'download';
  $('updateProgressTransferRow').classList.toggle('hidden', !downloadMode);
  $('updateProgressSpeedRow').classList.toggle('hidden', !downloadMode);
  $('updateProgressPathRow').classList.toggle('hidden', !downloadMode);
}

function hideUpdateProgressPanel() {
  $('updateProgressPanel').classList.add('hidden');
  $('updateModalBody').classList.remove('hidden');
}

function renderUpdateTransferProgress(status = {}) {
  showUpdateProgressPanel('download');
  const phases = {
    preparing: '准备下载',
    checksum: '读取校验文件',
    downloading: '下载更新包',
    verifying: '校验更新包',
    extracting: '解压更新包',
    ready: '准备完成',
    error: '准备失败',
  };
  const downloaded = Math.max(0, Number(status.downloadedBytes) || 0);
  const total = Math.max(0, Number(status.totalBytes) || 0);
  const percent = total > 0 ? downloaded / total * 100 : null;
  $('updateProgressMessage').textContent = status.message || '正在准备更新';
  $('updateProgressStage').textContent = phases[status.phase] || status.phase || '准备更新';
  $('updateProgressTransfer').textContent = total > 0
    ? `${formatUpdateBytes(downloaded)} / ${formatUpdateBytes(total)}`
    : (downloaded > 0 ? formatUpdateBytes(downloaded) : '正在获取文件大小');
  $('updateProgressSpeed').textContent = Number(status.bytesPerSecond) > 0 ? `${formatUpdateBytes(status.bytesPerSecond)}/s` : '—';
  $('updateProgressPath').textContent = status.downloadPath || '正在确定下载位置';
  $('updateProgressHint').textContent = '关闭此窗口不会取消已经开始的下载任务。';
  setUpdateProgressValue(status.phase === 'ready' ? 100 : percent);
}

function renderInstallProgress(stage, message, percent = null, complete = false) {
  showUpdateProgressPanel('install');
  $('updateProgressMessage').textContent = complete ? '更新完成' : '正在更新';
  $('updateProgressStage').textContent = stage;
  $('updateProgressHint').textContent = complete
    ? '更新已经完成，控制台网页即将自动刷新。'
    : '点击确认或关闭弹窗不影响更新。更新结束后会自动刷新控制台网页。';
  setUpdateProgressValue(percent);
}

function monitorUpdateRestart(targetVersion) {
  if (updateRestartMonitor) clearTimeout(updateRestartMonitor);
  updateInstallTarget = targetVersion;
  const startedAt = Date.now();
  let disconnected = false;
  const poll = async () => {
    try {
      const response = await fetch(`/api/state?update-reconnect=${Date.now()}`, {cache:'no-store'});
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const nextState = await response.json();
      if (String(nextState.version || '') === String(targetVersion || '')) {
        renderInstallProgress('新版本已启动', '更新完成', 100, true);
        updateRestartMonitor = setTimeout(() => location.reload(), 1200);
        return;
      }
      renderInstallProgress(disconnected ? '等待新版本启动' : '正在退出旧版本', '正在更新', disconnected ? 82 : 28);
    } catch {
      disconnected = true;
      renderInstallProgress('正在替换程序并重启服务', '正在更新', null);
    }
    if (Date.now() - startedAt > 120000) {
      renderInstallProgress('等待服务恢复超时，请查看运行日志', '更新仍可能在后台继续', null);
      return;
    }
    updateRestartMonitor = setTimeout(poll, 700);
  };
  updateRestartMonitor = setTimeout(poll, 500);
}

function closeUpdateModal() {
  $('updateModal').classList.add('hidden');
  hideUpdateProgressPanel();
  updateCandidate = null;
  updateModalPhase = 'info';
  configureUpdateReleaseHistory(null);
  $('updateModalSettings').classList.add('hidden');
}

function releaseVersion(value) {
  return displayVersion(String(value || '').replace(/^v/i, ''));
}

function normalizeReleaseEntry(entry) {
  const version = releaseVersion(entry?.version);
  if (!version) return null;
  const headingPattern = /^#{1,6}\s+BiliQueue\s+v[^\r\n]+\r?\n*/i;
  return {
    version,
    notes: String(entry?.notes || '该版本暂未提供更新日志。').replace(headingPattern, '').trim(),
    source: String(entry?.source || ''),
  };
}

function uniqueReleaseEntries(entries) {
  const seen = new Set();
  return entries.map(normalizeReleaseEntry).filter(entry => {
    if (!entry || seen.has(entry.version)) return false;
    seen.add(entry.version);
    return true;
  });
}

function renderUpdateReleaseEntries(entries) {
  const body = $('updateModalBody');
  body.replaceChildren();
  if (!entries.length) {
    body.textContent = '暂无更新日志。';
    return;
  }
  entries.forEach(entry => {
    const section = document.createElement('section');
    section.className = 'update-release-section';
    const heading = document.createElement('h3');
    heading.textContent = `v${entry.version}`;
    section.appendChild(heading);
    if (entry.source) {
      const source = document.createElement('div');
      source.className = 'update-release-source';
      source.textContent = `检查来源：${entry.source}`;
      section.appendChild(source);
    }
    const notes = document.createElement('div');
    notes.className = 'update-release-notes';
    notes.textContent = entry.notes || '该版本暂未提供更新日志。';
    section.appendChild(notes);
    body.appendChild(section);
  });
  body.scrollTop = 0;
}

function selectUpdateRelease(version = '') {
  const selected = version ? updateReleaseHistory.find(entry => entry.version === version) : null;
  renderUpdateReleaseEntries(selected ? [selected] : updateRecentReleases);
  $('updateRecentVersionsBtn').classList.toggle('is-active', !selected);
  $('updateHistoryList').querySelectorAll('[data-release-version]').forEach(button => {
    button.classList.toggle('is-active', button.dataset.releaseVersion === selected?.version);
  });
}

function configureUpdateReleaseHistory(releaseHistory) {
  const enabled = Boolean(releaseHistory?.all?.length);
  const layout = $('updateReleaseLayout');
  layout.classList.toggle('no-history', !enabled);
  $('updateModal').querySelector('.update-modal-card').classList.toggle('release-notes-mode', enabled);
  $('updateHistorySidebar').inert = !enabled;
  $('updateHistorySidebar').setAttribute('aria-hidden', String(!enabled));
  updateReleaseHistory = enabled ? uniqueReleaseEntries(releaseHistory.all) : [];
  updateRecentReleases = enabled ? uniqueReleaseEntries(releaseHistory.recent) : [];
  const list = $('updateHistoryList');
  list.replaceChildren();
  list.inert = !enabled;
  $('updateRecentVersionsBtn').classList.toggle('is-active', enabled);
  updateReleaseHistory.forEach(entry => {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'update-history-item';
    button.dataset.releaseVersion = entry.version;
    button.textContent = `v${entry.version}`;
    list.appendChild(button);
  });
  if (enabled) selectUpdateRelease();
}

function setUpdateModalPhase(phase) {
  updateModalPhase = phase;
  const isUpdateAction = phase === 'download' || phase === 'ready';
  $('updateModalActions').classList.remove('hidden');
  $('updateLaterBtn').classList.toggle('hidden', !isUpdateAction);
  $('updateLaterBtn').textContent = phase === 'ready' ? '下次启动时更新' : '稍后';
  $('installUpdateBtn').textContent = phase === 'ready' ? '立即更新' : (phase === 'download' ? '下载更新' : '确认');
  $('updateLaterBtn').disabled = false;
  $('installUpdateBtn').disabled = false;
}

function showUpdateModal({title, body, candidate = null, phase = candidate ? 'download' : 'info', showAutoCheck = false, releaseHistory = null}) {
  updateCandidate = candidate;
  $('updateModalTitle').textContent = title;
  configureUpdateReleaseHistory(releaseHistory);
  hideUpdateProgressPanel();
  if (!releaseHistory) $('updateModalBody').textContent = body;
  $('updateModalSettings').classList.toggle('hidden', !showAutoCheck);
  if (showAutoCheck) $('updateAutoCheck').checked = state?.config?.updates?.autoCheck !== false;
  setUpdateModalPhase(phase);
  $('updateModal').classList.remove('hidden');
  requestAnimationFrame(() => $('installUpdateBtn').focus({preventScroll:true}));
}

async function showLatestReleaseNotes() {
  const button = $('releaseNotesBtn');
  button.disabled = true;
  try {
    const local = await api('/api/update/notes', {method:'GET'});
    let remote = null;
    try {
      remote = await api('/api/update/check');
    } catch (err) {
      toast(`检查更新失败：${err.message}`);
    }
    const localReleases = uniqueReleaseEntries(Array.isArray(local.releases) ? local.releases : [{version:local.version, notes:local.notes}]);
    const currentVersion = releaseVersion(local.version);
    let currentIndex = localReleases.findIndex(entry => entry.version === currentVersion);
    if (currentIndex < 0) currentIndex = 0;
    const currentRelease = localReleases[currentIndex] || null;
    const previousRelease = localReleases[currentIndex + 1] || null;
    const newRelease = remote?.available ? normalizeReleaseEntry({version:remote.version, notes:remote.notes, source:remote.source}) : null;
    const allReleases = uniqueReleaseEntries([newRelease, ...localReleases].filter(Boolean));
    const recentReleases = [newRelease, currentRelease, previousRelease].filter(Boolean);
    showUpdateModal({
      title:'更新日志',
      body:'',
      candidate:remote?.available ? remote : null,
      phase:remote?.available ? (state.updateStatus?.preparedVersion === remote.version ? 'ready' : 'download') : 'info',
      releaseHistory:{all:allReleases, recent:recentReleases},
    });
  } catch (err) {
    toast(err.message);
  } finally {
    button.disabled = false;
  }
}

async function checkForUpdate() {
  const button = $('checkUpdateBtn');
  button.disabled = true;
  button.textContent = '正在检查';
  try {
    const result = await api('/api/update/check');
    if (!result.available) {
      showUpdateModal({title:'检查更新', body:`当前已是最新版本。\n\n当前版本：v${displayVersion(state.version)}\n远端最新版本：v${result.version}\n检查来源：${result.source}`, showAutoCheck:true});
      return;
    }
    const notes = result.notes || '该版本暂未提供更新日志。';
    showUpdateModal({
      title:`发现新版本 v${result.version}`,
      body:`检查来源：${result.source}\n\n${notes}`,
      candidate:result,
      phase: state.updateStatus?.preparedVersion === result.version ? 'ready' : 'download',
      showAutoCheck:true,
    });
  } catch (err) {
    await showAppAlert({title:'检查更新失败', message:err.message});
  } finally {
    button.disabled = false;
    button.textContent = '检查更新';
  }
}

async function init() {
  initCollapsibles();
  window.addEventListener('resize', syncCardHeights);
  $('obsUrl').textContent = `${location.origin}/overlay`;
  const initial = await fetch('/api/state').then(r => r.json());
  render(initial);
  bindNumericPairs();
  bindTextQuickControls();
  bindDeferredTextMirrors();
  await loadFontOptions();

  document.querySelectorAll('[data-hotkey-record]').forEach(button => {
    button.addEventListener('click', () => {
      if (recordingHotkey) {
        const previous = recordingHotkey;
        recordingHotkey = null;
        document.querySelector(`[data-hotkey-row="${previous}"]`)?.classList.remove('is-recording');
        renderHotkeys(state?.config?.hotkeys, state?.hotkeyStatus);
      }
      recordingHotkey = button.dataset.hotkeyRecord;
      const row = document.querySelector(`[data-hotkey-row="${recordingHotkey}"]`);
      const valueNode = document.querySelector(`[data-hotkey-value="${recordingHotkey}"]`);
      const statusNode = document.querySelector(`[data-hotkey-status="${recordingHotkey}"]`);
      row?.classList.add('is-recording');
      if (valueNode) valueNode.textContent = '请按下快捷键';
      if (statusNode) statusNode.textContent = '按 Esc 停止并清除';
      button.textContent = '录制中';
    });
  });
  document.addEventListener('keydown', handleHotkeyCapture, true);

  const source = new EventSource('/events');
  source.onmessage = event => render(JSON.parse(event.data));
  source.onerror = () => $('connectionDetail').textContent = '控制台与本机服务的事件流已中断，浏览器会自动尝试恢复';

  $('loginBtn').addEventListener('click', startQrLogin);
	$('releaseNotesBtn').addEventListener('click', showLatestReleaseNotes);
	$('checkUpdateBtn').addEventListener('click', checkForUpdate);
	$('updateModalCloseBtn').addEventListener('click', closeUpdateModal);
	$('updateRecentVersionsBtn').addEventListener('click', () => selectUpdateRelease());
	$('updateHistoryList').addEventListener('click', event => {
		const button = event.target.closest('[data-release-version]');
		if (button) selectUpdateRelease(button.dataset.releaseVersion);
	});
	$('updateLaterBtn').addEventListener('click', async () => {
		if (updateModalPhase !== 'ready') {
			closeUpdateModal();
			return;
		}
		const button = $('updateLaterBtn');
		button.disabled = true;
		$('installUpdateBtn').disabled = true;
		try {
			const result = await api('/api/update/defer');
			updateCandidate = null;
			setUpdateModalPhase('info');
			configureUpdateReleaseHistory(null);
			$('updateModalBody').textContent = `v${result.version} 已准备好，将在下次启动 BiliQueue 时安装。当前直播不会受到影响。`;
		} catch (err) {
			button.disabled = false;
			$('installUpdateBtn').disabled = false;
			await showAppAlert({title:'更新失败', message:err.message});
		}
	});
	$('updateModal').addEventListener('click', event => { if (event.target === $('updateModal')) closeUpdateModal(); });
	$('updateAutoCheck').addEventListener('change', async event => {
		const checkbox = event.currentTarget;
		const enabled = checkbox.checked;
		checkbox.disabled = true;
		try {
			const nextState = await api('/api/update/settings', {body:{autoCheck:enabled}});
			render(nextState);
		} catch (err) {
			checkbox.checked = !enabled;
			await showAppAlert({title:'设置失败', message:err.message});
		} finally {
			checkbox.disabled = false;
		}
	});
	$('installUpdateBtn').addEventListener('click', async () => {
		if (updateModalPhase === 'download-progress' || updateModalPhase === 'install-progress' || updateModalPhase === 'install-complete') {
			closeUpdateModal();
			return;
		}
		if (!updateCandidate) {
			closeUpdateModal();
			return;
		}
		const button = $('installUpdateBtn');
		button.disabled = true;
		$('updateLaterBtn').disabled = true;
		try {
			if (updateModalPhase === 'download') {
				setUpdateModalPhase('download-progress');
				configureUpdateReleaseHistory(null);
				renderUpdateTransferProgress(state?.updateStatus || {});
				const result = await api('/api/update/download');
				setUpdateModalPhase('ready');
				configureUpdateReleaseHistory(null);
				hideUpdateProgressPanel();
				$('updateModalBody').textContent = `v${result.version} 更新包已下载并解压完成。\n\n请选择立即更新，或安排在下次启动 BiliQueue 时更新。`;
				return;
			}
			const targetVersion = updateCandidate.version;
			setUpdateModalPhase('install-progress');
			configureUpdateReleaseHistory(null);
			renderInstallProgress('正在启动更新助手', '正在更新', 12);
			await api('/api/update/apply');
			updateCandidate = null;
			renderInstallProgress('正在退出旧版本', '正在更新', 28);
			monitorUpdateRestart(targetVersion);
		} catch (err) {
			button.disabled = false;
			button.textContent = updateModalPhase === 'ready' ? '立即更新' : (updateModalPhase === 'download' ? '下载更新' : '确认');
			$('updateLaterBtn').disabled = false;
			await showAppAlert({title:'更新失败', message:err.message});
		}
	});
  $('refreshQrBtn').addEventListener('click', startQrLogin);
  $('closeQrBtn').addEventListener('click', () => { stopQrPolling(); $('qrModal').classList.add('hidden'); });
  $('qrModal').addEventListener('click', event => { if (event.target === $('qrModal')) { stopQrPolling(); $('qrModal').classList.add('hidden'); } });
  $('logoutBtn').addEventListener('click', async () => {
    if (!await showAppConfirm({title:'退出登录', message:'确定退出当前 B 站登录吗？', confirmText:'退出登录', danger:true})) return;
    try { await api('/api/auth/logout'); } catch (err) { toast(err.message); }
  });
  $('exportConfigBtn').addEventListener('click', () => exportConfig().then(() => toast('配置已导出')).catch(err => toast(err.message)));
  $('importConfigBtn').addEventListener('click', () => $('importConfigFile').click());
  $('importConfigFile').addEventListener('change', async event => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    if (!await showAppConfirm({title:'导入配置', message:'导入配置会覆盖当前设置，现有设置会先自动备份。', confirmText:'导入配置', danger:true})) return;
    try { await importConfigFile(file); } catch (err) { toast(err.message); }
  });

  $('connectBtn').addEventListener('click', async () => {
    try { await api('/api/connect', {body:{roomId:$('roomId').value.trim()}}); } catch (err) { toast(err.message); }
  });
  $('disconnectBtn').addEventListener('click', () => api('/api/disconnect').catch(err => toast(err.message)));
  $('nextBtn').addEventListener('click', () => api('/api/queue/next').catch(err => toast(err.message)));
  $('skipBtn').addEventListener('click', () => api('/api/queue/skip').catch(err => toast(err.message)));
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

  $('mockJoinBtn').addEventListener('click', async () => {
    const uid = Date.now() + mockCounter;
    const username = `测试用户${String(mockCounter++).padStart(2,'0')}`;
    try { await api('/api/debug/message', {body:{uid,username,text:state.config.joinCommand}}); } catch (err) { toast(err.message); }
  });
  $('mockGiftBtn').addEventListener('click', async () => {
    const selected = selectedQueueUserId ? state.queue.find(user => user.id === selectedQueueUserId) : null;
    if (!selected) {
      await showAppAlert({title:'模拟礼物', message:'请先选中要添加礼物的用户。'});
      return;
    }
    const rawBattery = $('mockGiftBattery').value.trim();
    const battery = rawBattery === '' ? Number(state.config.giftPriority.thresholdBattery) : Number(rawBattery);
    if (!Number.isFinite(battery) || battery <= 0) return toast('礼物价值需要是大于 0 的数字');
    try { await api('/api/debug/gift', {body:{queueUserId:selected.id,giftName:'测试礼物',battery}}); } catch (err) { toast(err.message); }
  });
  $('mockGiftBattery').addEventListener('keydown', event => {
    if (event.key === 'Enter') $('mockGiftBtn').click();
  });

  const settingIds = ['queueEnabled','joinCommand','cancelCommand','clearCommand','nextCommand','maxQueue','giftQueueThresholdBattery','giftThresholdBattery','fanMedalLevel','paidGiftQueueEnabled','guardPriorityEnabled','giftPriorityEnabled','giftSortByValue','fanMedalQueueEnabled','guardQueueEnabled','background','currentEnabled','infoEnabled','currentBackground','queueBackground','infoBackground','scrollMode','shortAlign','currentTextColor','currentTextStrokeColor','currentFontFile','currentFontWeight','currentTextAlign','currentBadgeText','currentBadgeTextColor','currentBadgeBackground','currentBadgeOffsetX','currentBadgeOffsetY','queueTextColor','queueTextStrokeColor','queueFontFile','queueFontWeight','queueTextAlign','infoTextColor','infoTextStrokeColor','infoFontFile','infoFontWeight','infoTextAlign','showAvatar','showGuardIcon','showCount','showRules','showGiftIcon','showGiftBattery','doubleLineEnabled'];
  settingIds.forEach(id => $(id).addEventListener('input', () => { scheduleSave(); syncTextQuickControlsFromTargets(); }));
  settingIds.forEach(id => $(id).addEventListener('change', () => { scheduleSave(); syncTextQuickControlsFromTargets(); }));
  $('scrollMode').addEventListener('change', syncQueueStyleModeControls);
  deferredTextIds.forEach(id => $(id).addEventListener('input', markTextDraftDirty));
  $('applyTextBtn').addEventListener('click', async () => {
    cancelScheduledSave();
    const config = collectConfig({includeTextDrafts:true});
    try {
      const result = await queueConfigWrite(() => api('/api/config', {body:config}));
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
  document.querySelectorAll('.area-apply-text').forEach(button => {
    button.addEventListener('click', () => $('applyTextBtn').click());
  });
  document.querySelectorAll('.area-discard-text').forEach(button => {
    button.addEventListener('click', () => $('discardTextBtn').click());
  });
  updateTextDraftStatus();

  const backToTopBtn = $('backToTopBtn');
  const syncBackToTop = () => backToTopBtn.classList.toggle('is-visible', window.scrollY > 360);
  window.addEventListener('scroll', syncBackToTop, {passive:true});
  backToTopBtn.addEventListener('click', () => {
    window.scrollTo({top:0, behavior:window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'});
  });
  syncBackToTop();
  $('refreshFontsBtn').addEventListener('click', async () => {
    await loadFontOptions(true);
    $('previewFrame').contentWindow.location.reload();
  });

  async function resetOverlayGroup(groupName, label) {
    cancelScheduledSave();
    const keys = RESET_GROUPS[groupName] || [];
    const cfg = collectConfig({includeTextDrafts:false});
    cfg.overlay = {...cfg.overlay};
    for (const key of keys) {
      if (Object.prototype.hasOwnProperty.call(DEFAULT_OVERLAY, key)) cfg.overlay[key] = DEFAULT_OVERLAY[key];
    }
    try {
      const result = await queueConfigWrite(() => api('/api/config', {body:cfg}));
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
  $('copyUrlBtn').addEventListener('click', async () => {
    try {
      await copyText($('obsUrl').textContent);
      toast('\u5730\u5740\u5df2\u590d\u5236');
    } catch (err) {
      toast(err.message);
    }
  });
  $('openOverlayBtn').addEventListener('click', () => window.open('/overlay','_blank'));
  $('openMiniControlBtn').addEventListener('click', async () => {
    try {
      await api('/api/window/mini-control/open');
    } catch (err) {
      if (err.status === 501) window.open('/mini-control', '_blank');
      else toast(err.message);
    }
  });
  $('changeListenBtn').addEventListener('click', changeListenAddress);
  $('resetListenBtn').addEventListener('click', () => { $('listenHost').value = '127.0.0.1'; $('listenPort').value = '18303'; toast('\u5df2\u6062\u590d\u9ed8\u8ba4\u670d\u52a1\u5730\u5740\uff0c\u5e94\u7528\u540e\u751f\u6548'); });
}

init().catch(err => toast(err.message));
