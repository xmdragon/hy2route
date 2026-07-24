#!/bin/sh
set -eu

backup= dry=0
while [ $# -gt 0 ]; do
	case "$1" in
		--backup) backup=$2; shift 2 ;;
		--dry-run) dry=1; shift ;;
		*) echo "usage: $0 --backup PATH [--dry-run]" >&2; exit 2 ;;
	esac
done
[ -n "$backup" ] || { echo 'backup is required' >&2; exit 2; }
printf '%s\n' "backup=$backup restore-or-remove run-xray.sh"
[ "$dry" = 1 ] && exit 0
[ -d "$backup" ] || { echo 'verified backup directory is missing' >&2; exit 1; }

/etc/init.d/hy2route stop 2>/dev/null || true
nft delete table inet hy2route 2>/dev/null || true
restore_or_remove() {
	src=$1
	dst=/$1
	if [ -f "$backup/$src.absent" ]; then
		rm -f "$dst"
	elif [ -f "$backup/$src" ]; then
		mkdir -p "$(dirname "$dst")"
		cp -p "$backup/$src" "$dst"
	else
		echo "missing backup entry: $src" >&2
		exit 1
	fi
}
for file in etc/config/hy2route etc/init.d/hy2route usr/bin/hy2route usr/bin/hy2route-core usr/libexec/hy2route/generate.uc usr/libexec/hy2route/run-xray.sh www/luci-static/resources/view/hy2route/main.js usr/share/hy2route/routing.bin tmp/dnsmasq.d/hy2route.conf; do
	restore_or_remove "$file"
done
/etc/init.d/hy2route start
/usr/bin/hy2route status
