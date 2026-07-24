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
canary_table=0
canary_rule=0
printf '%s\n' "router=$router client=$client table=hy2route_canary dns=2053 tproxy=22345"
[ "$dry" = 1 ] && exit 0
[ -r "$binary" ] && [ -r "$data" ] || { echo 'release artifacts are missing' >&2; exit 1; }
[ -r files/usr/libexec/hy2route/generate.uc ] || { echo 'generator is missing' >&2; exit 1; }

remote() { ssh "root@$router" "$@"; }
actual_client=$(remote 'set -- $SSH_CONNECTION; printf "%s" "$1"')
[ "$actual_client" = "$client" ] || { echo "client must match this workstation source: $actual_client" >&2; exit 1; }
cleanup() {
	[ "$keep" = 1 ] && return 0
	remote sh -s -- "$stage" "$canary_table" "$canary_rule" <<'REMOTE_CLEANUP' || true
stage=$1
owns_table=$2
owns_rule=$3
delete_guard() {
	nft -a list chain inet hy2route prerouting_mangle 2>/dev/null | awk '/hy2route-core-canary-guard/ { print $NF }' | while read -r handle; do
		nft delete rule inet hy2route prerouting_mangle handle "$handle" 2>/dev/null || true
	done
}
p="$(cat "$stage/core.pid" 2>/dev/null || true)"
if [ -n "$p" ] && [ -r "/proc/$p/cmdline" ] && tr '\000' ' ' < "/proc/$p/cmdline" | grep -Fq "$stage/core"; then
	kill -9 "$p" 2>/dev/null || true
fi
[ "$owns_table" = 1 ] && nft delete table inet hy2route_canary 2>/dev/null || true
[ "$owns_rule" = 1 ] && ip rule del priority 10065 2>/dev/null || true
delete_guard
rm -rf "$stage"
REMOTE_CLEANUP
}
trap cleanup EXIT INT TERM

remote sh -s -- "$stage" "${backup:-/root/hy2route-backup-$(date +%Y%m%d-%H%M%S)-core}" <<'REMOTE_PREPARE'
stage=$1
backup=$2
[ ! -e "$stage" ] || { echo "canary stage already exists: $stage" >&2; exit 1; }
nft list table inet hy2route_canary >/dev/null 2>&1 && { echo 'canary nft table already exists' >&2; exit 1; }
nft -a list chain inet hy2route prerouting_mangle 2>/dev/null | grep -Fq 'hy2route-core-canary-guard' && { echo 'canary guard already exists' >&2; exit 1; }
[ ! -e "$backup" ] || { echo "backup path already exists: $backup" >&2; exit 1; }
mkdir -p -m 700 "$stage/uci"
mkdir -m 700 "$backup"
required_files='/etc/config/hy2route /etc/init.d/hy2route /usr/bin/hy2route /usr/bin/hy2route-core /usr/libexec/hy2route/generate.uc /usr/libexec/hy2route/run-xray.sh /www/luci-static/resources/view/hy2route/main.js /usr/share/hy2route/routing.bin /tmp/dnsmasq.d/hy2route.conf'
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
for file in $required_files; do
	backup_one "$file"
done
preflight_backup() {
	for file in $required_files; do
		path="$backup/${file#/}"
		[ -f "$path" ] || [ -f "$path.absent" ] || { echo "incomplete backup: $file" >&2; return 1; }
	done
}
preflight_backup
cp -p /etc/config/hy2route "$stage/uci/hy2route"
printf '%s\n' "backup=$backup"
REMOTE_PREPARE

tar -C "$(dirname "$binary")" -cf - "$(basename "$binary")" | remote 'tar -C /tmp/hy2route-core-canary -xf -'
tar -C "$(dirname "$data")" -cf - "$(basename "$data")" | remote 'tar -C /tmp/hy2route-core-canary -xf -'
tar -C files/usr/libexec/hy2route -cf - generate.uc | remote 'tar -C /tmp/hy2route-core-canary -xf -'

