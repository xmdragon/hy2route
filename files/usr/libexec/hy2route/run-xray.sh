#!/bin/sh

PROG="${HY2ROUTE_XRAY_BIN:-/usr/bin/xray}"
CURL="${HY2ROUTE_CURL_BIN:-/usr/bin/curl}"
LOGGER="${HY2ROUTE_LOGGER_BIN:-logger}"
RSS_READER="${HY2ROUTE_RSS_READER_BIN:-}"
CONFIG="$1"
TEST_SOCKS_PORT="${2:-10780}"
RSS_LIMIT_KB="${HY2ROUTE_RSS_LIMIT_KB:-112640}"
CHECK_INTERVAL_SECONDS="${HY2ROUTE_CHECK_INTERVAL_SECONDS:-30}"
BREACH_LIMIT="${HY2ROUTE_BREACH_LIMIT:-3}"
HEALTH_INTERVAL_SECONDS="${HY2ROUTE_HEALTH_INTERVAL_SECONDS:-30}"
HEALTH_TIMEOUT_SECONDS="${HY2ROUTE_HEALTH_TIMEOUT_SECONDS:-10}"
HEALTH_FAILURE_LIMIT="${HY2ROUTE_HEALTH_FAILURE_LIMIT:-3}"
HEALTH_RECOVERY_SUCCESS_LIMIT="${HY2ROUTE_HEALTH_RECOVERY_SUCCESS_LIMIT:-3}"
HEALTH_URLS="${HY2ROUTE_HEALTH_URLS:-https://www.gstatic.com/generate_204 https://cp.cloudflare.com/generate_204}"
HEALTH_RESTART_COOLDOWN_SECONDS="${HY2ROUTE_HEALTH_RESTART_COOLDOWN_SECONDS:-900}"
RESTART_DELAY_SECONDS="${HY2ROUTE_RESTART_DELAY_SECONDS:-5}"

log() {
	"$LOGGER" -p "$1" -t hy2route "$2"
}

fail() {
	log daemon.err "stage=supervisor label=xray elapsed=0s reason=$1"
	exit 2
}

[ -n "$CONFIG" ] || fail 'missing-config-path'
[ -r "$CONFIG" ] || fail 'config-not-readable'
[ -x "$PROG" ] || fail 'xray-not-executable'
[ -x "$CURL" ] || fail 'curl-not-executable'
[ -n "$HEALTH_URLS" ] || fail 'missing-health-targets'

case "$RSS_LIMIT_KB:$CHECK_INTERVAL_SECONDS:$BREACH_LIMIT:$HEALTH_INTERVAL_SECONDS:$HEALTH_TIMEOUT_SECONDS:$HEALTH_FAILURE_LIMIT:$HEALTH_RECOVERY_SUCCESS_LIMIT:$HEALTH_RESTART_COOLDOWN_SECONDS:$RESTART_DELAY_SECONDS:$TEST_SOCKS_PORT" in
	*[!0-9:]*) fail 'invalid-supervisor-number' ;;
esac
[ "$RSS_LIMIT_KB" -gt 0 ] || fail 'invalid-rss-limit'
[ "$CHECK_INTERVAL_SECONDS" -gt 0 ] || fail 'invalid-check-interval'
[ "$BREACH_LIMIT" -gt 0 ] || fail 'invalid-breach-limit'
[ "$HEALTH_INTERVAL_SECONDS" -gt 0 ] || fail 'invalid-health-interval'
[ "$HEALTH_TIMEOUT_SECONDS" -gt 0 ] || fail 'invalid-health-timeout'
[ "$HEALTH_FAILURE_LIMIT" -gt 0 ] || fail 'invalid-health-failure-limit'
[ "$HEALTH_RECOVERY_SUCCESS_LIMIT" -gt 0 ] || fail 'invalid-health-recovery-success-limit'
[ "$HEALTH_RESTART_COOLDOWN_SECONDS" -gt 0 ] || fail 'invalid-health-restart-cooldown'
[ "$TEST_SOCKS_PORT" -gt 0 ] && [ "$TEST_SOCKS_PORT" -le 65535 ] || fail 'invalid-test-socks-port'

child_pid=''

stop_child() {
	[ -n "$child_pid" ] || return 0
	if kill -0 "$child_pid" 2>/dev/null; then
		kill "$child_pid" 2>/dev/null || true
		attempts=0
		while kill -0 "$child_pid" 2>/dev/null && [ "$attempts" -lt 5 ]; do
			sleep 1
			attempts=$((attempts + 1))
		done
		kill -0 "$child_pid" 2>/dev/null && kill -KILL "$child_pid" 2>/dev/null || true
	fi
	wait "$child_pid" 2>/dev/null || true
	child_pid=''
}

handle_stop() {
	trap - TERM INT
	stop_child
	exit 0
}

trap handle_stop TERM INT

read_rss_kb() {
	if [ -n "$RSS_READER" ]; then
		"$RSS_READER" "$1"
		return
	fi
	[ -r "/proc/$1/status" ] || return 1
	awk '/^VmRSS:/ { print $2; exit }' "/proc/$1/status"
}

health_cooldown_remaining=0
health_restart_armed=1
health_recovery_successes=0

