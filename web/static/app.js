const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) },
    ...opts,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
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

async function loadSettings() {
  const st = await api('/api/settings');
  const f = $('#settingsForm');
  f.listen.value = st.listen || '';
  f.bemfaUID.value = st.bemfaUID || '';
  f.clientToken.value = st.clientToken || '';
  f.wsPath.value = st.wsPath || '';
}
$('#settingsForm').addEventListener('submit', async (ev) => {
  ev.preventDefault();
  const f = ev.target;
  const body = {
    listen: f.listen.value,
    bemfaUID: f.bemfaUID.value,
    clientToken: f.clientToken.value,
    wsPath: f.wsPath.value,
  };
  const res = await api('/api/settings', { method: 'PUT', body: JSON.stringify(body) });
  if (res.warning) alert('已保存，但巴法重连警告: ' + res.warning);
  else alert('已保存');
  refreshStatus();
  if (activeTab() === 'mqtt') loadMQTT();
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
