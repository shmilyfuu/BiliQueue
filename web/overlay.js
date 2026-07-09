'use strict';

let state = null;
let activeTrack = null;
let activeRow = null;
let offset = 0;
let lastTime = performance.now();
let cycleWidth = 0;
let scrolling = false;
let pageTimer = null;
let pageResetTimer = null;
let pageIndex = 0;
let renderToken = 0;
let lastQueueSignature = "";
let lastDoubleMode = null;
const isPreview = new URLSearchParams(location.search).has('preview');
const loadedFonts = new Map();
const defaultFontStack = 'Inter,"Microsoft YaHei","PingFang SC",system-ui,sans-serif';
let ellipsisTimer = null;
const measureCanvas = document.createElement('canvas');
const measureContext = measureCanvas.getContext('2d');

const $ = id => document.getElementById(id);
const escapeHtml = value => String(value ?? '').replace(/[&<>'"]/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[ch]));

function hexToRgb(hex) {
  const value = String(hex || '').replace('#','');
  if (value.length !== 6) return [0,0,0];
  return [parseInt(value.slice(0,2),16),parseInt(value.slice(2,4),16),parseInt(value.slice(4,6),16)];
}
function clamp01(value, fallback = 1) {
  const n = Number(value ?? fallback);
  if (!Number.isFinite(n)) return fallback;
  return Math.max(0, Math.min(1, n));
}
function rgba(hex, alpha) {
  const [r,g,b] = hexToRgb(hex);
  const a = clamp01(alpha);
  return `rgba(${r},${g},${b},${a})`;
}
function textShadowStroke(width, color) {
  const w = Math.max(0, Math.min(8, Math.round(Number(width || 0))));
  if (!w) return 'none';
  const c = color || '#000000';
  const parts = [];
  for (let x = -w; x <= w; x += 1) {
    for (let y = -w; y <= w; y += 1) {
      if (x === 0 && y === 0) continue;
      const d = Math.sqrt(x * x + y * y);
      if (d <= w + 0.35) parts.push(`${x}px ${y}px 0 ${c}`);
    }
  }
  return parts.join(',');
}
function fontFamilyName(file) {
  let hash = 2166136261;
  for (const ch of String(file || '')) {
    hash ^= ch.codePointAt(0);
    hash = Math.imul(hash, 16777619);
  }
  return `BiliQueueFont_${(hash >>> 0).toString(16)}`;
}
function ensureFont(file) {
  const name = String(file || '').trim();
  if (!name) return defaultFontStack;
  const family = fontFamilyName(name);
  if (!loadedFonts.has(name)) {
    const face = new FontFace(family, `url("/fonts/${encodeURIComponent(name)}")`);
    loadedFonts.set(name, face);
    face.load().then(loaded => {
      document.fonts.add(loaded);
      if (state) renderQueueArea();
    }).catch(() => loadedFonts.delete(name));
  }
  return `"${family}",${defaultFontStack}`;
}
function mediaImageURL(raw) {
  const value = String(raw || '').trim();
  if (!value) return '';
  if (value.startsWith('/api/media/image?')) return value;
  return `/api/media/image?url=${encodeURIComponent(value)}`;
}

function strokeText(value, scope, extraClass = '') {
  const text = escapeHtml(value);
  const classes = ['stroke-wrap', scope, extraClass].filter(Boolean).join(' ');
  return `<span class="${classes}">${text}</span>`;
}

function fittingText(value, scope, extraClass = '') {
  const text = escapeHtml(value);
  const classes = ['stroke-wrap', scope, 'manual-ellipsis', extraClass].filter(Boolean).join(' ');
  return `<span class="${classes}" data-full-text="${text}">${text}</span>`;
}

function avatar(user, enabled, area = 'queue') {
  if (!enabled) return '';
  const label = user?.manual ? '⭐️' : (user?.username || '?').slice(0,1);
  const initial = `<span class="avatar-initial${user?.manual ? ' manual-star' : ''}">${escapeHtml(label)}</span>`;
  const cls = `avatar ${area === 'current' ? 'current-avatar' : 'queue-avatar'}`;
  if (user?.avatar) return `<span class="${cls}">${initial}<img src="${escapeHtml(mediaImageURL(user.avatar))}" alt="" onerror="this.remove()"></span>`;
  return `<span class="${cls}">${initial}</span>`;
}
function giftMark(user, enabled, scope = 'stroke-queue') {
  if (!enabled || !user?.priority) return '';
  const fallback = strokeText('◆', scope, 'stroke-gift');
  const icon = user.giftIcon
    ? `${fallback}<img src="${escapeHtml(mediaImageURL(user.giftIcon))}" alt="" onerror="this.remove()">`
    : fallback;
  return `<span class="gift-mark" title="${escapeHtml(user.giftName || '礼物')}">${icon}</span>`;
}
function chip(user, index, settings) {
  return `<div class="user-chip${user.priority ? ' priority' : ''}"><span class="position">${strokeText(String(index).padStart(2,'0'), 'stroke-queue')}</span>${avatar(user,settings.showAvatar,'queue')}${giftMark(user,settings.showGiftIcon,'stroke-queue')}<span class="name">${fittingText(user.username, 'stroke-queue')}</span></div>`;
}

function stopScroll() {
  if (pageTimer) clearInterval(pageTimer);
  if (pageResetTimer) clearTimeout(pageResetTimer);
  pageTimer = null;
  pageResetTimer = null;
  pageIndex = 0;
  offset = 0;
  cycleWidth = 0;
  scrolling = false;
  activeTrack = null;
  activeRow = null;
}

function apply(nextState) {
  state = nextState;
  const o = state.config.overlay;
  const root = document.documentElement.style;
  const currentEnabled = o.currentEnabled !== false;
  const infoEnabled = o.infoEnabled !== false;
  const currentWidth = currentEnabled ? Number(o.currentWidth || 0) : 0;
  const infoWidth = infoEnabled ? Number(o.infoWidth || 0) : 0;
  root.setProperty('--bar-height', `${o.height}px`);
  root.setProperty('--current-font-size', `${o.currentFontSize}px`);
  root.setProperty('--current-text-color', rgba(o.currentTextColor || '#ffffff', o.currentTextOpacity));
  root.setProperty('--current-text-stroke-width', `${Math.max(0, Math.min(12, Number(o.currentTextStrokeWidth || 0)))}px`);
  root.setProperty('--current-text-stroke-color', o.currentTextStrokeColor || '#000000');
  root.setProperty('--current-text-shadow', textShadowStroke(o.currentTextStrokeWidth, o.currentTextStrokeColor));
  root.setProperty('--current-font-weight', String(o.currentFontWeight || 600));
  root.setProperty('--current-text-align', o.currentTextAlign || 'left');
  root.setProperty('--current-font-family', ensureFont(o.currentFontFile));
  root.setProperty('--current-badge-color', o.currentBadgeTextColor || '#ffffff');
  root.setProperty('--current-badge-bg', rgba(o.currentBadgeBackground || '#6577ed', o.currentBadgeOpacity ?? 0.92));
  root.setProperty('--current-badge-font-size', `${Math.max(8, Math.min(28, Number(o.currentBadgeFontSize ?? 11)))}px`);
  root.setProperty('--current-badge-radius', `${Math.max(0, Math.min(28, Number(o.currentBadgeRadius ?? 8)))}px`);
  root.setProperty('--current-badge-offset-x', `${Math.max(-80, Math.min(80, Number(o.currentBadgeOffsetX ?? -6)))}px`);
  root.setProperty('--current-badge-offset-y', `${Math.max(-80, Math.min(80, Number(o.currentBadgeOffsetY ?? -6)))}px`);
  root.setProperty('--queue-font-size', `${o.queueFontSize}px`);
  root.setProperty('--queue-text-color', rgba(o.queueTextColor || '#ffffff', o.queueTextOpacity));
  root.setProperty('--queue-text-stroke-width', `${Math.max(0, Math.min(12, Number(o.queueTextStrokeWidth || 0)))}px`);
  root.setProperty('--queue-text-stroke-color', o.queueTextStrokeColor || '#000000');
  root.setProperty('--queue-text-shadow', textShadowStroke(o.queueTextStrokeWidth, o.queueTextStrokeColor));
  root.setProperty('--queue-font-weight', String(o.queueFontWeight || 500));
  root.setProperty('--queue-font-family', ensureFont(o.queueFontFile));
  root.setProperty('--info-font-size', `${o.infoFontSize}px`);
  root.setProperty('--info-text-color', rgba(o.infoTextColor || '#ffffff', o.infoTextOpacity));
  root.setProperty('--info-text-stroke-width', `${Math.max(0, Math.min(12, Number(o.infoTextStrokeWidth || 0)))}px`);
  root.setProperty('--info-text-stroke-color', o.infoTextStrokeColor || '#000000');
  root.setProperty('--info-text-shadow', textShadowStroke(o.infoTextStrokeWidth, o.infoTextStrokeColor));
  root.setProperty('--info-font-weight', String(o.infoFontWeight || 500));
  root.setProperty('--info-text-align', o.infoTextAlign || 'left');
  root.setProperty('--info-font-family', ensureFont(o.infoFontFile));
  root.setProperty('--global-bg-color', rgba(o.background || '#000000', 0));
  root.setProperty('--mask-top', String(clamp01(o.gradientTopOpacity)));
  root.setProperty('--mask-bottom', String(clamp01(o.gradientBottomOpacity)));
  const gradientStart = Math.max(0, Math.min(100, Number(o.gradientStart ?? Math.max(0, 100 - Number(o.gradientRange || 100)))));
  const gradientEnd = Math.max(gradientStart, Math.min(100, Number(o.gradientEnd ?? 100)));
  root.setProperty('--mask-start', `${gradientStart}%`);
  root.setProperty('--mask-end', `${gradientEnd}%`);
  root.setProperty('--avatar-size', `${Math.max(12, Math.min(96, Number(o.avatarSize || 32)))}px`);
  root.setProperty('--current-avatar-size', `${Math.max(12, Math.min(96, Number(o.currentAvatarSize ?? o.avatarSize ?? 32)))}px`);
  root.setProperty('--queue-avatar-size', `${Math.max(12, Math.min(96, Number(o.queueAvatarSize ?? o.avatarSize ?? 32)))}px`);
  root.setProperty('--current-avatar-name-gap', `${Math.max(0, Math.min(80, Number(o.currentAvatarNameGap ?? 12)))}px`);
  root.setProperty('--queue-avatar-name-gap', `${Math.max(0, Math.min(80, Number(o.queueAvatarNameGap ?? 10)))}px`);
  root.setProperty('--current-bg', rgba(o.currentBackground || '#ffffff', o.currentBackgroundOpacity));
  root.setProperty('--queue-bg', rgba(o.queueBackground || '#000000', o.queueBackgroundOpacity));
  root.setProperty('--info-bg', rgba(o.infoBackground || '#ffffff', o.infoBackgroundOpacity));
  const edgeAlpha = Math.max(Number(o.queueBackgroundOpacity || 0), (Number(o.gradientTopOpacity || 0) + Number(o.gradientBottomOpacity || 0)) / 2);
  root.setProperty('--queue-edge', rgba(Number(o.queueBackgroundOpacity || 0) > 0 ? o.queueBackground : o.background, edgeAlpha));
  root.setProperty('--radius', `${o.radius}px`);
  root.setProperty('--current-width', `${currentWidth}px`);
  root.setProperty('--current-side-padding', `${Math.max(0, Math.min(120, Number(o.currentSidePadding ?? 20)))}px`);
  root.setProperty('--queue-width', `${o.queueWidth}px`);
  root.setProperty('--info-width', `${infoWidth}px`);
  root.setProperty('--queue-line-gap', `${o.queueLineGap}px`);
  root.setProperty('--queue-item-gap', `${Math.max(0, Number(o.queueItemGap ?? 22))}px`);
  root.setProperty('--info-line-gap', `${o.infoLineGap}px`);

  $('current').style.display = currentEnabled ? 'flex' : 'none';
  document.querySelector('.bg-current')?.classList.toggle('hidden-area', !currentEnabled);
  document.querySelector('.bg-info')?.classList.toggle('hidden-area', !infoEnabled);

  const current = state.queue[0];
  if (!currentEnabled) {
    $('current').innerHTML = '';
  } else if (current) {
    const currentAvatar = avatar(current,o.showAvatar,'current');
    const badgeText = String(o.currentBadgeText || '当前').trim();
    const avatarWithBadge = currentAvatar ? `<span class="current-avatar-stack">${currentAvatar}${badgeText ? `<span class="current-badge">${escapeHtml(badgeText)}</span>` : ''}</span>` : '';
    const media = `${avatarWithBadge}${giftMark(current,o.showGiftIcon,'stroke-current')}`;
    $('current').innerHTML = `<div class="current-user${media ? '' : ' no-media'}">${media ? `<div class="current-media">${media}</div>` : ''}<strong class="current-name">${fittingText(current.username, 'stroke-current')}</strong></div>`;
  } else {
    $('current').innerHTML = `<div class="placeholder">${strokeText(o.emptyText || '排队空闲中', 'stroke-current', 'stroke-block')}</div>`;
  }
  const infoLines = [];
  if (infoEnabled && state.config.queueEnabled !== false && o.showCount) infoLines.push(`<div class="info-line">${strokeText(state.paused ? `当前队列：${state.queue.length} 人（暂停）` : `当前队列：${state.queue.length} 人`, 'stroke-info', 'stroke-block')}</div>`);
  if (infoEnabled && o.showRules && String(o.infoText || '').length) infoLines.push(`<div class="info-line info-custom">${strokeText(o.infoText, 'stroke-info', 'stroke-block')}</div>`);
  $('info').style.display = infoEnabled && infoLines.length ? 'flex' : 'none';
  $('info').innerHTML = `<div class="info-copy">${infoLines.join('')}</div>`;

  scheduleManualEllipsis();
  updatePreviewScale();
  const nextQueueSignature = queueRenderSignature(state);
  if (nextQueueSignature !== lastQueueSignature || !activeTrack) {
    lastQueueSignature = nextQueueSignature;
    renderQueueArea();
  }
}

function queueRenderSignature(nextState) {
  const o = nextState.config.overlay || {};
  const waiting = (nextState.queue || []).slice(1).map(user => [
    user.id || '', user.uid || 0, user.username || '', user.avatar || '',
    user.priority ? 1 : 0, user.giftName || '', user.giftIcon || '', user.giftBattery || 0
  ]);
  const emptyText = waiting.length ? '' : (o.queueEmptyText || '');
  const keys = {
    waiting, emptyText,
    queueWidth: o.queueWidth, height: o.height, currentEnabled: o.currentEnabled !== false, infoEnabled: o.infoEnabled !== false, queueFontSize: o.queueFontSize, queueFontWeight: o.queueFontWeight, queueFontFile: o.queueFontFile,
    showAvatar: o.showAvatar, showGiftIcon: o.showGiftIcon, avatarSize: o.avatarSize, currentAvatarSize: o.currentAvatarSize, queueAvatarSize: o.queueAvatarSize, currentAvatarNameGap: o.currentAvatarNameGap, queueAvatarNameGap: o.queueAvatarNameGap,
    queueItemGap: o.queueItemGap, queueLineGap: o.queueLineGap, queuePageSize: o.queuePageSize ?? o.queueSecondPageSize,
    doubleLineEnabled: o.doubleLineEnabled, scrollMode: o.scrollMode, speed: o.speed, effectInterval: o.effectInterval, effectDuration: o.effectDuration,
    shortAlign: o.shortAlign
  };
  return JSON.stringify(keys);
}

function updatePreviewScale() {
  const bar = $('bar');
  if (!isPreview || !state) {
    bar.style.transform = '';
    bar.style.transformOrigin = '';
    return;
  }
  const o = state.config.overlay;
  const totalWidth = (o.currentEnabled === false ? 0 : Number(o.currentWidth || 0)) + Number(o.queueWidth || 0) + (o.infoEnabled === false ? 0 : Number(o.infoWidth || 0));
  const widthScale = totalWidth > 0 ? window.innerWidth / totalWidth : 1;
  const heightScale = Number(o.height || 0) > 0 ? window.innerHeight / Number(o.height) : 1;
  const scale = Math.min(1, widthScale, heightScale);
  bar.style.transformOrigin = 'left center';
  bar.style.transform = `scale(${scale})`;
}

function renderQueueArea() {
  stopScroll();
  const token = ++renderToken;
  const o = state.config.overlay;
  const waiting = state.queue.slice(1);
  const pageSize = normalizePageSize(o.queuePageSize ?? o.queueSecondPageSize ?? 5);
  const doubleEnabled = o.doubleLineEnabled !== false;
  const isDouble = doubleEnabled && waiting.length > pageSize;
  const doubleModeChanged = lastDoubleMode !== isDouble;
  lastDoubleMode = isDouble;

  $('singleRow').style.display = isDouble ? 'none' : 'flex';
  $('doubleRows').style.display = isDouble ? 'flex' : 'none';

  if (!waiting.length) {
    if (isDouble) {
      $('fixedRow').innerHTML = '';
      prepareStatic($('scrollTrack'), `<span class="queue-empty">${strokeText(o.queueEmptyText || '空', 'stroke-queue', 'stroke-block')}</span>`, o.shortAlign);
    } else {
      prepareStatic($('singleTrack'), `<span class="queue-empty">${strokeText(o.queueEmptyText || '空', 'stroke-queue', 'stroke-block')}</span>`, o.shortAlign);
    }
    return;
  }

  if (!isDouble) {
    prepareAnimatedRow($('singleTrack'), $('singleRow'), waiting, 2, o, token);
    return;
  }

  const renderNow = () => renderDoubleRows(waiting, o, pageSize, token);
  if (doubleModeChanged) {
    afterLayoutReady(() => {
      if (token === renderToken) renderNow();
    });
    return;
  }

  renderNow();
  if (needsLayoutRetry($('fixedRow'), o) || needsLayoutRetry($('scrollRow'), o)) {
    afterLayoutReady(() => {
      if (token === renderToken) renderNow();
    });
  }
}


function renderDoubleRows(waiting, settings, pageSize, token) {
  if (token !== renderToken) return;
  const pages = buildAlignedPages($('fixedRow'), waiting, 2, settings, pageSize);
  $('fixedRow').innerHTML = pages[0] || '';
  scheduleManualEllipsis();
  const secondPages = pages.slice(1);
  if (!secondPages.length) {
    $('scrollTrack').className = 'track';
    $('scrollTrack').style.transition = 'none';
    $('scrollTrack').style.transform = 'translate3d(0,0,0)';
    $('scrollTrack').style.opacity = '1';
    $('scrollTrack').innerHTML = '';
    return;
  }
  activeTrack = $('scrollTrack');
  activeRow = $('scrollRow');
  if (needsLayoutRetry($('scrollRow'), settings)) {
    afterLayoutReady(() => {
      if (token === renderToken) renderDoubleRows(waiting, settings, pageSize, token);
    });
    return;
  }
  if (settings.scrollMode === 'continuous') {
    prepareContinuousGridRow($('scrollTrack'), $('scrollRow'), waiting.slice(pageSize), 2 + pageSize, settings, token);
  } else if (settings.scrollMode === 'fade') setupFadePages($('scrollTrack'), secondPages, settings);
  else setupVerticalPages($('scrollTrack'), secondPages, settings);
}

function prepareStatic(track, html, align) {
  track.parentElement?.classList.remove('page-mask', 'double-page-mask');
  track.className = `track ${align === 'left' ? 'short-left' : align === 'right' ? 'short-right' : 'short-center'}`;
  track.style.transition = 'none';
  track.style.transform = 'translate3d(0,0,0)';
  track.style.opacity = '1';
  track.innerHTML = `<div class="copy">${html}</div>`;
  scheduleManualEllipsis();
}

function prepareAnimatedRow(track, row, users, startIndex, settings, token) {
  activeTrack = track;
  activeRow = row;
  row?.classList.remove('page-mask', 'double-page-mask');
  track.style.transition = 'none';
  track.style.transform = 'translate3d(0,0,0)';
  track.style.opacity = '1';

  if (settings.scrollMode === 'fade' || settings.scrollMode === 'paged') {
    renderPagedRow(track, row, users, startIndex, settings, token);
    if (needsLayoutRetry(row, settings)) {
      afterLayoutReady(() => {
        if (activeTrack === track && token === renderToken) renderPagedRow(track, row, users, startIndex, settings, token);
      });
    }
    return;
  }

  prepareContinuousGridRow(track, row, users, startIndex, settings, token);
  if (needsLayoutRetry(row, settings)) {
    afterLayoutReady(() => {
      if (activeTrack === track && token === renderToken) prepareContinuousGridRow(track, row, users, startIndex, settings, token);
    });
  }
}

function continuousGridMetrics(row, settings) {
  const columns = normalizePageSize(settings.queuePageSize ?? settings.queueSecondPageSize ?? 5);
  const gap = Math.max(0, Number(settings.queueItemGap ?? 22));
  const rowWidth = getUsableRowWidth(row, settings);
  const rawCellWidth = (rowWidth - gap * (columns + 1)) / columns;
  const cellWidth = Math.max(1, Math.floor(rawCellWidth));
  return { columns, gap, rowWidth, cellWidth };
}

function buildContinuousGridCopy(users, startIndex, settings, metrics, hidden = false) {
  const align = queueAlignClass(settings.shortAlign);
  const items = users.map((user, index) => chip(user, startIndex + index, settings)).join('');
  const attrs = hidden ? ' aria-hidden="true"' : '';
  return `<div class="copy continuous-copy ${align}"${attrs}><div class="continuous-grid" style="--queue-columns:${metrics.columns};--queue-grid-gap:${metrics.gap}px;--queue-cell-width:${metrics.cellWidth}px;">${items}</div></div>`;
}

function prepareContinuousGridRow(track, row, users, startIndex, settings, token) {
  row?.classList.remove('page-mask', 'double-page-mask');
  if (activeTrack !== track || token !== renderToken) return;
  if (!users.length) {
    track.className = 'track';
    track.innerHTML = '';
    cycleWidth = 0;
    scrolling = false;
    return;
  }
  const metrics = continuousGridMetrics(row, settings);
  const html = buildContinuousGridCopy(users, startIndex, settings, metrics, false);
  track.className = 'track continuous-track';
  track.style.transition = 'none';
  track.style.transform = 'translate3d(0,0,0)';
  track.style.opacity = '1';
  track.innerHTML = html;
  scheduleManualEllipsis();

  afterLayoutReady(() => {
    if (activeTrack !== track || token !== renderToken) return;
    const first = track.querySelector('.continuous-copy');
    cycleWidth = first ? Math.ceil(first.scrollWidth || first.getBoundingClientRect().width || 0) : 0;
    scrolling = cycleWidth > metrics.rowWidth;
    if (!scrolling) {
      track.classList.add(settings.shortAlign === 'left' ? 'short-left' : settings.shortAlign === 'right' ? 'short-right' : 'short-center');
      scheduleManualEllipsis();
      return;
    }
    track.innerHTML = html + buildContinuousGridCopy(users, startIndex, settings, metrics, true);
    scheduleManualEllipsis();
  });
}

function renderPagedRow(track, row, users, startIndex, settings, token) {
  if (activeTrack !== track || token !== renderToken) return;
  const pageLimit = normalizePageSize(settings.queuePageSize ?? settings.queueSecondPageSize ?? 5);
  const pages = buildFittingPages(row, users, startIndex, settings, pageLimit);
  if (settings.scrollMode === 'fade') setupFadePages(track, pages, settings);
  else setupVerticalPages(track, pages, settings);
}

function normalizePageSize(value) {
  return Math.max(1, Math.min(20, Number(value || 5)));
}

function afterLayoutReady(callback) {
  requestAnimationFrame(() => {
    requestAnimationFrame(() => callback());
  });
}

function queueAlignClass(align) {
  if (align === 'left') return 'align-left';
  if (align === 'right') return 'align-right';
  return 'align-center';
}

function getUsableRowWidth(row, settings) {
  const raw = Math.max(
    Number(row?.clientWidth || 0),
    Number(row?.getBoundingClientRect?.().width || 0),
    Number(settings?.queueWidth || 0)
  );
  return Math.max(1, Math.round(raw));
}

function needsLayoutRetry(row, settings) {
  const measured = Math.max(
    Number(row?.clientWidth || 0),
    Number(row?.getBoundingClientRect?.().width || 0)
  );
  const expected = Number(settings?.queueWidth || 0);
  if (measured <= 1) return true;
  if (expected > 0 && measured < expected * 0.75) return true;
  return false;
}

function measureChipWidths(row, users, startIndex, settings) {
  const probe = document.createElement('div');
  probe.className = 'measure-copy';
  row.appendChild(probe);
  const widths = users.map((user, index) => {
    probe.innerHTML = chip(user, startIndex + index, settings);
    const chipEl = probe.querySelector('.user-chip');
    if (!chipEl) return 1;
    const bounds = Math.ceil(chipEl.getBoundingClientRect().width || 0);
    const scroll = Math.ceil(chipEl.scrollWidth || 0);
    return Math.max(bounds, scroll, 1);
  });
  probe.remove();
  return widths.map(width => Math.max(1, width));
}

function buildAlignedPages(row, users, startIndex, settings, maxPerPage) {
  const columns = normalizePageSize(maxPerPage);
  const gap = Math.max(0, Number(settings.queueItemGap ?? 22));
  const align = queueAlignClass(settings.shortAlign);
  const pages = [];
  for (let i = 0; i < users.length; i += columns) {
    const list = users.slice(i, i + columns);
    const items = list.map((user, localIndex) => chip(user, startIndex + i + localIndex, settings)).join('');
    pages.push(`<div class="page-copy copy ${align}"><div class="aligned-grid fixed-columns" style="--queue-columns:${columns};--queue-grid-gap:${gap}px;">${items}</div></div>`);
  }
  return pages;
}

function buildFittingPages(row, users, startIndex, settings, maxPerPage) {
  return buildAlignedPages(row, users, startIndex, settings, maxPerPage);
}

function setVerticalPageActive(track, index, extraIndexes = []) {
  const activeIndexes = new Set([index, ...extraIndexes]);
  const nodes = [...track.children];
  nodes.forEach((node, i) => node.classList.toggle('active', activeIndexes.has(i)));
}

function setupVerticalPages(track, pages, settings) {
  const pageMaskRow = track.parentElement;
  if (pageTimer) clearInterval(pageTimer);
  if (pageResetTimer) clearTimeout(pageResetTimer);
  pageTimer = null;
  pageResetTimer = null;
  pageIndex = 0;
  track.style.transition = 'none';
  track.style.transform = 'translate3d(0,0,0)';
  if (pages.length <= 1) {
    pageMaskRow?.classList.remove('page-mask', 'double-page-mask');
    track.className = `track ${settings.shortAlign === 'left' ? 'short-left' : settings.shortAlign === 'right' ? 'short-right' : 'short-center'}`;
    track.innerHTML = pages[0] || '';
    scheduleManualEllipsis();
    return;
  }
  pageMaskRow?.classList.add('page-mask');
  pageMaskRow?.classList.toggle('double-page-mask', pageMaskRow?.id === 'scrollRow');
  const duration = Math.max(0.1, Number(settings.effectDuration || 0.42));
  const interval = Math.max(duration + 0.1, Number(settings.effectInterval || 4));
  const loopPages = pages.concat(pages[0]);
  track.className = 'track page-stack vertical-pages slide-fade-pages';
  track.innerHTML = loopPages.join('');
  track.querySelectorAll('.page-copy').forEach(page => { page.style.transitionDuration = `${duration}s`; });
  setVerticalPageActive(track, 0);
  scheduleManualEllipsis();
  pageTimer = setInterval(() => {
    if (pageResetTimer) {
      clearTimeout(pageResetTimer);
      pageResetTimer = null;
    }
    pageIndex += 1;
    const loopingToFirst = pageIndex >= pages.length;
    if (loopingToFirst) {
      track.classList.add('no-page-fade');
      setVerticalPageActive(track, pageIndex, [pageIndex - 1, 0]);
    } else {
      track.classList.remove('no-page-fade');
      setVerticalPageActive(track, pageIndex);
    }
    track.style.transition = `transform ${duration}s ease`;
    track.style.transform = `translate3d(0, ${-pageIndex * 100}%, 0)`;
    if (loopingToFirst) {
      pageResetTimer = setTimeout(() => {
        track.classList.add('no-page-fade');
        track.style.transition = 'none';
        track.style.transform = 'translate3d(0,0,0)';
        pageIndex = 0;
        setVerticalPageActive(track, 0, [track.children.length - 1]);
        void track.offsetHeight;
        requestAnimationFrame(() => {
          setVerticalPageActive(track, 0);
          track.classList.remove('no-page-fade');
        });
        pageResetTimer = null;
      }, duration * 1000 + 40);
    }
  }, interval * 1000);
}

function setupFadePages(track, pages, settings) {
  track.parentElement?.classList.remove('page-mask', 'double-page-mask');
  if (pageTimer) clearInterval(pageTimer);
  pageTimer = null;
  pageIndex = 0;
  track.style.transition = 'none';
  track.style.transform = 'translate3d(0,0,0)';
  if (pages.length <= 1) {
    track.className = `track ${settings.shortAlign === 'left' ? 'short-left' : settings.shortAlign === 'right' ? 'short-right' : 'short-center'}`;
    track.innerHTML = pages[0] || '';
    scheduleManualEllipsis();
    return;
  }
  const duration = Math.max(0.1, Number(settings.effectDuration || 0.42));
  const interval = Math.max(duration + 0.1, Number(settings.effectInterval || 4));
  track.className = 'track fade-stack';
  track.innerHTML = pages.map((page, index) => `<div class="fade-page${index === 0 ? ' active' : ''}">${page}</div>`).join('');
  scheduleManualEllipsis();
  track.querySelectorAll('.fade-page').forEach(page => { page.style.transitionDuration = `${duration}s`; });
  pageTimer = setInterval(() => {
    const pagesNodes = [...track.querySelectorAll('.fade-page')];
    pagesNodes[pageIndex]?.classList.remove('active');
    pageIndex = (pageIndex + 1) % pagesNodes.length;
    pagesNodes[pageIndex]?.classList.add('active');
  }, interval * 1000);
}

function setupHorizontalPaged(track, rowWidth, settings) {
  track.style.transition = 'none';
  track.style.transform = 'translate3d(0,0,0)';
  const duration = Math.max(0.1, Number(settings.effectDuration || 0.42));
  const interval = Math.max(duration + 0.1, Number(settings.effectInterval || 4));
  const step = Math.max(220, rowWidth * .86);
  const pages = Math.max(1, Math.ceil(cycleWidth / step));
  pageTimer = setInterval(() => {
    pageIndex = (pageIndex + 1) % pages;
    track.style.transition = `transform ${duration}s ease`;
    track.style.transform = `translate3d(${-Math.min(pageIndex * step, Math.max(0, cycleWidth - rowWidth))}px,0,0)`;
  }, interval * 1000);
}


function scheduleManualEllipsis() {
  if (ellipsisTimer) cancelAnimationFrame(ellipsisTimer);
  ellipsisTimer = requestAnimationFrame(() => {
    ellipsisTimer = null;
    requestAnimationFrame(() => fitManualEllipsis());
  });
}

function parsePx(value) {
  const n = Number(String(value || '').replace('px', ''));
  return Number.isFinite(n) ? n : 0;
}

function elementFont(style) {
  if (style.font && style.font !== '') return style.font;
  return `${style.fontStyle || 'normal'} ${style.fontVariant || 'normal'} ${style.fontWeight || '400'} ${style.fontSize || '16px'} ${style.fontFamily || defaultFontStack}`;
}

function textWidth(text, font) {
  if (!measureContext) return String(text || '').length * 12;
  measureContext.font = font;
  return measureContext.measureText(String(text || '')).width;
}

function truncateToWidth(text, maxWidth, font) {
  const value = String(text || '');
  if (maxWidth <= 0) return '';
  if (textWidth(value, font) <= maxWidth) return value;
  const dots = '...';
  const dotsWidth = textWidth(dots, font);
  if (dotsWidth >= maxWidth) return dots;
  let lo = 0;
  let hi = value.length;
  while (lo < hi) {
    const mid = Math.ceil((lo + hi) / 2);
    if (textWidth(value.slice(0, mid) + dots, font) <= maxWidth) lo = mid;
    else hi = mid - 1;
  }
  return value.slice(0, lo) + dots;
}

function currentNameAvailableWidth(box, strokeWidth) {
  const current = $('current');
  if (!current) return 0;
  const currentStyle = getComputedStyle(current);
  const contentWidth = Math.max(0,
    (current.clientWidth || 0) - parsePx(currentStyle.paddingLeft) - parsePx(currentStyle.paddingRight)
  );
  const user = box.closest('.current-user');
  const media = user?.querySelector('.current-media');
  const userStyle = user ? getComputedStyle(user) : null;
  const mediaWidth = media ? Math.max(media.getBoundingClientRect().width || 0, media.scrollWidth || 0) : 0;
  const gap = media ? parsePx(userStyle?.columnGap || getComputedStyle(document.documentElement).getPropertyValue('--current-avatar-name-gap')) : 0;
  const boxStyle = getComputedStyle(box);
  const horizontalPadding = parsePx(boxStyle.paddingLeft) + parsePx(boxStyle.paddingRight);
  const safeInset = Math.ceil(strokeWidth * 2 + 2);
  return Math.max(0, contentWidth - mediaWidth - gap - horizontalPadding - safeInset);
}

function fitManualEllipsis() {
  const nodes = document.querySelectorAll('.manual-ellipsis[data-full-text]');
  nodes.forEach(node => {
    const full = node.dataset.fullText || '';
    const box = node.closest('.name, .current-name') || node.parentElement;
    if (!box) {
      node.textContent = full;
      return;
    }
    node.textContent = full;
    const style = getComputedStyle(node);
    const boxStyle = getComputedStyle(box);
    const strokeWidth = parsePx(style.getPropertyValue('--stroke-width')) || parsePx(boxStyle.getPropertyValue('--stroke-width')) || 0;
    const horizontalPadding = parsePx(boxStyle.paddingLeft) + parsePx(boxStyle.paddingRight);
    const safeInset = Math.ceil(strokeWidth * 2 + 2);
    let available;
    if (box.classList.contains('current-name')) {
      available = currentNameAvailableWidth(box, strokeWidth);
    } else {
      const rawWidth = Math.max(box.clientWidth || 0, box.getBoundingClientRect?.().width || 0);
      available = Math.max(0, rawWidth - horizontalPadding - safeInset);
    }
    const font = elementFont(style);
    node.textContent = truncateToWidth(full, available, font);
  });
}

function animate(now) {
  const dt = Math.min(.1, (now - lastTime) / 1000);
  lastTime = now;
  if (state && activeTrack && scrolling && state.config.overlay.scrollMode === 'continuous' && cycleWidth > 0) {
    offset -= Number(state.config.overlay.speed || 0) * dt;
    if (-offset >= cycleWidth) offset += cycleWidth;
    activeTrack.style.transform = `translate3d(${offset}px,0,0)`;
  }
  requestAnimationFrame(animate);
}

fetch('/api/state').then(r => r.json()).then(apply);
const source = new EventSource('/events');
source.onmessage = event => apply(JSON.parse(event.data));
window.addEventListener('resize', () => {
  updatePreviewScale();
  if (state) renderQueueArea();
  scheduleManualEllipsis();
});
if (document.fonts?.ready) {
  document.fonts.ready.then(() => { if (state) renderQueueArea(); }).catch(() => {});
}
requestAnimationFrame(animate);
