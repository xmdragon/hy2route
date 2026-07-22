#!/bin/sh
set -eu

supervisor="${1:-files/usr/libexec/hy2route/run-xray.sh}"

if [ ! -x "$supervisor" ]; then
	echo "missing executable supervisor: $supervisor" >&2
	exit 1
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/hy2route-supervisor-test.XXXXXX")"
active_pid=''

cleanup() {
	if [ -n "$active_pid" ]; then
		kill "$active_pid" 2>/dev/null || true
		wait "$active_pid" 2>/dev/null || true
	fi
	rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

fake_xray="$tmp_dir/fake-xray"
fake_curl="$tmp_dir/fake-curl"
fake_logger="$tmp_dir/fake-logger"
fake_rss_reader="$tmp_dir/fake-rss-reader"

cat > "$fake_xray" <<'EOF'
#!/bin/sh
printf '%s\n' "$$" >> "$START_LOG"
sleep_pid=''
stop() {
	[ -n "$sleep_pid" ] && kill "$sleep_pid" 2>/dev/null || true
	exit 0
}
trap stop TERM INT
while :; do
	sleep 1 &
	sleep_pid=$!
	wait "$sleep_pid" 2>/dev/null || true
done
EOF

cat > "$fake_curl" <<'EOF'
#!/bin/sh
url=''
for arg do
	url="$arg"
done

case "$CURL_MODE:$url" in
	mixed:*gstatic*) exit 28 ;;
	mixed:*) printf '204'; exit 0 ;;
	fail:*) exit 28 ;;
	success:*) printf '204'; exit 0 ;;
	sequence:*)
		call_count=0
		[ ! -s "$CURL_STATE_FILE" ] || read -r call_count < "$CURL_STATE_FILE"
		call_count=$((call_count + 1))
		printf '%s\n' "$call_count" > "$CURL_STATE_FILE"
		case ",$CURL_SUCCESS_CALLS," in
			*",$call_count,"*) printf '204'; exit 0 ;;
			*) exit 28 ;;
		esac
		;;
	*) exit 2 ;;
esac
EOF

cat > "$fake_logger" <<'EOF'
#!/bin/sh
message=''
for arg do
	message="$arg"
done
printf '%s\n' "$message" >> "$LOG_FILE"
EOF

cat > "$fake_rss_reader" <<'EOF'
#!/bin/sh
case "$RSS_MODE" in
	high) printf '999999\n' ;;
	*) printf '1\n' ;;
esac
EOF

chmod 755 "$fake_xray" "$fake_curl" "$fake_logger" "$fake_rss_reader"

assert_eq() {
	expected="$1"
	actual="$2"
	label="$3"
	if [ "$actual" -ne "$expected" ]; then
		echo "$label: expected $expected, got $actual" >&2
		exit 1
	fi
}

assert_ge() {
	minimum="$1"
	actual="$2"
	label="$3"
	if [ "$actual" -lt "$minimum" ]; then
		echo "$label: expected at least $minimum, got $actual" >&2
		exit 1
	fi
}

require_log() {
	pattern="$1"
	log_file="$2"
	if ! grep -Fq "$pattern" "$log_file"; then
		echo "missing supervisor log: $pattern" >&2
		cat "$log_file" >&2
		exit 1
	fi
}

reject_log() {
	pattern="$1"
	log_file="$2"
	if grep -Fq "$pattern" "$log_file"; then
		echo "unexpected supervisor log: $pattern" >&2
		cat "$log_file" >&2
		exit 1
	fi
}

run_case() {
	case_name="$1"
	curl_mode="$2"
	rss_mode="$3"
	duration="$4"
	cooldown="$5"
	check_interval="$6"
	breach_limit="$7"
	curl_success_calls="${8:-}"

	case_dir="$tmp_dir/$case_name"
	mkdir "$case_dir"
	config="$case_dir/xray.json"
	start_log="$case_dir/starts.log"
	log_file="$case_dir/supervisor.log"
	curl_state_file="$case_dir/curl-state"
	: > "$config"
	: > "$start_log"
	: > "$log_file"
	: > "$curl_state_file"

	START_LOG="$start_log" \
	LOG_FILE="$log_file" \
	CURL_MODE="$curl_mode" \
	CURL_STATE_FILE="$curl_state_file" \
	CURL_SUCCESS_CALLS="$curl_success_calls" \
	RSS_MODE="$rss_mode" \
	HY2ROUTE_XRAY_BIN="$fake_xray" \
	HY2ROUTE_CURL_BIN="$fake_curl" \
	HY2ROUTE_LOGGER_BIN="$fake_logger" \
	HY2ROUTE_RSS_READER_BIN="$fake_rss_reader" \
	HY2ROUTE_RSS_LIMIT_KB=10 \
	HY2ROUTE_CHECK_INTERVAL_SECONDS="$check_interval" \
	HY2ROUTE_BREACH_LIMIT="$breach_limit" \
	HY2ROUTE_HEALTH_INTERVAL_SECONDS=1 \
	HY2ROUTE_HEALTH_TIMEOUT_SECONDS=1 \
	HY2ROUTE_HEALTH_FAILURE_LIMIT=1 \
	HY2ROUTE_HEALTH_RECOVERY_SUCCESS_LIMIT=3 \
	HY2ROUTE_HEALTH_RESTART_COOLDOWN_SECONDS="$cooldown" \
	HY2ROUTE_RESTART_DELAY_SECONDS=0 \
		"$supervisor" "$config" 10780 &
	active_pid=$!

	sleep "$duration"
	kill "$active_pid" 2>/dev/null || true
	wait "$active_pid" 2>/dev/null || true
	active_pid=''

	case_start_count="$(wc -l < "$start_log" | tr -d ' ')"
	case_start_log="$start_log"
	case_log_file="$log_file"
}

run_case mixed-target mixed low 4 10 60 3
assert_eq 1 "$case_start_count" 'one healthy target must keep Xray running'
reject_log 'restart-after-consecutive-health-failures' "$case_log_file"

run_case cooldown-suppression fail low 5 10 60 3
assert_eq 2 "$case_start_count" 'health failure may restart only once during cooldown'
require_log 'restart-suppressed-cooldown' "$case_log_file"

run_case persistent-failure fail low 8 3 60 3
assert_eq 2 "$case_start_count" 'persistent failure must not rearm health restart'
reject_log 'health-restart-rearmed' "$case_log_file"

run_case one-success-is-not-recovery sequence low 8 3 60 3 '3'
assert_eq 2 "$case_start_count" 'one successful round must not rearm health restart'
require_log 'restart-suppressed-unrecovered' "$case_log_file"
reject_log 'health-restart-rearmed' "$case_log_file"

run_case recovery-rearms sequence low 8 3 60 3 '3,4,5'
assert_eq 3 "$case_start_count" 'three successful rounds must rearm health restart'
require_log 'health-restart-rearmed' "$case_log_file"

run_case memory-restart success high 4 10 1 1
assert_ge 2 "$case_start_count" 'RSS protection must still restart Xray'
require_log 'restart-after-consecutive-thresholds' "$case_log_file"

echo 'supervisor tests passed'
