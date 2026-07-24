#!/bin/sh
set -eu
backup= dry=0
while [ $# -gt 0 ]; do case "$1" in --backup) backup=$2;shift 2;;--dry-run) dry=1;shift;;*) exit 2;;esac;done
[ -n "$backup" ] || exit 2
printf '%s\n' "backup=$backup restore-or-remove run-xray.sh"
if [ "$dry" = 1 ]; then exit 0; fi
echo 'rollback requires a verified backup manifest' >&2
exit 1
