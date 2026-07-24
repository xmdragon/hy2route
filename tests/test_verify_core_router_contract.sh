#!/bin/sh
set -eu

script=tools/verify-core-router.sh
sh -n "$script"
"$script" --dry-run --router 192.168.80.1 --expect legacy | grep -Fq 'legacy verification'
for literal in 'hy2route-core status' '[/]usr/bin/xray' '127.0.0.1#1053' 'core_state' '--expect'; do
	grep -Fq -- "$literal" "$script"
done
echo 'core router verification contract passed'
