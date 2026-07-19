#!/usr/bin/ucode
'use strict';

import { cursor } from 'uci';

const uci = cursor(getenv('HY2ROUTE_UCI_DIR'));
const mode = ARGV[0] || '';
const china4_file = getenv('HY2ROUTE_CHINA4_FILE') || '/usr/share/hy2route/china4.nft';

function fail(message) {
	warn('hy2route: ' + message + '\n');
	exit(1);
}

function get_section(type, name) {
	let s = uci.get_all('hy2route', name);
	if (!s || s['.type'] != type)
		fail('missing config ' + type + " '" + name + "'");
	return s;
}

function text(v, fallback) {
	if (v == null || v == '')
		return fallback;
	return '' + v;
}

function number(v, fallback, minimum, maximum, label) {
	let n = int(text(v, fallback));
	if (n < minimum || n > maximum)
		fail(label + ' must be between ' + minimum + ' and ' + maximum);
	return n;
}

function boolean(v, fallback) {
	return text(v, fallback ? '1' : '0') == '1';
}

function is_ipv4(v) {
	if (match(v, /^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$/) == null)
		return false;
	let parts = split(v, '.');
	if (length(parts) != 4)
		return false;
	for (let i = 0; i < 4; i++) {
		let n = int(parts[i]);
		if (n < 0 || n > 255)
			return false;
	}
	return true;
}

function valid_host(v) {
	return match(v, /^[A-Za-z0-9._:-]+$/) != null;
}

function valid_domain(v) {
	return match(v, /^[A-Za-z0-9_][A-Za-z0-9_.-]*[A-Za-z0-9_]$/) != null ||
		match(v, /^[A-Za-z0-9_]$/) != null;
}

function normalize_domain(v) {
	v = text(v, '');
	if (substr(v, 0, 2) == '*.')
		v = substr(v, 2);
	while (substr(v, 0, 1) == '.')
		v = substr(v, 1);
	return lc(v);
}

function nft_join(items) {
	let out = '';
	for (let i = 0; i < length(items); i++)
		out += (i ? ', ' : '') + items[i];
	return out;
}

const main = get_section('main', 'main');
const relay = get_section('hy2', 'relay');
const landing = get_section('landing', 'landing');

const relay_server = text(relay.server, '');
const landing_server = text(landing.server, '');
const landing_type = text(landing.type, 'socks');
const lan_interface = text(main.lan_interface, 'br-lan');
const transparent_port = number(main.transparent_port, 12345, 1, 65535, 'transparent_port');
const test_socks_port = number(main.test_socks_port, 10780, 1, 65535, 'test_socks_port');
const dns_port = number(main.dns_port, 1053, 1, 65535, 'dns_port');
const remote_dns = text(main.remote_dns, '8.8.8.8');
const bootstrap_dns = text(main.bootstrap_dns, '192.168.1.1');
const fwmark = number(main.fwmark, 102, 1, 2147483647, 'fwmark');
const log_level = text(main.log_level, 'warning');
const udp_policy = text(main.udp_policy, 'proxy');
const block_ipv6 = boolean(main.block_ipv6, true);
const congestion = text(relay.congestion, 'bbr');
const bbr_profile = text(relay.bbr_profile, 'standard');

if (!valid_host(relay_server) || !valid_host(landing_server))
	fail('relay and landing server values may contain only letters, digits, dot, colon, underscore and dash');
if (!is_ipv4(remote_dns) || !is_ipv4(bootstrap_dns))
	fail('remote_dns and bootstrap_dns must be valid IPv4 addresses');
if (landing_type != 'socks' && landing_type != 'http')
	fail("landing type must be 'socks' or 'http'");
if (udp_policy != 'proxy' && udp_policy != 'block' && udp_policy != 'direct')
	fail("udp_policy must be 'proxy', 'block' or 'direct'");
if (congestion != 'bbr' && congestion != 'reno')
	fail("congestion must be 'bbr' or 'reno'");
if (bbr_profile != 'conservative' && bbr_profile != 'standard' && bbr_profile != 'aggressive')
	fail("bbr_profile must be 'conservative', 'standard' or 'aggressive'");
if (match(lan_interface, /^[A-Za-z0-9_.:-]+$/) == null)
	fail('lan_interface contains unsupported characters');

let proxy_ips = [];
let direct_ips = [];
let proxy_domains = [];
let direct_domains = [];

uci.foreach('hy2route', 'rule', function(rule) {
	if (!boolean(rule.enabled, true))
		return;

	let action = text(rule.action, '');
	let type = text(rule.type, '');
	let value = text(rule.value, '');

	if (action != 'direct' && action != 'proxy')
		fail("rule action must be 'direct' or 'proxy'");
	if (type != 'ip' && type != 'domain')
		fail("rule type must be 'ip' or 'domain'");

	if (type == 'domain') {
		value = normalize_domain(value);
		if (!valid_domain(value))
			fail("invalid domain rule '" + value + "'");
		push(action == 'proxy' ? proxy_domains : direct_domains, value);
	}
	else {
		if (match(value, /^[0-9.]+(\/[0-9]+)?$/) == null)
			fail("only IPv4 addresses and CIDRs are supported in this release: '" + value + "'");
		push(action == 'proxy' ? proxy_ips : direct_ips, value);
	}
});

