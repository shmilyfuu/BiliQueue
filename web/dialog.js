'use strict';

(() => {
  let activeResolve = null;
  let previousFocus = null;

  const get = id => document.getElementById(id);

  function closeDialog(result) {
    const modal = get('confirmModal');
    if (!modal || modal.classList.contains('hidden')) return;
    modal.classList.add('hidden');
    const resolve = activeResolve;
    activeResolve = null;
    if (previousFocus?.isConnected) previousFocus.focus({preventScroll:true});
    previousFocus = null;
    resolve?.(result);
  }

  function bindDialog() {
    const modal = get('confirmModal');
    if (!modal || modal.dataset.bound === 'true') return;
    modal.dataset.bound = 'true';
    get('confirmCloseBtn')?.addEventListener('click', () => closeDialog(false));
    get('confirmCancelBtn')?.addEventListener('click', () => closeDialog(false));
    get('confirmAcceptBtn')?.addEventListener('click', () => closeDialog(true));
    modal.addEventListener('click', event => {
      if (event.target === modal) closeDialog(false);
    });
    document.addEventListener('keydown', event => {
      if (event.key === 'Escape' && !modal.classList.contains('hidden')) {
        event.preventDefault();
        closeDialog(false);
      }
    });
  }

  window.showAppConfirm = options => {
    bindDialog();
    const modal = get('confirmModal');
    if (!modal) return Promise.resolve(false);
    if (activeResolve) activeResolve(false);
    previousFocus = document.activeElement;
    get('confirmTitle').textContent = options?.title || '确认操作';
    get('confirmMessage').textContent = options?.message || '';
    const accept = get('confirmAcceptBtn');
    accept.textContent = options?.confirmText || '确定';
    accept.classList.toggle('danger', Boolean(options?.danger));
    accept.classList.toggle('primary', !options?.danger);
    modal.classList.remove('hidden');
    requestAnimationFrame(() => accept.focus({preventScroll:true}));
    return new Promise(resolve => { activeResolve = resolve; });
  };
})();
