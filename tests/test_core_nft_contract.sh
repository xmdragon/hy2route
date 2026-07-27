#!/bin/sh
set -eu
g="${1:-files/usr/libexec/hy2route/generate.uc}"
line() { grep -nF "$1" "$g" | head -n1 | cut -d: -f1; }
proxy="$(line 'ip daddr @force_proxy4 meta l4proto tcp tproxy')"
direct="$(line 'ip daddr @force_direct4 return')"
inspect="$(line 'ip daddr @inspect4 meta l4proto tcp tproxy')"
learned="$(line 'ip daddr @direct4 return')"
china="$(line 'ip daddr @china4 return')"
test "$proxy" -lt "$direct"
test "$direct" -lt "$inspect"
test "$inspect" -lt "$learned"
test "$learned" -lt "$china"
grep -Fq 'fib daddr type local return' "$g"
grep -Fq 'meta mark set 1' "$g"
grep -Fq 'meta mark vmap @core_state' "$g"
grep -Fq 'map output_state {' "$g"
grep -Fq 'type nat hook output priority -100; policy accept;' "$g"
grep -Fq 'meta l4proto != tcp return' "$g"
grep -Fq "meta mark ' + bypass_mark + ' return" "$g"
grep -Fq "number(main.bypass_mark, fwmark + 1" "$g"
grep -Fq 'meta mark vmap @output_state' "$g"
output_start="$(line "print('\\tchain output_active {")"
prerouting_nat_start="$(line "print('\\tchain prerouting_nat {")"
output_block="$(sed -n "${output_start},$((prerouting_nat_start - 2))p" "$g")"
test "$(printf '%s\n' "$output_block" | grep -Fc "meta l4proto tcp redirect to :' + transparent_port")" -eq 3
! printf '%s\n' "$output_block" | grep -Fq 'meta l4proto udp redirect'
test "$(sed -n "$((prerouting_nat_start - 1))p" "$g")" = "	if (canary_source != '') {"
prerouting_nat_end="$(awk -v start="$prerouting_nat_start" 'NR > start && /^[[:space:]]*}$/ { print NR; exit }' "$g")"
prerouting_nat_block="$(sed -n "${prerouting_nat_start},${prerouting_nat_end}p" "$g")"
test "$(printf '%s\n' "$prerouting_nat_block" | grep -Fc "dport 53 redirect to :' + dns_port")" -eq 2
! printf '%s\n' "$prerouting_nat_block" | grep -Fq 'transparent_port'
grep -Fq "if (canary_source == '')" "$g"
grep -Fq 'bypass_mark: bypass_mark' "$g"
grep -Fq 'unix.SO_MARK' internal/transport/socketmark_linux.go
grep -Fq 'unix.SO_ORIGINAL_DST' internal/dataplane/original_destination_linux.go
grep -Fq 'cfg.Firewall.BypassMark' cmd/hy2route-core/application.go
grep -Fq 'output_state' internal/firewall/nft_linux.go
grep -Fq 'output_active' internal/firewall/nft_linux.go
grep -Fq 'buildHeartbeatBatch' internal/firewall/nft_linux.go
grep -Fq 'exec.CommandContext(ctx, "nft", "-f", "-")' internal/firewall/nft_linux.go
test "$(grep -Fc 'exec.CommandContext(ctx, "nft"' internal/firewall/nft_linux.go)" -eq 1
! grep -Fq 'delete element inet' internal/firewall/nft_linux.go
grep -Fq 'flush map inet' internal/firewall/heartbeat.go
grep -Fq "const nft_priority = canary_source != '' ? ' - 10' : '';" "$g"
grep -Fq "priority mangle' + nft_priority" "$g"
grep -Fq "priority dstnat' + nft_priority" "$g"
echo 'core nft contract passed'