function make_landing() {
	let server = {
		address: landing_server,
		port: number(landing.port, 443, 1, 65535, 'landing port')
	};

	let username = text(landing.username, '');
	let password = text(landing.password, '');
	server.password = password;
	if (username != '' || password != '')
		server.users = [ { user: username, pass: password } ];

	return {
		tag: 'chain',
		protocol: landing_type,
		settings: { servers: [ server ] },
		proxySettings: { tag: 'hy2-relay', transportLayer: true },
		mux: { enabled: false }
	};
}

function make_relay() {
	let tls = { serverName: text(relay.sni, relay_server) };
	let pinned = text(relay.pinned_cert_sha256, '');
	if (pinned != '')
		tls.pinnedPeerCertSha256 = pinned;
	else if (boolean(relay.allow_insecure, false)) {
		// Xray >= 26.1.31 removed allowInsecure. Empty verification fields are
		// its compatibility representation for subscriptions using insecure=1.
		tls.pinnedPeerCertSha256 = '';
		tls.verifyPeerCertByName = '';
	}

	return {
		tag: 'hy2-relay',
		protocol: 'hysteria',
		settings: {
			version: 2,
			address: relay_server,
			port: number(relay.port, 443, 1, 65535, 'relay port')
		},
		streamSettings: {
			network: 'hysteria',
			security: 'tls',
			tlsSettings: tls,
			hysteriaSettings: {
				version: 2,
				auth: text(relay.auth, ''),
				udpIdleTimeout: number(relay.udp_idle_timeout, 60, 2, 600, 'udp_idle_timeout')
			},
			finalmask: {
				quicParams: {
					congestion: congestion,
					bbrProfile: bbr_profile,
					maxIdleTimeout: number(relay.max_idle_timeout, 60, 4, 120, 'max_idle_timeout'),
					keepAlivePeriod: number(relay.keep_alive_period, 15, 2, 60, 'keep_alive_period'),
					disablePathMTUDiscovery: boolean(relay.disable_mtu_discovery, false)
				}
			}
		},
		mux: { enabled: false }
	};
}

function emit_xray() {
	let route_rules = [
		{ inboundTag: [ 'dns-proxy' ], outboundTag: 'hy2-relay' },
		{ ip: [ 'geoip:private' ], outboundTag: 'direct' }
	];

	for (let domain in proxy_domains) {
		push(route_rules, { domain: [ 'domain:' + domain ], network: 'udp', outboundTag: 'hy2-relay' });
		push(route_rules, { domain: [ 'domain:' + domain ], network: 'tcp', outboundTag: 'chain' });
	}
	for (let domain in direct_domains)
		push(route_rules, { domain: [ 'domain:' + domain ], outboundTag: 'direct' });
	for (let ip in proxy_ips) {
		push(route_rules, { ip: [ ip ], network: 'udp', outboundTag: 'hy2-relay' });
		push(route_rules, { ip: [ ip ], network: 'tcp', outboundTag: 'chain' });
	}
	for (let ip in direct_ips)
		push(route_rules, { ip: [ ip ], outboundTag: 'direct' });

	push(route_rules, { ip: [ 'geoip:cn' ], outboundTag: 'direct' });
	push(route_rules, {
		inboundTag: [ 'udp-tproxy', 'test-socks' ],
		network: 'udp',
		outboundTag: 'hy2-relay'
	});
	push(route_rules, {
		inboundTag: [ 'tcp-redirect', 'test-socks' ],
		network: 'tcp',
		outboundTag: 'chain'
	});

	let config = {
		log: { loglevel: log_level },
		inbounds: [
			{
				tag: 'tcp-redirect',
				port: transparent_port,
				protocol: 'dokodemo-door',
				settings: { network: 'tcp', followRedirect: true },
				streamSettings: { sockopt: { tproxy: 'redirect' } },
				sniffing: {
					enabled: true,
					routeOnly: true,
					destOverride: [ 'http', 'tls' ]
				}
			},
			{
				tag: 'udp-tproxy',
				port: transparent_port,
				protocol: 'dokodemo-door',
				settings: { network: 'udp', followRedirect: true },
				streamSettings: { sockopt: { tproxy: 'tproxy' } },
				sniffing: {
					enabled: true,
					routeOnly: true,
					destOverride: [ 'quic' ]
				}
			},
			{
				tag: 'dns-proxy',
				listen: '127.0.0.1',
				port: dns_port,
				protocol: 'dokodemo-door',
				settings: {
					address: remote_dns,
					port: 53,
					network: landing_type == 'socks' ? 'tcp,udp' : 'tcp'
				}
			},
			{
				tag: 'test-socks',
				listen: '127.0.0.1',
				port: test_socks_port,
				protocol: 'socks',
				settings: { auth: 'noauth', udp: true },
				sniffing: {
					enabled: true,
					routeOnly: true,
					destOverride: [ 'http', 'tls', 'quic' ]
				}
			}
		],
		outbounds: [
			make_landing(),
			make_relay(),
			{
				tag: 'direct',
				protocol: 'freedom',
				settings: {}
			},
			{ tag: 'block', protocol: 'blackhole', settings: {} }
		],
		routing: {
			domainStrategy: 'IPIfNonMatch',
			domainMatcher: 'hybrid',
			rules: route_rules
		}
	};

	print(sprintf('%J\n', config));
}

