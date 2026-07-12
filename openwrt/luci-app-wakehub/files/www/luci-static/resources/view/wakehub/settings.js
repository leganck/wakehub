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
	var text = running ? _('Running') : _('Not running');
	return E('span', { 'style': 'font-weight:bold;color:%s'.format(color) }, text);
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
		var port = uci.get('wakehub', 'main', 'listen_port') || '8080';

		m = new form.Map('wakehub', _('WakeHub'),
			_('LAN power hub: Wake-on-LAN, Bemfa MQTT, and client shutdown. Manage devices in the built-in Web UI; this page controls the service and global settings only.'));

		// After LuCI commits UCI, restart so wakehub-uci-sync + procd pick up changes.
		m.save = function () {
			return form.Map.prototype.save.apply(this, arguments).then(function () {
				return fs.exec('/etc/init.d/wakehub', ['restart']).then(function (res) {
					if (res.code && res.code !== 0) {
						ui.addNotification(null, E('p', {}, _('Service restart failed (code %d)').format(res.code)), 'warning');
					}
				}).catch(function (e) {
					ui.addNotification(null, E('p', {}, e.message || e), 'warning');
				});
			});
		};

		s = m.section(form.TypedSection, 'wakehub', _('Service'));
		s.anonymous = true;
		s.addremove = false;

		o = s.option(form.DummyValue, '_status', _('Status'));
		o.rawhtml = true;
		o.cfgvalue = function () {
			return renderStatus(running);
		};

		o = s.option(form.DummyValue, '_webui', _('Web UI'));
		o.rawhtml = true;
		o.cfgvalue = function () {
			var href = 'http://%s:%s/'.format(window.location.hostname, port);
			return E('a', { href: href, target: '_blank', rel: 'noopener' }, href);
		};

		o = s.option(form.Flag, 'enabled', _('Enable'));
		o.default = o.disabled;
		o.rmempty = false;

		o = s.option(form.Value, 'listen_port', _('Listen port'));
		o.datatype = 'port';
		o.placeholder = '8080';
		o.rmempty = false;

		o = s.option(form.Value, 'config_path', _('Config file path'));
		o.placeholder = '/etc/wakehub/config.json';
		o.rmempty = false;

		o = s.option(form.Value, 'bemfa_uid', _('Bemfa UID'));
		o.password = true;
		o.rmempty = true;
		o.placeholder = _('Private key from Bemfa console');

		o = s.option(form.Value, 'client_token', _('Client token'));
		o.password = true;
		o.rmempty = true;
		o.description = _('Shared token for wakehub-client WebSocket auth. Leave empty to disable token check.');

		o = s.option(form.Value, 'ws_path', _('WebSocket path'));
		o.placeholder = '/api/ws/client';
		o.rmempty = false;

		return m.render();
	}
});
