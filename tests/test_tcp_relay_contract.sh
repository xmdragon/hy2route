#!/bin/sh
set -eu

generator="files/usr/libexec/hy2route/generate.uc"
config="files/etc/config/hy2route"
luci="files/www/luci-static/resources/view/hy2route/main.js"

require_literal() {
	file="$1"
	literal="$2"
	if ! grep -Fq "$literal" "$file"; then
		echo "missing TCP relay contract in $file: $literal" >&2
		exit 1
	fi
}

require_literal "$generator" "let tcp_relay = uci.get_all('hy2route', 'tcp_relay');"
require_literal "$generator" "const tcp_relay_enabled = tcp_relay != null && boolean(tcp_relay.enabled, false);"
require_literal "$generator" "const tcp_transport_tag = tcp_relay_enabled ? 'tcp-relay' : 'hy2-relay';"
require_literal "$generator" 'function make_tcp_relay() {'
require_literal "$generator" "tag: 'tcp-relay'"
require_literal "$generator" "protocol: 'vless'"
require_literal "$generator" "security: 'reality'"
require_literal "$generator" 'proxySettings: { tag: tcp_transport_tag, transportLayer: true }'
require_literal "$generator" "{ inboundTag: [ 'dns-proxy' ], outboundTag: tcp_transport_tag }"
require_literal "$generator" "push(outbounds, make_tcp_relay());"
require_literal "$generator" 'if (tcp_relay_enabled && is_ipv4(tcp_relay_server))'
require_literal "$generator" "push(bypass, tcp_relay_server);"
require_literal "$generator" "if (tcp_relay_enabled && !is_ipv4(tcp_relay_server))"
require_literal "$generator" "print('server=/' + tcp_relay_server + '/' + bootstrap_dns + '\\n');"

require_literal "$config" "config vless 'tcp_relay'"
require_literal "$config" "option enabled '0'"
require_literal "$config" "option reality_password ''"
require_literal "$config" "option short_id ''"

require_literal "$luci" "s = m.section(form.NamedSection, 'tcp_relay', 'vless', _('VLESS TCP 中转'));"
require_literal "$luci" "o = s.option(form.Flag, 'enabled', _('启用 TCP 中转')"
require_literal "$luci" "o = s.option(form.Value, 'reality_password', _('REALITY 公钥'));"
require_literal "$luci" "o = s.option(form.Value, 'short_id', _('REALITY Short ID'));"

echo 'TCP relay contract tests passed'
