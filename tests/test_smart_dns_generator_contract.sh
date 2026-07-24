#!/bin/sh
set -eu

generator="files/usr/libexec/hy2route/generate.uc"
config="files/etc/config/hy2route"
luci="files/www/luci-static/resources/view/hy2route/main.js"

require_literal() {
	file="$1"
	literal="$2"
	if ! grep -Fq "$literal" "$file"; then
		echo "missing smart DNS contract in $file: $literal" >&2
		exit 1
	fi
}

require_block() {
	file="$1"
	block="$2"
	normalized_file="$(tr '\n' '\034' < "$file")"
	normalized_block="$(printf '%s' "$block" | tr '\n' '\034')"
	if ! printf '%s' "$normalized_file" | grep -Fq "$normalized_block"; then
		echo "missing smart DNS block in $file: $block" >&2
		exit 1
	fi
}

require_literal "$config" "option smart_dns '1'"
require_literal "$config" "option smart_dns_port '65353'"

require_literal "$generator" "const smart_dns = boolean(main.smart_dns, true);"
require_literal "$generator" "const smart_dns_port = number(main.smart_dns_port, 65353, 1, 65535, 'smart_dns_port');"
require_literal "$generator" "fail('smart_dns_port must differ from transparent_port, test_socks_port and dns_port');"
require_literal "$generator" "print('\\tset china6 {\\n\\t\\ttype ipv6_addr\\n\\t\\tflags interval\\n\\t\\tauto-merge\\n\\t}\\n');"
require_literal "$generator" "function emit_chinadns() {"
require_literal "$generator" "print('bind-addr 127.0.0.1\\n');"
require_literal "$generator" "print('bind-port ' + smart_dns_port + '\\n');"
require_literal "$generator" "print('china-dns ' + bootstrap_dns + '\\n');"
require_literal "$generator" "print('trust-dns tcp://127.0.0.1#' + dns_port + '\\n');"
require_literal "$generator" "print('ipset-name4 inet@hy2route@china4\\n');"
require_literal "$generator" "print('ipset-name6 inet@hy2route@china6\\n');"
require_literal "$generator" "print('no-ipv6\\n');"
require_literal "$generator" "print('cache 1024\\n');"
require_literal "$generator" "print('verdict-cache 1024\\n');"
require_block "$generator" "if (smart_dns)
		print('server=127.0.0.1#' + smart_dns_port + '\\n');
	else if (tcp_relay_enabled || landing_type == 'socks')
		print('server=127.0.0.1#' + dns_port + '\\n');
	else
		print('server=' + bootstrap_dns + '\\n');"
require_literal "$generator" "else if (mode == 'chinadns')"
require_literal "$generator" "fail(\"usage: generate.uc <xray|nft|dnsmasq|chinadns>\");"

require_literal "$luci" "o = s.taboption('general', form.Flag, 'smart_dns', _('智能 DNS'),"
require_literal "$luci" "o.default = o.enabled;"
require_literal "$luci" "['smart_dns_port', _('智能 DNS 端口'), '65353']"

echo 'smart DNS generator contract tests passed'
