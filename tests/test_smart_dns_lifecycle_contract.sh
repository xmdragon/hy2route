#!/bin/sh
set -eu
init=files/etc/init.d/hy2route
grep -Fq '"$GEN" core > "$RUNDIR/core.json.new"' "$init"
grep -Fq '"$PROG" check --config "$RUNDIR/core.json.new"' "$init"
grep -Fq 'cp "$RUNDIR/dnsmasq.conf" "$DNSMASQ_SNIPPET"' "$init"
grep -Fq 'restore_dnsmasq' "$init"
! grep -Eq 'chinadns|xray' "$init"
echo 'core DNS lifecycle contract passed'