local_core=$(sha256sum "$binary" | awk '{print $1}')
local_data=$(sha256sum "$data" | awk '{print $1}')
local_generator=$(sha256sum files/usr/libexec/hy2route/generate.uc | awk '{print $1}')
remote_core=$(remote "sha256sum '$stage/$(basename "$binary")' | awk '{print \$1}'")
remote_data=$(remote "sha256sum '$stage/$(basename "$data")' | awk '{print \$1}'")
remote_generator=$(remote "sha256sum '$stage/generate.uc' | awk '{print \$1}'")
[ "$local_core" = "$remote_core" ] && [ "$local_data" = "$remote_data" ] && [ "$local_generator" = "$remote_generator" ] || { echo 'remote artifact checksum mismatch' >&2; exit 1; }

remote sh -s -- "$stage" "$client" "$(basename "$binary")" "$(basename "$data")" <<'REMOTE_START'
set -eu
stage=$1
client=$2
binary=$3
data=$4
created_table=0
cleanup_remote() {
	[ "$created_table" = 1 ] && nft delete table inet hy2route_canary 2>/dev/null || true
	ip rule del priority 10065 2>/dev/null || true
	nft -a list chain inet hy2route prerouting_mangle 2>/dev/null | awk '/hy2route-core-canary-guard/ { print $NF }' | while read -r handle; do
		nft delete rule inet hy2route prerouting_mangle handle "$handle" 2>/dev/null || true
	done
}
trap cleanup_remote EXIT INT TERM
mv "$stage/$binary" "$stage/core"
mv "$stage/$data" "$stage/routing.bin"
chmod 700 "$stage/core" "$stage/generate.uc"
uci -c "$stage/uci" set hy2route.main.transparent_port=22345
uci -c "$stage/uci" set hy2route.main.dns_port=2053
uci -c "$stage/uci" set hy2route.main.fwmark=103
uci -c "$stage/uci" set hy2route.main.nft_table=hy2route_canary
uci -c "$stage/uci" set hy2route.main.canary_source="$client"
uci -c "$stage/uci" commit hy2route
HY2ROUTE_UCI_DIR="$stage/uci" HY2ROUTE_CHINA4_FILE=/usr/share/hy2route/china4.nft "$stage/generate.uc" core > "$stage/core.json"
HY2ROUTE_UCI_DIR="$stage/uci" HY2ROUTE_CHINA4_FILE=/usr/share/hy2route/china4.nft "$stage/generate.uc" nft > "$stage/canary.nft"
sed -i -e "s|/usr/share/hy2route/routing.bin|$stage/routing.bin|" -e "s|/var/run/hy2route-core.sock|$stage/control.sock|" "$stage/core.json"
"$stage/core" check --config "$stage/core.json"
nft -c -f "$stage/canary.nft"
ip rule add priority 10065 fwmark 103 lookup "$(uci -c "$stage/uci" get hy2route.main.route_table)"
ip route add local 0.0.0.0/0 dev lo table "$(uci -c "$stage/uci" get hy2route.main.route_table)" 2>/dev/null || true
nft insert rule inet hy2route prerouting_mangle position 0 meta mark 103 return comment "hy2route-core-canary-guard"
nft -f "$stage/canary.nft"
created_table=1
: > "$stage/canary.log"
GOMEMLIMIT=64MiB GOGC=50 "$stage/core" serve --config "$stage/core.json" >"$stage/canary.log" 2>&1 &
echo $! > "$stage/core.pid"
sleep 12
p="$(cat "$stage/core.pid")"
kill -0 "$p"
nft list map inet hy2route_canary core_state | grep -Fq 'jump active'
test ! -s "$stage/canary.log"
trap - EXIT INT TERM
REMOTE_START
canary_table=1
canary_rule=1

timeout 15 nslookup wechat.com "$router" | grep -Eq 'Address: [0-9]'
timeout 15 nslookup www.google.com "$router" | grep -Eq 'Address: [0-9]'
timeout 30 curl -kfsS -o /dev/null --connect-timeout 8 --max-time 28 https://www.google.com/generate_204
timeout 30 curl -kfsS -o /dev/null --connect-timeout 8 --max-time 28 https://www.wechat.com/
remote "nft list set inet hy2route_canary direct4 | grep -Fq 'elements = {'"
printf '%s\n' 'core canary passed'
