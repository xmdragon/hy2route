#!/bin/sh
set -eu
generator=${1:-files/usr/libexec/hy2route/generate.uc}
work=$(mktemp -d /tmp/hy2route-guard-test.XXXXXX)
trap 'rm -rf "$work"' EXIT INT TERM
mkdir "$work/uci"
: > "$work/china4.nft"
cat > "$work/uci/hy2route" <<'CONFIG'
config main 'main'
 option nft_table 'hy2route_guard_test'
 option fail_open '0'
config hy2 'relay'
 option server '203.0.113.1'
 option auth 'test-only'
config landing 'landing'
 option type 'socks'
 option server '203.0.113.2'
 option udp '1'
 option udp_max_payload '2038'
CONFIG
export HY2ROUTE_UCI_DIR="$work/uci" HY2ROUTE_CHINA4_FILE="$work/china4.nft"
"$generator" core > "$work/core.json"
test "$(jsonfilter -i "$work/core.json" -e '@.fail_open')" = false
test "$(jsonfilter -i "$work/core.json" -e '@.landing.udp')" = true
test "$(jsonfilter -i "$work/core.json" -e '@.landing.max_udp_payload')" = 2038
"$generator" nft > "$work/nft.conf"
nft -c -f "$work/nft.conf"
grep -q 'jump fail_closed' "$work/nft.conf"
grep -q 'jump output_fail_closed' "$work/nft.conf"
grep -q 'type filter hook output priority -110' "$work/nft.conf"
grep -q 'meta mark vmap @output_guard_state' "$work/nft.conf"
# Failure chains preserve explicit-direct and domestic routing precedence.
awk '/chain fail_closed \{/,/^\t}/' "$work/nft.conf" > "$work/failure.conf"
grep -q 'ip daddr @force_proxy4.*drop' "$work/failure.conf"
grep -q 'ip daddr @force_direct4 return' "$work/failure.conf"
grep -q 'ip daddr @china4 return' "$work/failure.conf"
uci -c "$work/uci" set hy2route.main.bypass_mark=1
uci -c "$work/uci" commit hy2route
for mode in core nft; do
	if "$generator" "$mode" > "$work/invalid.$mode" 2> "$work/error"; then
		echo 'reserved heartbeat selector accepted as bypass mark' >&2; exit 1
	fi
	test ! -s "$work/invalid.$mode"
	grep -q bypass_mark "$work/error"
done
echo 'fail-closed generated configuration passed'
