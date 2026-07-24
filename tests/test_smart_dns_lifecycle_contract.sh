#!/bin/sh
set -eu

init="files/etc/init.d/hy2route"
cli="files/usr/bin/hy2route"

require_literal() {
	file="$1"
	literal="$2"
	if ! grep -Fq "$literal" "$file"; then
		echo "missing smart DNS lifecycle contract in $file: $literal" >&2
		exit 1
	fi
}

require_block() {
	file="$1"
	block="$2"
	normalized_file="$(tr '\n' '\034' < "$file")"
	normalized_block="$(printf '%s' "$block" | tr '\n' '\034')"
	if ! printf '%s' "$normalized_file" | grep -Fq "$normalized_block"; then
		echo "missing smart DNS lifecycle block in $file: $block" >&2
		exit 1
	fi
}

require_literal "$init" 'CHINADNS=/usr/bin/chinadns-ng'
require_literal "$init" 'smart_dns_enabled() {'
require_literal "$init" '"$GEN" chinadns > "$RUNDIR/chinadns.conf" || return 1'
require_literal "$init" '[ -x "$CHINADNS" ] || return 1'
require_literal "$init" 'probe_chinadns() {'
require_literal "$init" '"$CHINADNS" -C "$RUNDIR/chinadns.conf" > "$RUNDIR/chinadns.probe.log" 2>&1 &'
require_literal "$init" 'logger -t hy2route "ChinaDNS startup probe failed: $probe_error"'
require_block "$init" 'if smart_dns_enabled && ! probe_chinadns; then
		remove_network_rules
		return 1
	fi'
require_literal "$init" 'procd_open_instance chinadns'
require_literal "$init" 'procd_set_param command "$CHINADNS" -C "$RUNDIR/chinadns.conf"'
require_literal "$init" 'procd_set_param respawn 3600 5 5'

require_literal "$cli" 'CHINADNS=/usr/bin/chinadns-ng'
require_literal "$cli" '"$GEN" chinadns > "$RUNDIR/chinadns.check.conf" || exit 1'
require_literal "$cli" '[ -x "$CHINADNS" ] || {'
require_literal "$cli" "ps w | grep -q '[c]hinadns-ng -C /tmp/hy2route/chinadns.conf'"

echo 'smart DNS lifecycle contract tests passed'
