#!/bin/sh
set -eu

generator="${1:-files/usr/libexec/hy2route/generate.uc}"

require_literal() {
	if ! grep -Fq "$1" "$generator"; then
		echo "missing routing contract: $1" >&2
		exit 1
	fi
}

require_block() {
	normalized_generator="$(tr '\n' '\034' < "$generator")"
	normalized_needle="$(printf '%s' "$1" | tr '\n' '\034')"
	if ! printf '%s' "$normalized_generator" | grep -Fq "$normalized_needle"; then
		echo "missing routing contract block: $1" >&2
		exit 1
	fi
}

reject_literal() {
	if grep -Fq "$1" "$generator"; then
		echo "obsolete routing contract remains: $1" >&2
		exit 1
	fi
}

require_literal "push(route_rules, { domain: [ 'domain:' + domain ], network: 'udp', outboundTag: 'hy2-relay' });"
require_literal "push(route_rules, { domain: [ 'domain:' + domain ], network: 'tcp', outboundTag: 'chain' });"
require_literal "push(route_rules, { ip: [ ip ], network: 'udp', outboundTag: 'hy2-relay' });"
require_literal "push(route_rules, { ip: [ ip ], network: 'tcp', outboundTag: 'chain' });"
require_block "push(route_rules, {
		inboundTag: [ 'udp-tproxy', 'test-socks' ],
		network: 'udp',
		outboundTag: 'hy2-relay'
	});"
require_block "push(route_rules, {
		inboundTag: [ 'tcp-redirect', 'test-socks' ],
		network: 'tcp',
		outboundTag: 'chain'
	});"
require_literal "settings: { auth: 'noauth', udp: true }"
require_literal "ip daddr @force_proxy4 meta l4proto udp tproxy"
require_block "if (udp_policy == 'proxy')
		print('\\t\\tmeta l4proto udp tproxy ip to :' + transparent_port + ' meta mark set ' + fwmark + ' accept\\n');
	else if (udp_policy == 'block')
		print('\\t\\tmeta l4proto udp drop\\n');
	else
		print('\\t\\tmeta l4proto udp return\\n');"
reject_literal "inboundTag: [ 'tcp-redirect', 'udp-tproxy', 'test-socks' ]"
reject_literal "HTTP landing cannot carry UDP"
reject_literal "landing_type == 'socks' && udp_policy == 'proxy'"
reject_literal "ip daddr @force_proxy4 meta l4proto udp drop"

line_number() {
	grep -nF "$1" "$generator" | head -n 1 | cut -d: -f1
}

force_proxy_line="$(line_number 'ip daddr @force_proxy4 meta l4proto udp tproxy')"
force_direct_line="$(line_number 'ip daddr @force_direct4 return')"
china_line="$(line_number 'ip daddr @china4 return')"
policy_line="$(line_number "if (udp_policy == 'proxy')")"

if [ "$force_proxy_line" -ge "$force_direct_line" ] ||
	[ "$force_direct_line" -ge "$china_line" ] ||
	[ "$china_line" -ge "$policy_line" ]; then
	echo 'invalid nft UDP rule order: force proxy, force direct, China bypass, policy' >&2
	exit 1
fi
