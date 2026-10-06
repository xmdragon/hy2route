#!/bin/sh
set -eu
init=${1:-files/etc/init.d/hy2route}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM
# Redirect external service calls and runtime directories to fixtures.
sed -e 's|/etc/init.d/dnsmasq restart|dnsmasq_restart|g' \
	-e 's| /tmp/dnsmasq.d | "$RUNDIR/dnsmasq.d" |g' "$init" > "$work/init"

run_case() (
	. "$work/init"
	EVENTS="$work/$1.events"
	export EVENTS
	: > "$EVENTS"
	RUNDIR="$work/$1"
	mkdir "$RUNDIR"
	echo previous > "$RUNDIR/core.json"
	FAIL_VALIDATION=0
	[ "$1" != validation_failure ] || FAIL_VALIDATION=1
	export FAIL_VALIDATION
	DNSMASQ_SNIPPET="$RUNDIR/upstream.conf"
	GEN="$RUNDIR/generator"
	PROG="$RUNDIR/core"
	cat > "$GEN" <<'GENERATOR'
#!/bin/sh
echo prepared >> "$EVENTS"
echo fixture
GENERATOR
	cat > "$PROG" <<'CORE'
#!/bin/sh
echo validated >> "$EVENTS"
[ "$FAIL_VALIDATION" = 0 ]
CORE
	chmod +x "$GEN" "$PROG"
	enabled=1
	config_value() {
		case "$1" in
			enabled) echo "$enabled" ;;
			fwmark) echo 102 ;;
			route_table) echo 166 ;;
		esac
	}
	ip() {
		case "$*" in
			'-N rule show') echo '10066: from all fwmark 0x66 lookup 166' ;;
			*) echo route_installed >> "$EVENTS" ;;
		esac
	}
	nft() {
		case "$1" in
			-f) echo firewall_installed >> "$EVENTS" ;;
		esac
	}
	passwall2_running() { return 1; }
	procd_open_instance() { echo instance_opened >> "$EVENTS"; }
	procd_set_param() { :; }
	procd_close_instance() { echo instance_registered >> "$EVENTS"; }
	logger() { echo "$*" >> "$EVENTS"; }
	# rc.common submits procd's service definition before calling this hook.
	stop() { stop_service; echo core_stopped >> "$EVENTS"; }
	start() {
		start_service
		echo service_submitted >> "$EVENTS"
		if type service_started >/dev/null 2>&1; then service_started; fi
	}
	case_name=$1
	# Keep the mocked external command independent of its service arguments.
	dnsmasq_restart() {
		grep -q service_submitted "$EVENTS" || {
			echo 'DNS restarted before procd submission' >&2; return 1
		}
		echo dnsmasq_restart >> "$EVENTS"
		[ "$case_name" = success ]
	}
	if [ "$case_name" = disabled_hook ]; then
		enabled=0
		service_started
		! grep -q dnsmasq_restart "$EVENTS"
		return
	fi
	if [ "$case_name" = validation_failure ]; then
		if reload_service; then echo 'invalid reload accepted' >&2; exit 1; fi
		! grep -Eq 'core_stopped|instance_registered|firewall_installed' "$EVENTS"
		test "$(cat "$RUNDIR/core.json")" = previous
		return
	fi
	if [ "$case_name" = startup_failure ]; then
		if start; then echo 'DNS activation failure reported as success' >&2; exit 1; fi
	elif [ "$case_name" = success ]; then
		reload_service
		grep -q core_stopped "$EVENTS"
	else
		if reload_service; then echo 'DNS activation failure reported as success' >&2; exit 1; fi
		grep -q core_stopped "$EVENTS"
	fi
	test "$(grep -c prepared "$EVENTS")" = 3
	test "$(grep -c validated "$EVENTS")" = 1
	grep -q firewall_installed "$EVENTS"
	grep -q instance_registered "$EVENTS"
	grep -q service_submitted "$EVENTS"
	grep -q dnsmasq_restart "$EVENTS"
	if [ "$case_name" != success ]; then
		grep -q 'dnsmasq restart failed' "$EVENTS"
	else
		! grep -q 'dnsmasq restart failed' "$EVENTS"
	fi
)

run_case success
run_case reload_failure
run_case startup_failure
run_case validation_failure
run_case disabled_hook
echo 'DNS activation failures propagate while the core remains supervised'
