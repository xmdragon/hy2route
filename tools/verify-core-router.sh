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
case "$expect" in
	legacy)
		remote 'ps w | grep -q "[/]usr/bin/xray"; nft list table inet hy2route >/dev/null; ! pgrep -f "[/]usr/bin/hy2route-core serve --config /tmp/hy2route/core.json" >/dev/null'
		echo 'legacy verification passed: core not cut over'
		;;
	core)
		remote 'hy2route-core status --socket /var/run/hy2route-core.sock >/tmp/hy2route-core-status.json; ! ps w | grep -q "[/]usr/bin/xray"; grep -Fq "server=127.0.0.1#1053" /tmp/dnsmasq.d/hy2route.conf; nft list map inet hy2route core_state >/dev/null; grep -Fq "\"mode\"" /tmp/hy2route-core-status.json'
		echo 'core verification passed'
		;;
esac
