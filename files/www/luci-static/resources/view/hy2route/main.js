'use strict';
'require view';
'require form';
'require uci';

function validIPv4OrCIDR(value) {
	var fields = value.split('/');
	var octets;

	if (fields.length > 2)
		return false;
	if (fields.length === 2 && (!/^\d+$/.test(fields[1]) || +fields[1] > 32))
		return false;

	octets = fields[0].split('.');
	return octets.length === 4 && octets.every(function(octet) {
		return /^\d+$/.test(octet) && +octet >= 0 && +octet <= 255;
	});
}

function validDomain(value) {
	var domain = value.replace(/^\*\./, '').replace(/^\.+/, '');

	return domain.length > 0 && domain.length <= 253 &&
		/^[A-Za-z0-9_](?:[A-Za-z0-9_.-]*[A-Za-z0-9_])?$/.test(domain) &&
		domain.split('.').every(function(label) {
			return label.length > 0 && label.length <= 63 && !/^-|-$/.test(label);
		});
}

return view.extend({
	load: function() {
		return uci.load('hy2route');
	},

	render: function() {
		var m, s, o;

		m = new form.Map('hy2route', 'HY2Route',
			_('透明代理：TCP 经 HY2 中转到 SOCKS/HTTP 落地，UDP 直接从 HY2 中转出站。中国大陆 IPv4 默认直连。'));

		s = m.section(form.NamedSection, 'main', 'main', _('基本设置'));
		s.tab('general', _('常规'));
		s.tab('advanced', _('高级'));

		o = s.taboption('general', form.Flag, 'enabled', _('启用 HY2Route'));
		o.rmempty = false;
		o.default = o.disabled;

		o = s.taboption('general', form.ListValue, 'udp_policy', _('UDP 策略'),
			_('代理 UDP 直接从 HY2 中转出站，不经过 SOCKS/HTTP 落地。'));
		o.value('proxy', _('代理'));
		o.value('direct', _('直连'));
		o.value('block', _('阻止'));
		o.default = 'proxy';
		o.rmempty = false;

		o = s.taboption('general', form.Value, 'bootstrap_dns', _('引导 DNS'));
		o.datatype = 'ip4addr';
		o.default = '192.168.1.1';
		o.rmempty = false;

		o = s.taboption('general', form.Value, 'remote_dns', _('远程 DNS'));
		o.datatype = 'ip4addr';
		o.default = '8.8.8.8';
		o.rmempty = false;

		o = s.taboption('general', form.ListValue, 'log_level', _('日志级别'));
		o.value('warning', _('警告'));
		o.value('info', _('信息'));
		o.value('debug', _('调试'));
		o.value('none', _('关闭'));
		o.default = 'warning';

		o = s.taboption('advanced', form.Value, 'lan_interface', _('LAN 接口'));
		o.default = 'br-lan';
		o.rmempty = false;

	[
		['transparent_port', _('透明代理端口'), '12345'],
		['test_socks_port', _('本机测试 SOCKS 端口'), '10780'],
		['dns_port', _('DNS 代理端口'), '1053']
	].forEach(function(spec) {
		o = s.taboption('advanced', form.Value, spec[0], spec[1]);
		o.datatype = 'port';
		o.default = spec[2];
		o.rmempty = false;
	});

		o = s.taboption('advanced', form.Value, 'fwmark', _('TPROXY 标记'));
		o.datatype = 'uinteger';
		o.default = '102';
		o.rmempty = false;

		o = s.taboption('advanced', form.Value, 'route_table', _('策略路由表'));
		o.datatype = 'uinteger';
		o.default = '166';
		o.rmempty = false;

		o = s.taboption('advanced', form.Flag, 'block_ipv6', _('阻止 IPv6 转发'),
			_('阻止 LAN 客户端通过未代理的 IPv6 访问外网，不影响路由器本机 IPv6 服务。'));
		o.default = o.enabled;

		s = m.section(form.NamedSection, 'relay', 'hy2', _('HY2 中转'));

		o = s.option(form.Value, 'server', _('服务器'));
		o.datatype = 'host';
		o.rmempty = false;

		o = s.option(form.Value, 'port', _('端口'));
		o.datatype = 'port';
		o.default = '443';
		o.rmempty = false;

		o = s.option(form.Value, 'auth', _('认证密码'));
		o.password = true;
		o.rmempty = false;

		o = s.option(form.Value, 'sni', _('TLS SNI'));
		o.datatype = 'host';
		o.rmempty = true;

		o = s.option(form.Flag, 'allow_insecure', _('允许不安全证书'),
			_('仅在中转证书无法正常验证时启用。'));
		o.default = o.disabled;

		o = s.option(form.Value, 'pinned_cert_sha256', _('固定证书 SHA-256'));
		o.password = true;
		o.rmempty = true;

		o = s.option(form.ListValue, 'congestion', _('拥塞控制'));
		o.value('bbr', _('BBR（稳定）'));
		o.value('reno', _('Reno（保守）'));
		o.default = 'bbr';
		o.rmempty = false;

		o = s.option(form.ListValue, 'bbr_profile', _('BBR 模式'));
		o.value('conservative', _('保守'));
		o.value('standard', _('标准'));
		o.value('aggressive', _('激进'));
		o.default = 'standard';
		o.rmempty = false;
		o.depends('congestion', 'bbr');

		o = s.option(form.Value, 'udp_idle_timeout', _('UDP 空闲时间（秒）'));
		o.datatype = 'range(2,600)';
		o.default = '60';
		o.rmempty = false;

		o = s.option(form.Value, 'max_idle_timeout', _('QUIC 最大空闲时间（秒）'));
		o.datatype = 'range(4,120)';
		o.default = '60';
		o.rmempty = false;

		o = s.option(form.Value, 'keep_alive_period', _('QUIC 保活间隔（秒）'));
		o.datatype = 'range(0,60)';
		o.default = '0';
		o.description = _('0 表示关闭主动保活，允许空闲 QUIC 连接按最大空闲时间关闭。');
		o.rmempty = false;

		o = s.option(form.Flag, 'disable_mtu_discovery', _('禁用路径 MTU 探测'));
		o.default = o.disabled;

		s = m.section(form.NamedSection, 'landing', 'landing', _('落地代理'));

		o = s.option(form.ListValue, 'type', _('类型'));
		o.value('socks', _('SOCKS5'));
		o.value('http', _('HTTP'));
		o.default = 'socks';
		o.rmempty = false;

		o = s.option(form.Value, 'server', _('服务器'));
		o.datatype = 'host';
		o.rmempty = false;

		o = s.option(form.Value, 'port', _('端口'));
		o.datatype = 'port';
		o.default = '443';
		o.rmempty = false;

		o = s.option(form.Value, 'username', _('用户名'));
		o.rmempty = true;

		o = s.option(form.Value, 'password', _('密码'));
		o.password = true;
		o.rmempty = true;

		s = m.section(form.GridSection, 'rule', _('指定规则'),
			_('域名规则同时匹配该域名及其子域名。IP 规则支持单个 IPv4 或 CIDR；相同目标同时出现时，代理优先。'));
		s.anonymous = true;
		s.addremove = true;
		s.sortable = true;
		s.nodescriptions = true;

		o = s.option(form.Flag, 'enabled', _('启用'));
		o.default = o.enabled;
		o.editable = true;

		o = s.option(form.ListValue, 'type', _('目标类型'));
		o.value('domain', _('域名'));
		o.value('ip', _('IPv4/CIDR'));
		o.default = 'domain';
		o.rmempty = false;

		o = s.option(form.ListValue, 'action', _('动作'));
		o.value('direct', _('直连'));
		o.value('proxy', _('代理'));
		o.default = 'direct';
		o.rmempty = false;

		o = s.option(form.Value, 'value', _('目标'));
		o.rmempty = false;
		o.placeholder = 'example.com / 203.0.113.0/24';
		o.validate = function(sectionId, value) {
			var type = this.section.formvalue(sectionId, 'type') || 'domain';

			if (type === 'ip' && !validIPv4OrCIDR(value))
				return _('请输入有效的 IPv4 地址或 CIDR。');
			if (type === 'domain' && !validDomain(value))
				return _('请输入有效域名，可使用 *.example.com。');
			return true;
		};

		return m.render();
	}
});
