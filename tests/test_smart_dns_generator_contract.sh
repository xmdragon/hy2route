#!/bin/sh
set -eu
g=files/usr/libexec/hy2route/generate.uc
grep -Fq 'function emit_core() {' "$g"
grep -Fq "domestic_dns: bootstrap_dns + ':53'" "$g"
grep -Fq "trusted_dns: remote_dns + ':53'" "$g"
grep -Fq "data: { routing: '/usr/share/hy2route/routing.bin' }" "$g"
grep -Fq 'else if (mode == '\''core'\'')' "$g"
! grep -F 'fail("usage:' "$g" | grep -Fq '<xray|nft|dnsmasq|chinadns>'
echo 'core DNS generator contract passed'
