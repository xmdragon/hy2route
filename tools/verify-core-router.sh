#!/bin/sh
set -eu

router= expect= dry=0
while [ $# -gt 0 ]; do
	case "$1" in
		--router) router=$2; shift 2 ;;
		--expect) expect=$2; shift 2 ;;
		--dry-run) dry=1; shift ;;
		*) echo "usage: $0 --router IPv4 --expect legacy|core [--dry-run]" >&2; exit 2 ;;
	esac
done
case "$router" in ''|*[!0-9.]*|.*|*.) echo 'router must be an IPv4 address' >&2; exit 2;; esac
case "$expect" in legacy|core) ;; *) echo 'expect must be legacy or core' >&2; exit 2;; esac
if [ "$dry" = 1 ]; then
	echo "$expect verification: read-only"
	exit 0
fi

remote() { ssh "root@$router" "$@"; }
valid_timestamp() {
	field=$1
	value="$(printf '%s\n' "$status" | sed -n "s/.*\"$field\":\"\([^\"]*\)\".*/\1/p")"
	printf '%s\n' "$value" | grep -Eq '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?Z$'
	date -u -d "$value" '+%Y-%m-%dT%H:%M:%S' >/dev/null 2>&1
}
case "$expect" in
	legacy)
		remote 'ps w | grep -q "[/]usr/bin/xray"; nft list table inet hy2route >/dev/null; ! pgrep -f "[/]usr/bin/hy2route-core serve --config /tmp/hy2route/core.json" >/dev/null'
		echo 'legacy verification passed: core not cut over'
		;;
	core)
		status="$(remote 'hy2route-core status --socket /var/run/hy2route-core.sock')"
		remote '! ps w | grep -q "[/]usr/bin/xray"; grep -Fq "server=127.0.0.1#1053" /tmp/dnsmasq.d/hy2route.conf; nft list map inet hy2route core_state >/dev/null'
		printf '%s\n' "$status" | grep -Fq '"mode"'
		printf '%s\n' "$status" | grep -Eq '"hy2_state":"(idle|connected|degraded)"'
		case "$status" in
			*'"hy2_state":"connected"'*)
				printf '%s\n' "$status" | grep -Fq '"hy2_connected":true'
				valid_timestamp hy2_last_success
				;;
			*'"hy2_state":"degraded"'*)
				printf '%s\n' "$status" | grep -Fq '"hy2_connected":false'
				valid_timestamp hy2_last_error
				;;
			*'"hy2_state":"idle"'*)
				printf '%s\n' "$status" | grep -Fq '"hy2_connected":false'
				if printf '%s\n' "$status" | grep -Eq '"hy2_last_(success|error)":'; then
					exit 1
				fi
				;;
			*) exit 1 ;;
		esac
		echo 'core verification passed'
		;;
esac