function emit_nft() {
	let bypass = [
		'0.0.0.0/8', '10.0.0.0/8', '100.64.0.0/10', '127.0.0.0/8',
		'169.254.0.0/16', '172.16.0.0/12', '192.168.0.0/16',
		'224.0.0.0/4', '240.0.0.0/4'
	];
	if (is_ipv4(relay_server))
		push(bypass, relay_server);
	if (is_ipv4(landing_server))
		push(bypass, landing_server);

	print('table inet hy2route {\n');
	print('\tset bypass4 {\n\t\ttype ipv4_addr\n\t\tflags interval\n\t\tauto-merge\n');
	print('\t\telements = { ' + nft_join(bypass) + ' }\n\t}\n');
	print('\tset china4 {\n\t\ttype ipv4_addr\n\t\tflags interval\n\t\tauto-merge\n\t}\n');
	print('\tset force_proxy4 {\n\t\ttype ipv4_addr\n\t\tflags interval\n\t\tauto-merge\n');
	if (length(proxy_ips))
		print('\t\telements = { ' + nft_join(proxy_ips) + ' }\n');
	print('\t}\n');
	print('\tset force_direct4 {\n\t\ttype ipv4_addr\n\t\tflags interval\n\t\tauto-merge\n');
	if (length(direct_ips))
		print('\t\telements = { ' + nft_join(direct_ips) + ' }\n');
	print('\t}\n');
	if (block_ipv6) {
		print('\tchain block_forward_ipv6 {\n');
		print('\t\ttype filter hook forward priority -1; policy accept;\n');
		print('\t\tiifname "' + lan_interface + '" meta nfproto ipv6 drop\n');
		print('\t}\n');
	}

	print('\tchain prerouting_mangle {\n');
	print('\t\ttype filter hook prerouting priority mangle; policy accept;\n');
	print('\t\tiifname != "' + lan_interface + '" return\n');
	print('\t\tmeta nfproto != ipv4 return\n');
	print('\t\tip daddr @bypass4 return\n');
	print('\t\tip daddr @force_proxy4 meta l4proto udp tproxy ip to :' + transparent_port + ' meta mark set ' + fwmark + ' accept\n');
	print('\t\tip daddr @force_direct4 return\n');
	print('\t\tip daddr @china4 return\n');
	if (udp_policy == 'proxy')
		print('\t\tmeta l4proto udp tproxy ip to :' + transparent_port + ' meta mark set ' + fwmark + ' accept\n');
	else if (udp_policy == 'block')
		print('\t\tmeta l4proto udp drop\n');
	else
		print('\t\tmeta l4proto udp return\n');
	print('\t}\n');

	print('\tchain prerouting_nat {\n');
	print('\t\ttype nat hook prerouting priority dstnat; policy accept;\n');
	print('\t\tiifname != "' + lan_interface + '" return\n');
	print('\t\tmeta nfproto != ipv4 return\n');
	print('\t\tmeta l4proto != tcp return\n');
	print('\t\tip daddr @bypass4 return\n');
	print('\t\tip daddr @force_proxy4 meta l4proto tcp redirect to :' + transparent_port + '\n');
	print('\t\tip daddr @force_direct4 return\n');
	print('\t\tip daddr @china4 return\n');
	print('\t\tmeta l4proto tcp redirect to :' + transparent_port + '\n');
	print('\t}\n');
	print('}\n');
	print('include "' + china4_file + '"\n');
}

function emit_dnsmasq() {
	print('# Generated by hy2route. Changes are discarded.\n');
	print('no-resolv\nstrict-order\n');

	if (!is_ipv4(relay_server))
		print('server=/' + relay_server + '/' + bootstrap_dns + '\n');
	if (!is_ipv4(landing_server) && landing_server != relay_server)
		print('server=/' + landing_server + '/' + bootstrap_dns + '\n');

	for (let domain in direct_domains) {
		print('server=/' + domain + '/' + bootstrap_dns + '\n');
		print('nftset=/' + domain + '/4#inet#hy2route#force_direct4\n');
	}
	for (let domain in proxy_domains)
		print('nftset=/' + domain + '/4#inet#hy2route#force_proxy4\n');

	if (landing_type == 'socks')
		print('server=127.0.0.1#' + dns_port + '\n');
	else
		print('server=' + bootstrap_dns + '\n');
}

if (mode == 'xray')
	emit_xray();
else if (mode == 'nft')
	emit_nft();
else if (mode == 'dnsmasq')
	emit_dnsmasq();
else
	fail("usage: generate.uc <xray|nft|dnsmasq>");
