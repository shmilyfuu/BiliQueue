'use strict';

let state = null;
let activeTrack = null;
let activeRow = null;
let offset = 0;
let lastTime = performance.now();
let cycleWidth = 0;
let scrolling = false;
let pageTimer = null;
let pageIndex = 0;
const isPreview = new URLSearchParams(location.search).has('preview');

const $ = id => document.getElementById(id);
const escapeHtml = value => String(value ?? '').replace(/[&<>'"]/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[ch]));

function hexToRgb(hex) {
  const value = String(hex || '').replace('#','');
  if (value.length !== 6) return [0,0,0];
  return [parseInt(value.slice(0,2),16),parseInt(value.slice(2,4),16),parseInt(value.slice(4,6),16)];
}
function mediaImageURL(raw) {
  const value = String(raw || '').trim();
  if (!value) return '';
  if (value.startsWith('/api/media/image?')) return value;
  return `/api/media/image?url=${encodeURIComponent(value)}`;
}

function avatar(user, enabled) {
  if (!enabled) return '';
  const initial = escapeHtml((user?.username || '?').slice(0,1));
  if (user?.avatar) return `<span class="avatar">${initial}<img src="${escapeHtml(mediaImageURL(user.avatar))}" alt="" onerror="this.remove()"></span>`;
  return `<span class="avatar">${initial}</span>`;
}
function giftMark(user, enabled) {
  if (!enabled || !user?.priority) return '';
  const icon = user.giftIcon
    ? `◆<img src="${escapeHtml(mediaImageURL(user.giftIcon))}" alt="" onerror="this.remove()">`
    : '◆';
  return `<span class="gift-mark" title="${escapeHtml(user.giftName || '礼物')}">${icon}</span>`;
}
function chip(user, index, settings) {
  return `<div class="user-chip${user.priority ? ' priority' : ''}"><span class="position">${String(index).padStart(2,'0')}</span>${avatar(user,settings.showAvatar)}${giftMark(user,settings.showGiftIcon)}<span class="name">${escapeHtml(user.username)}</span></div>`;
}

function stopScroll() {
  if (pageTimer) clearInterval(pageTimer);
  pageTimer = null;
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
  const [r,g,b] = hexToRgb(o.background || '#000000');
  const root = document.documentElement.style;
  root.setProperty('--bar-height', `${o.height}px`);
  root.setProperty('--current-font-size', `${o.currentFontSize}px`);
  root.setProperty('--current-text-color', o.currentTextColor || '#ffffff');
  root.setProperty('--current-font-weight', String(o.currentFontWeight || 600));
  root.setProperty('--current-text-align', o.currentTextAlign || 'left');
  root.setProperty('--queue-font-size', `${o.queueFontSize}px`);
  root.setProperty('--queue-text-color', o.queueTextColor || '#ffffff');
  root.setProperty('--queue-font-weight', String(o.queueFontWeight || 500));
  root.setProperty('--info-font-size', `${o.infoFontSize}px`);
  root.setProperty('--info-text-color', o.infoTextColor || '#ffffff');
  root.setProperty('--info-font-weight', String(o.infoFontWeight || 500));
  root.setProperty('--info-text-align', o.infoTextAlign || 'left');
  root.setProperty('--bar-bg', `rgba(${r},${g},${b},${o.opacity})`);
  root.setProperty('--radius', `${o.radius}px`);
  root.setProperty('--current-width', `${o.currentWidth}px`);
  root.setProperty('--queue-width', `${o.queueWidth}px`);
  root.setProperty('--info-width', `${o.infoWidth}px`);
  root.setProperty('--queue-line-gap', `${o.queueLineGap}px`);
  root.setProperty('--info-line-gap', `${o.infoLineGap}px`);

  const current = state.queue[0];
  $('current').innerHTML = current
    ? `<div class="current-user">${avatar(current,o.showAvatar)}${giftMark(current,o.showGiftIcon)}<div class="current-copy"><small>当前</small><strong>${escapeHtml(current.username)}</strong></div></div>`
    : `<div class="placeholder">${escapeHtml(o.emptyText || '排队空闲中')}</div>`;

  const infoLines = [];
  if (o.showCount) infoLines.push(`<div class="info-line">${state.paused ? `当前队列：${state.queue.length} 人（暂停）` : `当前队列：${state.queue.length} 人`}</div>`);
  if (o.showRules && String(o.infoText || '').length) infoLines.push(`<div class="info-line info-custom">${escapeHtml(o.infoText)}</div>`);
  $('info').style.display = infoLines.length ? 'flex' : 'none';
  $('info').innerHTML = `<div class="info-copy">${infoLines.join('')}</div>`;

  updatePreviewScale();
  renderQueueArea();
}

function updatePreviewScale() {
  const bar = $('bar');
  if (!isPreview || !state) {
    bar.style.transform = '';
    bar.style.transformOrigin = '';
    return;
  }
  const o = state.config.overlay;
  const totalWidth = Number(o.currentWidth || 0) + Number(o.queueWidth || 0) + Number(o.infoWidth || 0);
  const scale = totalWidth > 0 ? Math.min(1, window.innerWidth / totalWidth) : 1;
  bar.style.transformOrigin = 'left center';
  bar.style.transform = `scale(${scale})`;
}

function renderQueueArea() {
  stopScroll();
  const o = state.config.overlay;
  const waiting = state.queue.slice(1);
  const threshold = Math.max(1, Number(o.doubleLineThreshold || 8));
  const isDouble = waiting.length > threshold;

  $('singleRow').style.display = isDouble ? 'none' : 'flex';
  $('doubleRows').style.display = isDouble ? 'flex' : 'none';

  if (!isDouble) {
    const html = waiting.length ? waiting.map((user,i) => chip(user,i+2,o)).join('') : `<span class="queue-empty">${escapeHtml(o.queueEmptyText || '空')}</span>`;
    prepareTrack($('singleTrack'), $('singleRow'), html, waiting.length > 0, o);
    return;
  }

  const firstLine = waiting.slice(0, threshold);
  const secondLine = waiting.slice(threshold);
  $('fixedRow').style.gridTemplateColumns = `repeat(${Math.max(1, firstLine.length)}, minmax(0, 1fr))`;
  $('fixedRow').innerHTML = firstLine.map((user,i) => chip(user,i+2,o)).join('');
  const secondHTML = secondLine.length ? secondLine.map((user,i) => chip(user,i+2+threshold,o)).join('') : `<span class="queue-empty">${escapeHtml(o.queueEmptyText || '空')}</span>`;
  prepareTrack($('scrollTrack'), $('scrollRow'), secondHTML, secondLine.length > 0, o);
}

function prepareTrack(track, row, html, hasUsers, settings) {
  activeTrack = track;
  activeRow = row;
  track.className = 'track';
  track.style.transition = 'none';
  track.style.transform = 'translate3d(0,0,0)';
  track.innerHTML = `<div class="copy">${html}</div>`;

  requestAnimationFrame(() => {
    if (activeTrack !== track) return;
    const rowWidth = row.clientWidth;
    const first = track.querySelector('.copy');
    cycleWidth = first ? first.scrollWidth : 0;
    scrolling = hasUsers && cycleWidth > rowWidth;
    if (!scrolling) {
      track.classList.add(settings.shortAlign === 'left' ? 'short-left' : 'short-center');
      return;
    }
    if (settings.scrollMode === 'paged') setupPaged(track, rowWidth);
    else track.innerHTML += `<div class="copy" aria-hidden="true">${html}</div>`;
  });
}

function setupPaged(track, rowWidth) {
  const step = Math.max(220, rowWidth * .86);
  const pages = Math.max(1, Math.ceil(cycleWidth / step));
  pageTimer = setInterval(() => {
    pageIndex = (pageIndex + 1) % pages;
    track.style.transition = 'transform 420ms ease';
    track.style.transform = `translate3d(${-Math.min(pageIndex * step, Math.max(0, cycleWidth - rowWidth))}px,0,0)`;
  }, 4000);
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
window.addEventListener('resize', updatePreviewScale);
requestAnimationFrame(animate);