while :; do
	"$PROG" run -c "$CONFIG" &
	child_pid=$!
	elapsed=0
	rss_breaches=0
	health_failures=0
	restart_reason=''

	while kill -0 "$child_pid" 2>/dev/null; do
		sleep 1
		elapsed=$((elapsed + 1))

		if [ "$health_cooldown_remaining" -gt 0 ]; then
			health_cooldown_remaining=$((health_cooldown_remaining - 1))
		fi

		if [ $((elapsed % CHECK_INTERVAL_SECONDS)) -eq 0 ]; then
			rss_kb="$(read_rss_kb "$child_pid")"
			case "$rss_kb" in
				''|*[!0-9]*)
					log daemon.err "stage=memory-watch label=xray-rss elapsed=${elapsed}s reason=rss-unavailable"
					restart_reason='memory'
					break
					;;
			esac

			if [ "$rss_kb" -gt "$RSS_LIMIT_KB" ]; then
				rss_breaches=$((rss_breaches + 1))
				log daemon.warn "stage=memory-watch label=xray-rss elapsed=${elapsed}s rss_kb=$rss_kb limit_kb=$RSS_LIMIT_KB breaches=$rss_breaches reason=threshold-exceeded"
			else
				rss_breaches=0
			fi

			if [ "$rss_breaches" -ge "$BREACH_LIMIT" ]; then
				log daemon.err "stage=memory-watch label=xray-rss elapsed=${elapsed}s rss_kb=$rss_kb limit_kb=$RSS_LIMIT_KB breaches=$rss_breaches reason=restart-after-consecutive-thresholds"
				restart_reason='memory'
				break
			fi
		fi

		if [ $((elapsed % HEALTH_INTERVAL_SECONDS)) -eq 0 ]; then
			probe_count=0
			probe_results=''
			health_round_ok=0

			for health_url in $HEALTH_URLS; do
				probe_count=$((probe_count + 1))
				http_code="$("$CURL" -sS --max-time "$HEALTH_TIMEOUT_SECONDS" \
					--socks5-hostname "127.0.0.1:$TEST_SOCKS_PORT" \
					-o /dev/null -w '%{http_code}' "$health_url" 2>/dev/null)"
				curl_status=$?
				probe_result="${http_code:-000}:$curl_status"
				if [ -n "$probe_results" ]; then
					probe_results="$probe_results,$probe_result"
				else
					probe_results="$probe_result"
				fi

				if [ "$curl_status" -eq 0 ] && [ "$http_code" = '204' ]; then
					health_round_ok=1
					break
				fi
			done

			if [ "$health_round_ok" -eq 1 ]; then
				health_failures=0
				if [ "$health_restart_armed" -eq 0 ]; then
					health_recovery_successes=$((health_recovery_successes + 1))
					if [ "$health_cooldown_remaining" -eq 0 ] && [ "$health_recovery_successes" -ge "$HEALTH_RECOVERY_SUCCESS_LIMIT" ]; then
						health_restart_armed=1
						log daemon.notice "stage=health-watch label=chain elapsed=${elapsed}s successes=$health_recovery_successes required=$HEALTH_RECOVERY_SUCCESS_LIMIT reason=health-restart-rearmed"
						health_recovery_successes=0
					fi
				fi
			else
				health_failures=$((health_failures + 1))
				health_recovery_successes=0
				log daemon.warn "stage=health-watch label=chain elapsed=${elapsed}s probes=$probe_count results=$probe_results failures=$health_failures reason=probe-round-failed"
			fi

			if [ "$health_failures" -ge "$HEALTH_FAILURE_LIMIT" ]; then
				if [ "$health_restart_armed" -eq 1 ]; then
					log daemon.err "stage=health-watch label=chain elapsed=${elapsed}s probes=$probe_count results=$probe_results failures=$health_failures cooldown_seconds=$HEALTH_RESTART_COOLDOWN_SECONDS reason=restart-after-consecutive-health-failures"
					health_restart_armed=0
					health_cooldown_remaining="$HEALTH_RESTART_COOLDOWN_SECONDS"
					health_recovery_successes=0
					restart_reason='health'
					break
				fi

				if [ "$health_cooldown_remaining" -gt 0 ]; then
					log daemon.warn "stage=health-watch label=chain elapsed=${elapsed}s probes=$probe_count results=$probe_results failures=$health_failures cooldown_remaining=$health_cooldown_remaining reason=restart-suppressed-cooldown"
				else
					log daemon.warn "stage=health-watch label=chain elapsed=${elapsed}s probes=$probe_count results=$probe_results failures=$health_failures successes=$health_recovery_successes required=$HEALTH_RECOVERY_SUCCESS_LIMIT reason=restart-suppressed-unrecovered"
				fi
				health_failures=0
			fi
		fi
	done

	if [ -n "$restart_reason" ]; then
		stop_child
		sleep "$RESTART_DELAY_SECONDS"
		continue
	fi

	wait "$child_pid"
	status=$?
	child_pid=''
	log daemon.err "stage=process-watch label=xray elapsed=${elapsed}s exit_status=$status reason=process-exited"
	exit "$status"
done
