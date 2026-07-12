'use strict';
'require view';
'require form';
'require uci';
'require rpc';
'require fs';
'require ui';

var callServiceList = rpc.declare({
	object: 'service',
	method: 'list',
	params: ['name'],
	expect: { '': {} }
});

function getServiceStatus() {
	return L.resolveDefault(callServiceList('wakehub'), {}).then(function (res) {
		var is = res.wakehub && res.wakehub.instances ? res.wakehub.instances : {};
		for (var k in is) {
			if (is[k].running)
				return true;
		}
		return false;
	});
}

function renderStatus(running) {
	var color = running ? 'green' : 'red';
	var text = running ? _('运行中') : _('未运行');
	return E('span', { 'style': 'font-weight:bold;color:%s'.format(color) }, text);
}

function randomToken(len) {
	len = len || 32;
	var bytes = Math.ceil(len / 2);
	var arr = new Uint8Array(bytes);
	if (window.crypto && window.crypto.getRandomValues)
		window.crypto.getRandomValues(arr);
	else {
		for (var i = 0; i < bytes; i++)
			arr[i] = Math.floor(Math.random() * 256);
	}
	var hex = Array.prototype.map.call(arr, function (b) {
		return ('0' + b.toString(16)).slice(-2);
	}).join('');
	return hex.slice(0, len);
}

