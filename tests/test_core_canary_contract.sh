#!/bin/sh
set -eu

deploy=tools/deploy-core-canary.sh
rollback=tools/rollback-core.sh

sh -n "$deploy" "$rollback"
"$deploy" --dry-run --router 192.168.80.1 --client 192.168.80.20 | grep -Fq 'table=hy2route_canary'
"$rollback" --dry-run --backup /root/hy2route-backup-20000101-000000-core | grep -Fq 'restore-or-remove run-xray.sh'

for literal in 'sha256sum' 'local_generator' 'canary_source' 'trap cleanup' 'nft list table inet hy2route_canary' 'GOMEMLIMIT=64MiB' 'ssh "root@$router"' '[ ! -e "$backup" ]'; do
	grep -Fq "$literal" "$deploy"
done

awk '
	/backup_one\(\)/ { in_backup = 1; saw_backup = 1 }
	in_backup && /mkdir -p "\$\(dirname "\$dst"\)"/ { parent_created = 1 }
	in_backup && /if \[ -e "\$src" \]/ { saw_if = 1; exit(parent_created ? 0 : 1) }
	END { exit(saw_backup && saw_if && parent_created ? 0 : 1) }
' "$deploy"

for literal in '.absent' 'restore-or-remove run-xray.sh' 'nft delete table inet hy2route' 'preflight_backup'; do
	grep -Fq "$literal" "$rollback"
done

awk '
	/preflight_backup\(\)/ { preflight = NR }
	/\/etc\/init.d\/hy2route stop/ { stop = NR }
	END { exit(preflight && stop && preflight < stop ? 0 : 1) }
' "$rollback"

echo 'core canary contract passed'
