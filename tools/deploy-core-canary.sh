#!/bin/sh
set -eu
router= client= binary=build/hy2route-core data=build/hy2route-data.bin dry=0
while [ $# -gt 0 ]; do case "$1" in --router) router=$2;shift 2;;--client) client=$2;shift 2;;--binary) binary=$2;shift 2;;--data) data=$2;shift 2;;--dry-run) dry=1;shift;;*) exit 2;;esac;done
[ -n "$router" ] && [ -n "$client" ] || exit 2
printf '%s\n' "router=$router client=$client table=hy2route_canary dns=2053 tproxy=22345"
if [ "$dry" = 1 ]; then exit 0; fi
echo 'Use the staged, source-isolated canary procedure; production files are not replaced by this helper.' >&2
exit 1
