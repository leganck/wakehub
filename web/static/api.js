/** Shared DOM helpers, API client, toast. */
(function (global) {
  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];

  function escapeHtml(s) {
    return String(s ?? '').replace(
      /[&<>"']/g,
      (c) =>
        ({
          '&': '&amp;',
          '<': '&lt;',
          '>': '&gt;',
          '"': '&quot;',
          "'": '&#39;',
        })[c]
    );
  }

  function ensureToastHost() {
    let host = $('#toastHost');
    if (!host) {
      host = document.createElement('div');
      host.id = 'toastHost';
      host.className = 'toast-host';
      host.setAttribute('aria-live', 'polite');
      document.body.appendChild(host);
    }
    return host;
  }

  function toast(message, kind = 'info') {
    const host = ensureToastHost();
    const el = document.createElement('div');
    el.className = 'toast toast-' + kind;
    el.textContent = message;
    host.appendChild(el);
    setTimeout(() => {
      el.classList.add('toast-out');
      setTimeout(() => el.remove(), 300);
    }, 3200);
  }

  async function api(path, opts = {}) {
    const res = await fetch(path, {
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) },
      ...opts,
    });
    if (res.status === 401) {
      toast('需要登录（HTTP Basic）', 'error');
      throw new Error('需要登录（HTTP Basic 认证）。请刷新页面并输入用户名/密码。');
    }
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      const msg = data.error || res.statusText;
      toast(msg, 'error');
      throw new Error(msg);
    }
    return data;
  }

  /** Poll device until lastEvent changes or timeout. */
  async function pollDeviceEvent(deviceId, { timeoutMs = 8000, intervalMs = 600 } = {}) {
    const deadline = Date.now() + timeoutMs;
    let last = '';
    while (Date.now() < deadline) {
      try {
        const d = await api('/api/devices/' + deviceId);
        if (d.clientLastEvent && d.clientLastEvent !== last) {
          if (last) return d;
          last = d.clientLastEvent;
        }
      } catch {
        /* ignore transient */
      }
      await new Promise((r) => setTimeout(r, intervalMs));
    }
    try {
      return await api('/api/devices/' + deviceId);
    } catch {
      return null;
    }
  }

  global.$ = $;
  global.$$ = $$;
  global.escapeHtml = escapeHtml;
  global.api = api;
  global.toast = toast;
  global.pollDeviceEvent = pollDeviceEvent;
})(window);
