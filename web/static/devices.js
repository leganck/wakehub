/** Device list / cards / batch actions. */
(function (global) {
  let cachedDevices = [];

  function getCachedDevices() {
    return cachedDevices;
  }

  function deviceCard(d) {
    const el = document.createElement('div');
    el.className = 'device';
    const S = global.WakeHubStatus || {};
    const badges = S.statusBadgesHTML ? S.statusBadgesHTML(d) : '';
    const nicHint = d.preferredNic ? ` · 网卡 ${escapeHtml(d.preferredNic)}` : '';
    const verHint = d.clientVersion ? ` · 客户端 v${escapeHtml(d.clientVersion)}` : '';
    const lastEv = S.formatClientEvent ? S.formatClientEvent(d) : '';
    const lastEvHTML = lastEv
      ? `<div class="meta event-line">最近客户端事件：${escapeHtml(lastEv)}</div>`
      : '';
    el.innerHTML = `
    <div class="device-head">
      <div>
        <h3 class="device-title">
          <input type="checkbox" class="device-select" data-id="${escapeHtml(d.id)}" />
          ${escapeHtml(d.name)}
        </h3>
        <div class="meta">MAC ${escapeHtml(d.mac)}${nicHint}${verHint}</div>
      </div>
      <div class="badges">${badges}</div>
    </div>
    <div class="meta">广播 ${escapeHtml(d.broadcast || '自动')} · WOL ${d.port || 9} · 重复 ${d.repeat || 3}</div>
    <div class="meta">探测 ${escapeHtml(d.probeMethod || 'off')}${d.probeHost ? ' @ ' + escapeHtml(d.probeHost) : ''}${d.probeMethod === 'tcp' ? ':' + (d.probePort || 3389) : ''} · 绑定 ${escapeHtml(d.boundClientKey || '—')}</div>
    ${lastEvHTML}
    <div class="actions">
      <button class="btn btn-wake" data-act="wake" type="button">唤醒</button>
      <button class="btn btn-shutdown" data-act="shutdown" type="button" ${d.clientOnline ? '' : 'title="需要客户端在线"'}>关机</button>
      <button class="btn btn-utility" data-act="restart" type="button" ${d.clientOnline ? '' : 'title="需要客户端在线"'}>重启</button>
      <button class="btn btn-utility" data-act="probe" type="button">探测</button>
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
        toast('已发送唤醒: ' + (d.name || d.id), 'ok');
      } catch (e) {
        msg.className = 'error actmsg';
        msg.textContent = e.message;
      }
    };
    el.querySelector('[data-act=shutdown]').onclick = async () => {
      const msg = el.querySelector('.actmsg');
      const name = d.name || d.id || '该设备';
      if (!d.clientOnline) {
        msg.className = 'error actmsg';
        msg.textContent = '客户端离线，无法远程关机';
        return;
      }
      if (!confirm(`确认向「${name}」发送关机指令？\n\n设备将立即关机（需客户端在线）。`)) {
        msg.className = 'actmsg muted';
        msg.textContent = '已取消关机';
        return;
      }
      if (!confirm(`最后确认：立即关闭「${name}」？`)) {
        msg.className = 'actmsg muted';
        msg.textContent = '已取消关机';
        return;
      }
      try {
        const res = await api('/api/devices/' + d.id + '/shutdown', { method: 'POST', body: '{}' });
        msg.className = 'ok actmsg';
        msg.textContent = '已下发关机' + (res.requestId ? ` (${res.requestId.slice(-8)})` : '') + '，等待客户端 ACK…';
        const updated = await pollDeviceEvent(d.id);
        if (updated && updated.clientLastEvent) {
          msg.textContent = '客户端: ' + updated.clientLastEvent + (updated.clientLastError ? ' · ' + updated.clientLastError : '');
          msg.className = String(updated.clientLastEvent).includes('err') ? 'error actmsg' : 'ok actmsg';
        }
        loadDevices();
      } catch (e) {
        msg.className = 'error actmsg';
        msg.textContent = e.message;
      }
    };
    el.querySelector('[data-act=restart]').onclick = async () => {
      const msg = el.querySelector('.actmsg');
      if (!d.clientOnline) {
        msg.className = 'error actmsg';
        msg.textContent = '客户端离线，无法远程重启';
        return;
      }
      if (!confirm(`确认重启「${d.name || d.id}」？`)) return;
      try {
        const res = await api('/api/devices/' + d.id + '/restart', { method: 'POST', body: '{}' });
        msg.className = 'ok actmsg';
        msg.textContent = '已下发重启' + (res.requestId ? ` (${res.requestId.slice(-8)})` : '');
        await pollDeviceEvent(d.id);
        loadDevices();
      } catch (e) {
        msg.className = 'error actmsg';
        msg.textContent = e.message;
      }
    };
    el.querySelector('[data-act=probe]').onclick = async () => {
      const msg = el.querySelector('.actmsg');
      try {
        const res = await api('/api/devices/' + d.id + '/probe', { method: 'POST', body: '{}' });
        const p = res.probe || {};
        msg.className = p.online ? 'ok actmsg' : 'error actmsg';
        msg.textContent = p.online
          ? `探测在线 ${p.target || ''} ${p.latencyMs != null ? p.latencyMs + 'ms' : ''}`
          : `探测离线 ${p.error || ''}`;
        loadDevices();
      } catch (e) {
        msg.className = 'error actmsg';
        msg.textContent = e.message;
      }
    };
    el.querySelector('[data-act=edit]').onclick = () => {
      if (typeof openDeviceDialog === 'function') openDeviceDialog(d);
    };
    el.querySelector('[data-act=del]').onclick = async () => {
      if (!confirm('删除设备 ' + d.name + ' ?')) return;
      await api('/api/devices/' + d.id, { method: 'DELETE' });
      loadDevices();
    };
    return el;
  }

  function selectedDeviceIds() {
    return $$('#deviceList .device-select:checked').map((el) => el.dataset.id).filter(Boolean);
  }

  async function batchAction(action) {
    const ids = selectedDeviceIds();
    if (!ids.length) {
      toast('请先勾选设备', 'error');
      return;
    }
    if (action === 'shutdown') {
      if (!confirm(`确认对 ${ids.length} 台设备发送关机？`)) return;
      if (!confirm('最后确认批量关机？')) return;
    }
    try {
      const res = await api('/api/batch', {
        method: 'POST',
        body: JSON.stringify({ action, ids }),
      });
      const results = res.results || {};
      const fail = Object.entries(results).filter(([, v]) => v !== 'ok');
      toast(fail.length ? `完成，失败 ${fail.length} 台` : `已对 ${ids.length} 台执行 ${action}`, fail.length ? 'error' : 'ok');
      loadDevices();
    } catch (e) {
      /* toast in api */
    }
  }

  async function loadDevices() {
    const data = await api('/api/devices');
    cachedDevices = data.list || [];
    const box = $('#deviceList');
    box.innerHTML = '';
    cachedDevices.forEach((d) => box.appendChild(deviceCard(d)));
    if (!cachedDevices.length) {
      box.innerHTML = '<p class="empty muted">暂无设备，点击「添加设备」开始</p>';
    }
    if (typeof refreshStatus === 'function') refreshStatus();
  }

  global.getCachedDevices = getCachedDevices;
  global.deviceCard = deviceCard;
  global.selectedDeviceIds = selectedDeviceIds;
  global.batchAction = batchAction;
  global.loadDevices = loadDevices;
})(window);
