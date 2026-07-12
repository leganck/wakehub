const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];

async function api(path, opts = {}) {
  const res = await fetch(path, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) },
    ...opts,
  });
  if (res.status === 401) {
    throw new Error('需要登录（HTTP Basic 认证）。请刷新页面并输入用户名/密码。');
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

function settingsBodyFromForm(f, extra = {}) {
  const body = {
    listen: f.listen.value,
    bemfaUID: f.bemfaUID.value,
    clientToken: f.clientToken.value,
    wsPath: f.wsPath.value,
    basicAuthEnable: !!f.basicAuthEnable?.checked,
    basicAuthUser: (f.basicAuthUser?.value || 'admin').trim() || 'admin',
    ...extra,
  };
  // Only send password when user typed a new one (empty = keep server-side value).
  const pw = (f.basicAuthPassword?.value || '').trim();
  if (pw) body.basicAuthPassword = pw;
  return body;
}

function applySettingsToForm(f, st) {
  if (!st || !f) return;
  if (st.listen !== undefined) f.listen.value = st.listen || '';
  if (st.bemfaUID !== undefined) f.bemfaUID.value = st.bemfaUID || '';
  if (st.clientToken !== undefined) f.clientToken.value = st.clientToken || '';
  if (st.wsPath !== undefined) f.wsPath.value = st.wsPath || '/api/ws/client';
  if (f.basicAuthEnable) f.basicAuthEnable.checked = !!st.basicAuthEnable;
  if (f.basicAuthUser) f.basicAuthUser.value = st.basicAuthUser || 'admin';
  // Never fill password from API (not returned). Clear input after load/save.
  if (f.basicAuthPassword) {
    f.basicAuthPassword.value = '';
    f.basicAuthPassword.placeholder = st.basicAuthPasswordSet
      ? '已设置，留空则不修改'
      : '默认 admin，建议修改';
  }
  const authHint = $('#authPasswordHint');
  if (authHint) {
    authHint.textContent = st.basicAuthPasswordSet
      ? '管理密码已设置（接口不回传明文）。留空保存表示不修改；填写则更新密码。客户端 WS 不受 Basic 影响。'
      : '尚未设置管理密码时服务端使用默认 admin。建议启用认证后立即修改。客户端 WS 不受 Basic 影响。';
  }
  applyGlobalSettingsLock(st);
}

/** Global fields owned by LuCI when globalManagedByLuci && !globalSettingsWritable */
const GLOBAL_SETTING_FIELDS = [
  'listen',
  'basicAuthEnable',
  'basicAuthUser',
  'basicAuthPassword',
  'bemfaUID',
  'clientToken',
  'wsPath',
];

function applyGlobalSettingsLock(st) {
  // Default writable when field absent (non-OpenWrt).
  const canWrite = st.globalSettingsWritable !== undefined ? !!st.globalSettingsWritable : true;

  const banner = $('#globalSettingsBanner');
  if (banner) {
    if (st.globalManagedByLuci && !canWrite) {
      banner.hidden = false;
      banner.textContent =
        '全局设置由 LuCI（服务 → WakeHub）管理，本页只读。设备增删/唤醒/关机仍可在此操作。';
      banner.className = 'settings-banner';
    } else if (st.globalManagedByLuci && canWrite) {
      banner.hidden = false;
      banner.textContent =
        '全局设置由 LuCI 管理；当前为 writeback 模式，Web 保存会写回 UCI。改端口后请在 LuCI 重启服务。';
      banner.className = 'settings-banner warn';
    } else {
      banner.hidden = true;
    }
  }

  const f = $('#settingsForm');
  if (!f) return;
  GLOBAL_SETTING_FIELDS.forEach((name) => {
    const el = f.elements.namedItem(name);
    if (!el) return;
    if (el instanceof RadioNodeList) return;
    el.disabled = !canWrite;
  });
  const regen = $('#btnRegenToken');
  if (regen) regen.disabled = !canWrite;
  const submit = f.querySelector('button[type="submit"]');
  if (submit) {
    submit.disabled = !canWrite;
    submit.textContent = canWrite ? '保存设置' : '由 LuCI 管理（只读）';
  }
}

function showTab(name) {
  $$('.tab').forEach((t) => t.classList.remove('active'));
  $$('nav button').forEach((b) => b.classList.toggle('active', b.dataset.tab === name));
  $('#tab-' + name).classList.add('active');
  if (name === 'mqtt') loadMQTT();
  if (name === 'discover') loadDiscover();
  if (name === 'devices') loadDevices();
  if (name === 'settings') loadSettings();
}
$$('nav button').forEach((b) => b.addEventListener('click', () => showTab(b.dataset.tab)));

function activeTab() {
  const btn = $('nav button.active');
  return btn ? btn.dataset.tab : 'devices';
}

async function refreshStatus() {
  try {
    const s = await api('/api/status');
    const mqtt = s.mqtt || {};
    const bemfaConnected = !!(s.bemfaConnected || mqtt.connected);
    const uidSet = !!(s.bemfaUIDSet || mqtt.uidSet);
    const bemfaClass = bemfaConnected ? 'ok' : uidSet ? 'muted' : 'muted';
    const bemfaText = bemfaConnected ? '已连接' : uidSet ? '未连接' : '未配置';
    $('#statusBar').innerHTML =
      `巴法 <span class="${bemfaClass}">${bemfaText}</span> · 客户端 ${s.clients} · 设备 ${s.devices}`;
  } catch (e) {
    $('#statusBar').textContent = '状态获取失败: ' + e.message;
  }
}

function formatTime(ms) {
  if (!ms) return '—';
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return '—';
  const pad = (n) => String(n).padStart(2, '0');
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

function mqttConnLabel(st) {
  if (!st) return { text: '未知', badge: '—', badgeClass: 'off', connClass: 'muted' };
  if (!st.enabled || !st.uidSet) {
    return { text: '未配置 UID', badge: '未配置', badgeClass: 'off', connClass: 'muted' };
  }
  if (st.connected) {
    return { text: 'MQTT 已连接', badge: '在线', badgeClass: 'on', connClass: 'ok' };
  }
  if (st.started) {
    return { text: '连接中 / 已断开', badge: '离线', badgeClass: 'off', connClass: 'muted' };
  }
  return { text: '未启动', badge: '停止', badgeClass: 'off', connClass: 'muted' };
}

function renderMQTTStatus(st) {
  const label = mqttConnLabel(st);
  const connText = $('#mqttConnText');
  const badge = $('#mqttConnBadge');
  connText.textContent = label.text;
  connText.className = 'mqtt-conn ' + label.connClass;
  badge.textContent = label.badge;
  badge.className = 'badge ' + label.badgeClass;

  $('#mqttUID').textContent = st.uidMasked || (st.uidSet ? '已设置' : '未设置');
  $('#mqttTopicCount').textContent = String(st.topicCount ?? (st.topics || []).length ?? 0);
  $('#mqttConnectedAt').textContent = st.connectedAt ? formatTime(st.connectedAt * (st.connectedAt < 1e12 ? 1000 : 1)) : '—';
  $('#mqttLastError').textContent = st.lastError || '无';
  $('#mqttLastError').className = st.lastError ? 'meta error' : 'meta';

  const topicsEl = $('#mqttTopics');
  const topics = st.topics || [];
  if (!topics.length) {
    topicsEl.textContent = '暂无订阅';
    topicsEl.className = 'topic-list meta';
    return;
  }
  topicsEl.className = 'topic-list';
  topicsEl.innerHTML = topics
    .map(
      (t) =>
        `<div class="topic-item"><code>${escapeHtml(t.topic)}</code><span class="meta">device ${escapeHtml(t.deviceId)}</span></div>`
    )
    .join('');
}

function renderMQTTLogs(list) {
  const box = $('#mqttLogBox');
  const logs = Array.isArray(list) ? list.slice() : [];
  if (!logs.length) {
    box.innerHTML = '<div class="log-empty muted">暂无 MQTT 日志</div>';
    return;
  }
  // newest last for console feel
  logs.sort((a, b) => (a.time || 0) - (b.time || 0));
  const stickBottom = box.scrollTop + box.clientHeight >= box.scrollHeight - 24;
  box.innerHTML = logs
    .map((e) => {
      const level = escapeHtml(e.level || 'info');
      const ts = formatTime(e.time);
      return `<div class="log-line log-${level}"><span class="log-time">${ts}</span><span class="log-level">${level}</span><span class="log-msg">${escapeHtml(e.message)}</span></div>`;
    })
    .join('');
  if (stickBottom) box.scrollTop = box.scrollHeight;
}

async function loadMQTT() {
  try {
    const data = await api('/api/mqtt/logs?limit=200');
    renderMQTTStatus(data.status || {});
    renderMQTTLogs(data.list || []);
  } catch (e) {
    $('#mqttConnText').textContent = '加载失败: ' + e.message;
    $('#mqttConnText').className = 'mqtt-conn error';
    $('#mqttLogBox').innerHTML = `<div class="log-empty error">${escapeHtml(e.message)}</div>`;
  }
  refreshStatus();
}

$('#btnRefreshMQTT').onclick = () => loadMQTT();
$('#btnClearMQTTLogs').onclick = async () => {
  try {
    await api('/api/mqtt/logs', { method: 'DELETE' });
    await loadMQTT();
  } catch (e) {
    alert('清空失败: ' + e.message);
  }
};

function deviceCard(d) {
  const el = document.createElement('div');
  el.className = 'device';
  el.innerHTML = `
    <div class="device-head">
      <div>
        <h3 class="device-title">${escapeHtml(d.name)}</h3>
        <div class="meta">MAC ${escapeHtml(d.mac)}</div>
      </div>
      <div class="badges">
        <span class="badge ${d.clientOnline ? 'on' : 'off'}">客户端${d.clientOnline ? '在线' : '离线'}</span>
        <span class="badge ${d.bemfaEnable ? 'on' : 'off'}">巴法${d.bemfaEnable ? '开' : '关'}</span>
      </div>
    </div>
    <div class="meta">广播 ${escapeHtml(d.broadcast || '自动')} · 端口 ${d.port || 9} · 重复 ${d.repeat || 3}</div>
    <div class="meta">主题 ${escapeHtml(d.bemfaTopic || '—')} · 绑定 ${escapeHtml(d.boundClientKey || '—')}</div>
    <div class="actions">
      <button class="btn btn-wake" data-act="wake" type="button">唤醒</button>
      <button class="btn btn-shutdown" data-act="shutdown" type="button">关机</button>
      <button class="btn btn-utility" data-act="edit" type="button">编辑</button>
      <button class="btn btn-danger" data-act="del" type="button">删除</button>
    </div>
    <p class="actmsg"></p>
  `;
  el.querySelector('[data-act=wake]').onclick = async () => {
    const msg = el.querySelector('.actmsg');
    try {
      await api('/api/devices/' + d.id + '/wake', { method: 'POST', body: '{}' });
      msg.className = 'ok actmsg';
      msg.textContent = '已发送唤醒';
    } catch (e) {
      msg.className = 'error actmsg';
      msg.textContent = e.message;
    }
  };
  el.querySelector('[data-act=shutdown]').onclick = async () => {
    const msg = el.querySelector('.actmsg');
    const name = d.name || d.id || '该设备';
    const ok = confirm(
      `确认向「${name}」发送关机指令？\n\n` +
        `设备将立即关机（需客户端在线）。\n` +
        `此操作不可撤销，请再次确认。`
    );
    if (!ok) {
      msg.className = 'actmsg muted';
      msg.textContent = '已取消关机';
      return;
    }
    const ok2 = confirm(`最后确认：立即关闭「${name}」？`);
    if (!ok2) {
      msg.className = 'actmsg muted';
      msg.textContent = '已取消关机';
      return;
    }
    try {
      await api('/api/devices/' + d.id + '/shutdown', { method: 'POST', body: '{}' });
      msg.className = 'ok actmsg';
      msg.textContent = '已发送关机';
    } catch (e) {
      msg.className = 'error actmsg';
      msg.textContent = e.message;
    }
  };
  el.querySelector('[data-act=edit]').onclick = () => openDeviceDialog(d);
  el.querySelector('[data-act=del]').onclick = async () => {
    if (!confirm('删除设备 ' + d.name + ' ?')) return;
    await api('/api/devices/' + d.id, { method: 'DELETE' });
    loadDevices();
  };
  return el;
}

async function loadDevices() {
  const data = await api('/api/devices');
  const box = $('#deviceList');
  box.innerHTML = '';
  (data.list || []).forEach((d) => box.appendChild(deviceCard(d)));
  if (!(data.list || []).length) {
    box.innerHTML = '<p class="empty muted">暂无设备，点击「添加设备」开始</p>';
  }
  refreshStatus();
}

function openDeviceDialog(d = {}) {
  const f = $('#deviceForm');
  f.id.value = d.id || '';
  f.name.value = d.name || '';
  f.mac.value = d.mac || '';
  f.broadcast.value = d.broadcast || '';
  f.port.value = d.port || 9;
  f.repeat.value = d.repeat || 3;
  f.boundClientKey.value = d.boundClientKey || '';
  f.bemfaEnable.checked = !!d.bemfaEnable;
  f.bemfaTopic.value = d.bemfaTopic || '';
  f.bemfaName.value = d.bemfaName || '';
  $('#deviceDialogTitle').textContent = d.id ? '编辑设备' : '添加设备';
  $('#deviceFormError').textContent = '';
  $('#deviceDialog').showModal();
}

$('#btnAddDevice').onclick = () => openDeviceDialog({});
$('#btnRefreshDevices').onclick = () => loadDevices();

$('#deviceForm').addEventListener('submit', async (ev) => {
  const submitter = ev.submitter;
  if (submitter && submitter.value === 'cancel') return;
  ev.preventDefault();
  const f = ev.target;
  const body = {
    id: f.id.value,
    name: f.name.value,
    mac: f.mac.value,
    broadcast: f.broadcast.value,
    port: Number(f.port.value || 9),
    repeat: Number(f.repeat.value || 3),
    boundClientKey: f.boundClientKey.value,
    bemfaEnable: f.bemfaEnable.checked,
    bemfaTopic: f.bemfaTopic.value,
    bemfaName: f.bemfaName.value,
  };
  try {
    if (body.id) await api('/api/devices/' + body.id, { method: 'PUT', body: JSON.stringify(body) });
    else await api('/api/devices', { method: 'POST', body: JSON.stringify(body) });
    $('#deviceDialog').close();
    loadDevices();
  } catch (e) {
    $('#deviceFormError').textContent = e.message;
  }
});

async function loadDiscover() {
  const data = await api('/api/discover');
  const cbox = $('#clientList');
  cbox.innerHTML = '';
  (data.clients || []).forEach((c) => {
    const el = document.createElement('div');
    el.className = 'item';
    const nics =
      (c.nics || [])
        .map((n) => `${escapeHtml(n.mac)} ${escapeHtml((n.ipv4 || []).join(', '))}`)
        .join('<br/>') || '—';
    el.innerHTML = `
      <div class="item-head">
        <div>
          <h3 class="item-title">${escapeHtml(c.hostname || c.key)}</h3>
          <div class="meta">key=${escapeHtml(c.key)} · ${escapeHtml(c.remote || '')}</div>
        </div>
        <span class="badge on">在线</span>
      </div>
      <div class="meta">${nics}</div>
      <div class="actions">
        <button class="btn btn-primary" type="button">一键建档</button>
      </div>`;
    el.querySelector('button').onclick = async () => {
      await api('/api/devices/from-client', {
        method: 'POST',
        body: JSON.stringify({ clientKey: c.key }),
      });
      showTab('devices');
      loadDevices();
    };
    cbox.appendChild(el);
  });
  if (!(data.clients || []).length) {
    cbox.innerHTML = '<p class="empty muted">无已连接客户端</p>';
  }

  const mbox = $('#mdnsList');
  mbox.innerHTML = '';
  (data.mdns || []).forEach((p) => {
    const el = document.createElement('div');
    el.className = 'item';
    el.innerHTML = `
      <div class="item-head">
        <div>
          <h3 class="item-title">${escapeHtml(p.hostname || p.instance)}</h3>
          <div class="meta">key=${escapeHtml(p.key || '—')} · mac=${escapeHtml(p.mac || '—')}</div>
        </div>
      </div>
      <div class="meta">ips ${escapeHtml((p.ips || []).join(', ') || '—')}</div>`;
    mbox.appendChild(el);
  });
  if (!(data.mdns || []).length) {
    mbox.innerHTML = '<p class="empty muted">暂无 mDNS 结果（需 macvlan / 同网段）</p>';
  }
}
$('#btnRefreshDiscover').onclick = () => loadDiscover();

function listenPortFromSettings(listen) {
  const s = String(listen || '').trim();
  if (!s) return window.location.port || '8080';
  // ":8080" or "0.0.0.0:8080" or "8080"
  const m = s.match(/:(\d+)\s*$/);
  if (m) return m[1];
  if (/^\d+$/.test(s)) return s;
  return window.location.port || '8080';
}

function wsURLFromSettings(listen, wsPath) {
  const host = window.location.hostname || '127.0.0.1';
  const port = listenPortFromSettings(listen);
  let path = (wsPath || '/api/ws/client').trim() || '/api/ws/client';
  if (!path.startsWith('/')) path = '/' + path;
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  // Prefer current page host (reachable from browser); port from listen setting
  const pagePort = window.location.port;
  const usePort = port || pagePort || '8080';
  return `${proto}//${host}:${usePort}${path}`;
}

function shellQuote(s) {
  // single-quote for POSIX; also fine for display on Windows when path has no quotes needed
  return "'" + String(s ?? '').replace(/'/g, `'\\''`) + "'";
}

function updateClientCommands() {
  const f = $('#settingsForm');
  if (!f) return;
  const token = (f.clientToken.value || '').trim();
  const key = ($('#clientKeyInput')?.value || 'my-pc').trim() || 'my-pc';
  const server = wsURLFromSettings(f.listen.value, f.wsPath.value);
  const runEl = $('#clientCmdRun');
  const instEl = $('#clientCmdInstall');
  if (!runEl || !instEl) return;

  if (!token) {
    runEl.textContent = '# Token 为空：打开本页或保存设置后将自动生成';
    instEl.textContent = '# Token 为空：打开本页或保存设置后将自动生成';
    return;
  }

  // Cross-platform run (binary name; user may prefix path)
  runEl.textContent =
    `wakehub-client run -server ${server} -token ${token} -key ${key}`;

  // Install hints for both OS
  instEl.textContent =
    `# Windows（管理员 PowerShell / CMD）\n` +
    `wakehub-client.exe install -server ${server} -token ${token} -key ${key}\n\n` +
    `# Linux（root）\n` +
    `./wakehub-client install -server ${shellQuote(server)} -token ${shellQuote(token)} -key ${shellQuote(key)}`;
}

async function loadSettings() {
  const st = await api('/api/settings');
  const f = $('#settingsForm');
  applySettingsToForm(f, st);

  const hint = $('#tokenHint');
  if (hint) {
    hint.textContent = st.tokenGenerated
      ? '已自动生成随机 Token 并写入配置，请使用下方命令连接客户端。'
      : '为空时服务端会自动生成随机 Token 并持久化。保存空 Token 也会重新生成。';
  }
  updateClientCommands();
}

$('#settingsForm')?.addEventListener('submit', async (ev) => {
  ev.preventDefault();
  const f = ev.target;
  const body = settingsBodyFromForm(f);
  const res = await api('/api/settings', { method: 'PUT', body: JSON.stringify(body) });
  applySettingsToForm(f, res.settings || res);
  if (res.warning) alert('已保存，但巴法重连警告: ' + res.warning);
  else alert('已保存');
  updateClientCommands();
  refreshStatus();
  if (activeTab() === 'mqtt') loadMQTT();
});

$('#btnRegenToken')?.addEventListener('click', async () => {
  if (!confirm('重新生成 Token 后，旧客户端需更新参数才能连接，继续？')) return;
  const f = $('#settingsForm');
  const body = settingsBodyFromForm(f, { regenerateToken: true });
  const res = await api('/api/settings', { method: 'PUT', body: JSON.stringify(body) });
  applySettingsToForm(f, res.settings || res);
  updateClientCommands();
  const hint = $('#tokenHint');
  if (hint) hint.textContent = '已重新生成 Token 并保存。';
  if (res.warning) alert('Token 已更新，但巴法重连警告: ' + res.warning);
});

['listen', 'clientToken', 'wsPath'].forEach((name) => {
  const el = $(`#settingsForm [name="${name}"]`);
  if (el) el.addEventListener('input', updateClientCommands);
});
$('#clientKeyInput')?.addEventListener('input', updateClientCommands);

document.querySelectorAll('.btn-copy').forEach((btn) => {
  btn.addEventListener('click', async () => {
    const id = btn.getAttribute('data-copy');
    const pre = id ? document.getElementById(id) : null;
    const text = pre ? pre.textContent : '';
    if (!text || text.startsWith('#')) {
      alert('暂无可用命令');
      return;
    }
    try {
      await navigator.clipboard.writeText(text);
      const old = btn.textContent;
      btn.textContent = '已复制';
      setTimeout(() => {
        btn.textContent = old;
      }, 1500);
    } catch {
      // fallback
      const ta = document.createElement('textarea');
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
      btn.textContent = '已复制';
      setTimeout(() => {
        btn.textContent = '复制';
      }, 1500);
    }
  });
});

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

// boot
showTab('devices');
loadDevices();
loadSettings();
loadDiscover();
setInterval(refreshStatus, 10000);
setInterval(() => {
  if (activeTab() === 'mqtt') loadMQTT();
}, 3000);
