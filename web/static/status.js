/**
 * Unified status badges: client (WS) · probe (TCP/ICMP) · bemfa (MQTT).
 * Shared by device cards and status legend.
 */
(function (global) {
  function escapeHtml(s) {
    return String(s ?? '')
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  /** Client WS online — required for remote shutdown. */
  function clientBadge(d) {
    const on = !!d.clientOnline;
    const title = on
      ? '客户端 WebSocket 已连接，可远程关机'
      : d.boundClientKey
        ? '已绑定客户端 key，但当前未连接'
        : '未绑定客户端，无法远程关机';
    return `<span class="badge badge-client ${on ? 'on' : 'off'}" title="${escapeHtml(title)}">客户端 ${on ? '在线' : '离线'}</span>`;
  }

  /** TCP/ICMP probe — independent of client. */
  function probeBadge(d) {
    if (!d.probeMethod || d.probeMethod === 'off') {
      return `<span class="badge badge-probe off" title="未启用在线探测">探测 关</span>`;
    }
    if (d.probeOnline === true) {
      const lat = d.probe && d.probe.latencyMs != null ? ` ${d.probe.latencyMs}ms` : '';
      return `<span class="badge badge-probe on" title="探测目标可达${escapeHtml(lat)}">探测 在线</span>`;
    }
    if (d.probeOnline === false) {
      const err = (d.probe && d.probe.error) || '不可达';
      return `<span class="badge badge-probe off" title="${escapeHtml(err)}">探测 离线</span>`;
    }
    return `<span class="badge badge-probe warn" title="等待探测结果">探测 中</span>`;
  }

  /**
   * Bemfa: device-level enable + global MQTT connection.
   * on = enabled and MQTT connected
   * warn = enabled but MQTT down
   * off = device not using bemfa
   */
  function bemfaBadge(d) {
    if (!d.bemfaEnable) {
      return `<span class="badge badge-bemfa off" title="设备未启用巴法云">巴法 关</span>`;
    }
    if (d.bemfaConnected) {
      const topic = d.bemfaTopic ? `主题 ${d.bemfaTopic}` : 'MQTT 已连接';
      return `<span class="badge badge-bemfa on" title="${escapeHtml(topic)}">巴法 已连</span>`;
    }
    return `<span class="badge badge-bemfa warn" title="设备已开巴法，但全局 MQTT 未连接">巴法 开·未连</span>`;
  }

  function formatClientEvent(d) {
    if (!d.clientLastEvent) return '';
    const t = d.clientLastEventAt
      ? new Date(d.clientLastEventAt * 1000).toLocaleString()
      : '';
    let text = d.clientLastEvent;
    if (d.clientLastError) text += ' · ' + d.clientLastError;
    if (t) text += ' · ' + t;
    return text;
  }

  function statusBadgesHTML(d) {
    return clientBadge(d) + probeBadge(d) + bemfaBadge(d);
  }

  global.WakeHubStatus = {
    escapeHtml,
    clientBadge,
    probeBadge,
    bemfaBadge,
    formatClientEvent,
    statusBadgesHTML,
  };
})(window);
