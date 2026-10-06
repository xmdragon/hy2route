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
	config_value() {
		case "$1" in
			enabled) echo 1 ;;
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
	# The rc.common wrappers run these callbacks in the same shell.
	stop() { stop_service; echo core_stopped >> "$EVENTS"; }
	start() { start_service; }
	case_name=$1
	# Keep the mocked external command independent of its service arguments.
	dnsmasq_restart() {
		echo dnsmasq_restart >> "$EVENTS"
		[ "$case_name" = success ]
	}
	if [ "$case_name" = validation_failure ]; then
		if reload_service; then echo 'invalid reload accepted' >&2; exit 1; fi
		! grep -Eq 'core_stopped|instance_registered|firewall_installed' "$EVENTS"
		test "$(cat "$RUNDIR/core.json")" = previous
		return
	fi
	if [ "$case_name" = startup_failure ]; then
		start_service
	else
		reload_service
		grep -q core_stopped "$EVENTS"
	fi
	test "$(grep -c prepared "$EVENTS")" = 3
	test "$(grep -c validated "$EVENTS")" = 1
	grep -q firewall_installed "$EVENTS"
	grep -q instance_registered "$EVENTS"
	grep -q dnsmasq_restart "$EVENTS"
	if [ "$case_name" != success ]; then
		grep -q 'dnsmasq restart failed' "$EVENTS"
	fi
)

run_case success
run_case reload_failure
run_case startup_failure
run_case validation_failure
echo 'core startup and reload survive dnsmasq restart failure'
