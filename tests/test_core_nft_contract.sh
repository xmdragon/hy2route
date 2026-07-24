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
echo 'core nft contract passed'
