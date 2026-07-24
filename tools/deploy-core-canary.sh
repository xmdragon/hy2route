#!/bin/sh
set -eu

router= client= binary=build/hy2route-core data=build/hy2route-data.bin backup= keep=0 dry=0
while [ $# -gt 0 ]; do
	case "$1" in
		--router) router=$2; shift 2 ;;
		--client) client=$2; shift 2 ;;
		--binary) binary=$2; shift 2 ;;
		--data) data=$2; shift 2 ;;
		--backup) backup=$2; shift 2 ;;
		--keep) keep=1; shift ;;
		--dry-run) dry=1; shift ;;
		*) echo "usage: $0 --router IPv4 --client IPv4 [--binary PATH] [--data PATH] [--backup PATH] [--keep] [--dry-run]" >&2; exit 2 ;;
	esac
done

case "$router" in ''|*[!0-9.]*|.*|*.) echo 'router must be an IPv4 address' >&2; exit 2;; esac
case "$client" in ''|*[!0-9.]*|.*|*.) echo 'client must be an IPv4 address' >&2; exit 2;; esac
for octet in $(printf '%s' "$router" | tr . ' ') $(printf '%s' "$client" | tr . ' '); do
	case "$octet" in ''|*[!0-9]*) exit 2;; esac
	[ "$octet" -le 255 ] || exit 2
done

stage=/tmp/hy2route-core-canary
printf '%s\n' "router=$router client=$client table=hy2route_canary dns=2053 tproxy=22345"
[ "$dry" = 1 ] && exit 0
[ -r "$binary" ] && [ -r "$data" ] || { echo 'release artifacts are missing' >&2; exit 1; }
[ -r files/usr/libexec/hy2route/generate.uc ] || { echo 'generator is missing' >&2; exit 1; }

remote() { ssh "root@$router" "$@"; }
cleanup() {
	[ "$keep" = 1 ] && return 0
	remote sh -s -- "$stage" <<'REMOTE_CLEANUP' || true
stage=$1
p="$(cat "$stage/core.pid" 2>/dev/null || true)"
if [ -n "$p" ] && [ -r "/proc/$p/cmdline" ] && tr '\000' ' ' < "/proc/$p/cmdline" | grep -Fq "$stage/core"; then
	kill -9 "$p" 2>/dev/null || true
fi
nft delete table inet hy2route_canary 2>/dev/null || true
rm -rf "$stage"
REMOTE_CLEANUP
}
trap cleanup EXIT INT TERM

remote sh -s -- "$stage" "${backup:-/root/hy2route-backup-$(date +%Y%m%d-%H%M%S)-core}" <<'REMOTE_PREPARE'
stage=$1
backup=$2
[ ! -e "$stage" ] || { echo "canary stage already exists: $stage" >&2; exit 1; }
mkdir -p -m 700 "$stage/uci" "$backup"
backup_one() {
	src=$1
	dst="$backup/${src#/}"
	mkdir -p "$(dirname "$dst")"
	if [ -e "$src" ]; then
		cp -p "$src" "$dst"
	else
		: > "$dst.absent"
	fi
}
for file in /etc/config/hy2route /etc/init.d/hy2route /usr/bin/hy2route /usr/bin/hy2route-core /usr/libexec/hy2route/generate.uc /usr/libexec/hy2route/run-xray.sh /www/luci-static/resources/view/hy2route/main.js /usr/share/hy2route/routing.bin /tmp/dnsmasq.d/hy2route.conf; do
	backup_one "$file"
done
cp -p /etc/config/hy2route "$stage/uci/hy2route"
printf '%s\n' "backup=$backup"
REMOTE_PREPARE

tar -C "$(dirname "$binary")" -cf - "$(basename "$binary")" | remote 'tar -C /tmp/hy2route-core-canary -xf -'
tar -C "$(dirname "$data")" -cf - "$(basename "$data")" | remote 'tar -C /tmp/hy2route-core-canary -xf -'
tar -C files/usr/libexec/hy2route -cf - generate.uc | remote 'tar -C /tmp/hy2route-core-canary -xf -'

local_core=$(sha256sum "$binary" | awk '{print $1}')
local_data=$(sha256sum "$data" | awk '{print $1}')
remote_core=$(remote "sha256sum '$stage/$(basename "$binary")' | awk '{print \$1}'")
remote_data=$(remote "sha256sum '$stage/$(basename "$data")' | awk '{print \$1}'")
[ "$local_core" = "$remote_core" ] && [ "$local_data" = "$remote_data" ] || { echo 'remote artifact checksum mismatch' >&2; exit 1; }

remote sh -s -- "$stage" "$client" "$(basename "$binary")" "$(basename "$data")" <<'REMOTE_START'
stage=$1
client=$2
binary=$3
data=$4
mv "$stage/$binary" "$stage/core"
mv "$stage/$data" "$stage/routing.bin"
chmod 700 "$stage/core" "$stage/generate.uc"
uci -c "$stage/uci" set hy2route.main.transparent_port=22345
uci -c "$stage/uci" set hy2route.main.dns_port=2053
uci -c "$stage/uci" set hy2route.main.nft_table=hy2route_canary
uci -c "$stage/uci" set hy2route.main.canary_source="$client"
uci -c "$stage/uci" commit hy2route
HY2ROUTE_UCI_DIR="$stage/uci" HY2ROUTE_CHINA4_FILE=/usr/share/hy2route/china4.nft "$stage/generate.uc" core > "$stage/core.json"
HY2ROUTE_UCI_DIR="$stage/uci" HY2ROUTE_CHINA4_FILE=/usr/share/hy2route/china4.nft "$stage/generate.uc" nft > "$stage/canary.nft"
sed -i "s|/usr/share/hy2route/routing.bin|$stage/routing.bin|" "$stage/core.json"
"$stage/core" check --config "$stage/core.json"
nft -c -f "$stage/canary.nft"
nft delete table inet hy2route_canary 2>/dev/null || true
nft -f "$stage/canary.nft"
: > "$stage/canary.log"
GOMEMLIMIT=64MiB GOGC=50 "$stage/core" serve --config "$stage/core.json" >"$stage/canary.log" 2>&1 &
echo $! > "$stage/core.pid"
sleep 12
p="$(cat "$stage/core.pid")"
kill -0 "$p"
nft list map inet hy2route_canary core_state | grep -Fq 'jump active'
test ! -s "$stage/canary.log"
REMOTE_START

timeout 15 nslookup wechat.com "$router" | grep -Eq 'Address: [0-9]'
timeout 15 nslookup www.google.com "$router" | grep -Eq 'Address: [0-9]'
timeout 30 curl -kfsS -o /dev/null --connect-timeout 8 --max-time 28 https://www.google.com/generate_204
timeout 30 curl -kfsS -o /dev/null --connect-timeout 8 --max-time 28 https://www.wechat.com/
remote "nft list set inet hy2route_canary direct4 | grep -Fq 'elements = {'"
printf '%s\n' 'core canary passed'
