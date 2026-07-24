#!/bin/sh
set -eu

deploy=tools/deploy-core-canary.sh
rollback=tools/rollback-core.sh

sh -n "$deploy" "$rollback"
"$deploy" --dry-run --router 192.168.80.1 --client 192.168.80.20 | grep -Fq 'table=hy2route_canary'
"$rollback" --dry-run --backup /root/hy2route-backup-20000101-000000-core | grep -Fq 'restore-or-remove run-xray.sh'

for literal in 'sha256sum' 'canary_source' 'trap cleanup' 'nft delete table inet hy2route_canary' 'GOMEMLIMIT=64MiB' 'ssh "root@$router"'; do
	grep -Fq "$literal" "$deploy"
done

awk '
	/backup_one\(\)/ { in_backup = 1 }
	in_backup && /mkdir -p "\$\(dirname "\$dst"\)"/ { parent_created = 1 }
	in_backup && /if \[ -e "\$src" \]/ { exit(parent_created ? 0 : 1) }
' "$deploy"

for literal in '.absent' 'restore-or-remove run-xray.sh' 'nft delete table inet hy2route'; do
	grep -Fq "$literal" "$rollback"
done

echo 'core canary contract passed'
