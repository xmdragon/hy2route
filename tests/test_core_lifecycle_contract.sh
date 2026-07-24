#!/bin/sh
set -eu
init=files/etc/init.d/hy2route
grep -Fq 'PROG=/usr/bin/hy2route-core' "$init"
grep -Fq 'procd_open_instance core' "$init"
grep -Fq 'procd_set_param command "$PROG" serve --config "$RUNDIR/core.json"' "$init"
grep -Fq 'procd_set_param limits nofile=' "$init"
grep -Fq '"$PROG" check --config "$RUNDIR/core.json.new"' "$init"
! grep -Eq 'XRAY|CHINADNS|run-xray|xray.json|chinadns.conf' "$init"
echo 'core lifecycle contract passed'