function shellQuote(s) {
	return "'" + String(s || '').replace(/'/g, `'\\''`) + "'";
}

function buildClientCommands(host, port, token, key, wsPath) {
	port = String(port || '8080').trim() || '8080';
	key = String(key || 'my-pc').trim() || 'my-pc';
	wsPath = String(wsPath || '/api/ws/client').trim() || '/api/ws/client';
	if (wsPath.charAt(0) !== '/')
		wsPath = '/' + wsPath;
	var server = 'ws://%s:%s%s'.format(host, port, wsPath);
	var run = 'wakehub-client run -server %s -token %s -key %s'.format(server, token, key);
	var install =
		'# Windows（管理员）\n' +
		'wakehub-client.exe install -server %s -token %s -key %s\n\n'.format(server, token, key) +
		'# Linux（root）\n' +
		'./wakehub-client install -server %s -token %s -key %s'.format(
			shellQuote(server), shellQuote(token), shellQuote(key));
	return { run: run, install: install, server: server };
}

function copyText(text) {
	if (navigator.clipboard && navigator.clipboard.writeText)
		return navigator.clipboard.writeText(text);
	return new Promise(function (resolve, reject) {
		var ta = document.createElement('textarea');
		ta.value = text;
		document.body.appendChild(ta);
		ta.select();
		try {
			document.execCommand('copy');
			resolve();
		} catch (e) {
			reject(e);
		}
		document.body.removeChild(ta);
	});
}

return view.extend({
	load: function () {
		return Promise.all([
			uci.load('wakehub'),
			getServiceStatus()
		]);
	},

	render: function (data) {
		var running = data[1];
		var m, s, o;
		var tokenAuto = false;
		var curToken = uci.get('wakehub', 'main', 'client_token') || '';
		if (!String(curToken).trim()) {
			curToken = randomToken(32);
			uci.set('wakehub', 'main', 'client_token', curToken);
			tokenAuto = true;
		}
		var port = uci.get('wakehub', 'main', 'listen_port') || '8080';
		var wsPath = uci.get('wakehub', 'main', 'ws_path') || '/api/ws/client';
		var host = window.location.hostname || '192.168.1.1';

		m = new form.Map('wakehub', _('WakeHub'),
			_('局域网电源中枢：WOL、巴法 MQTT、客户端关机。设备管理用内置 Web；本页配置服务与客户端连接。'));

		m.save = function () {
			return form.Map.prototype.save.apply(this, arguments).then(function () {
				return fs.exec('/etc/init.d/wakehub', ['restart']).then(function (res) {
					if (res.code && res.code !== 0)
						ui.addNotification(null, E('p', {}, _('服务重启失败 (code %d)').format(res.code)), 'warning');
					else
						ui.addNotification(null, E('p', {}, _('配置已保存并已重启服务')), 'info');
				}).catch(function (e) {
					ui.addNotification(null, E('p', {}, e.message || e), 'warning');
				});
			});
		};

		s = m.section(form.TypedSection, 'wakehub', _('服务设置'));
		s.anonymous = true;
		s.addremove = false;

		o = s.option(form.DummyValue, '_status', _('运行状态'));
		o.rawhtml = true;
		o.cfgvalue = function () {
			return renderStatus(running);
		};

		o = s.option(form.Flag, 'enabled', _('启用服务'));
		o.default = o.disabled;
		o.rmempty = false;

		o = s.option(form.Value, 'listen_port', _('运行端口'));
		o.datatype = 'port';
		o.placeholder = '8080';
		o.default = '8080';
		o.rmempty = false;
		o.description = _('HTTP / WebSocket 监听端口，默认 8080。');

		o = s.option(form.Value, 'config_path', _('配置文件路径'));
		o.placeholder = '/etc/wakehub/config.json';
		o.rmempty = false;

		o = s.option(form.Value, 'bemfa_uid', _('巴法 UID'));
		o.password = true;
		o.rmempty = true;

		o = s.option(form.Value, 'client_token', _('客户端 Token'));
		o.password = false;
		o.rmempty = false;
		o.optional = false;
		o.cfgvalue = function (section_id) {
			var v = uci.get('wakehub', section_id, 'client_token');
			if (!v || !String(v).trim())
				return curToken;
			return v;
		};
		o.description = tokenAuto
			? _('原先未设置，已自动填充随机 Token；请保存并应用后生效。')
			: _('客户端连接鉴权令牌；勿泄露。可清空后保存以触发重新生成（需配合 Web 端，或手动粘贴新随机值）。');

		o = s.option(form.Value, 'ws_path', _('WebSocket 路径'));
		o.placeholder = '/api/ws/client';
		o.default = '/api/ws/client';
		o.rmempty = false;

		/* Client command helper (read-only UI) */
		s = m.section(form.NamedSection, 'main', 'wakehub', _('客户端连接命令'));
		s.anonymous = true;

		o = s.option(form.DummyValue, '_client_cmds', _('可复制命令'));
		o.rawhtml = true;
		o.cfgvalue = function () {
			var p = uci.get('wakehub', 'main', 'listen_port') || port || '8080';
			var tok = uci.get('wakehub', 'main', 'client_token') || curToken;
			var wsp = uci.get('wakehub', 'main', 'ws_path') || wsPath;
			var cmds = buildClientCommands(host, p, tok, 'my-pc', wsp);
			var runId = 'wakehub-cmd-run';
			var instId = 'wakehub-cmd-install';

			var box = E('div', { 'class': 'cbi-value-field' }, [
				E('p', {}, _('将下方命令复制到 PC 执行（把二进制名换成实际路径）。Token 变更后需更新客户端。')),
				E('div', { 'style': 'margin:8px 0 4px;font-weight:600' }, _('运行')),
				E('pre', {
					'id': runId,
					'style': 'white-space:pre-wrap;word-break:break-all;background:#1d1d1f;color:#f5f5f7;padding:10px;border-radius:6px;font-size:12px'
				}, [cmds.run]),
				E('button', {
					'class': 'btn cbi-button cbi-button-action',
					'type': 'button',
					'click': function (ev) {
						ev.preventDefault();
						copyText(cmds.run).then(function () {
							ui.addNotification(null, E('p', {}, _('运行命令已复制')), 'info');
						}).catch(function () {
							ui.addNotification(null, E('p', {}, _('复制失败，请手动选择文本')), 'warning');
						});
					}
				}, _('复制运行命令')),
				E('div', { 'style': 'margin:16px 0 4px;font-weight:600' }, _('安装为系统服务')),
				E('pre', {
					'id': instId,
					'style': 'white-space:pre-wrap;word-break:break-all;background:#1d1d1f;color:#f5f5f7;padding:10px;border-radius:6px;font-size:12px'
				}, [cmds.install]),
				E('button', {
					'class': 'btn cbi-button cbi-button-action',
					'type': 'button',
					'click': function (ev) {
						ev.preventDefault();
						copyText(cmds.install).then(function () {
							ui.addNotification(null, E('p', {}, _('安装命令已复制')), 'info');
						}).catch(function () {
							ui.addNotification(null, E('p', {}, _('复制失败，请手动选择文本')), 'warning');
						});
					}
				}, _('复制安装命令')),
				E('p', { 'style': 'margin-top:12px;opacity:0.75' },
					_('Web UI：http://%s:%s/').format(host, p))
			]);
			return box;
		};

		return m.render();
	}
});
