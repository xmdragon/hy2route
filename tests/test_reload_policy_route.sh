#!/bin/sh
set -eu
init=${1:-files/etc/init.d/hy2route}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM

run_case() (
	. "$init"
	events="$work/$1"
	: > "$events"
	requested_mark=$2
	requested_table=$3
	existing_rule=$4
	enabled=${5:-1}
	config_value() {
		case "$1" in
			enabled) echo "$enabled" ;;
			fwmark) echo "$requested_mark" ;;
			route_table) echo "$requested_table" ;;
		esac
	}
	ip() {
		case "$*" in
			'rule show') printf '%s\n' "$existing_rule" ;;
			*) echo "ip $*" >> "$events" ;;
		esac
	}
	logger() { echo diagnostic >> "$events"; }
	live_config="$work/$1.core.json"
	echo original > "$live_config"
	prepare_config() { echo validated >> "$events"; echo rejected > "$live_config"; }
	passwall2_running() { return 1; }
	stop() { echo stopped >> "$events"; }
	start() { echo started >> "$events"; }
	case "$1" in
		changed_mark|changed_table)
			if reload_service; then echo 'incompatible reload accepted' >&2; exit 1; fi
			! grep -Eq 'stopped|started|^ip ' "$events"
			grep -q diagnostic "$events"
			test "$(cat "$live_config")" = original
			;;
		start_mismatch)
			if start_service; then echo 'incompatible start accepted' >&2; exit 1; fi
			! grep -q '^ip ' "$events"
			;;
		disabled)
			reload_service
			grep -q stopped "$events"
			! grep -Eq 'started|validated' "$events"
			;;
		*)
			reload_service
			grep -q stopped "$events"
			grep -q started "$events"
			test "$KEEP_POLICY" = 1
			;;
	esac
)

rule='10066: from all fwmark 0x66 lookup 166'
run_case changed_mark 104 166 "$rule"
run_case changed_table 102 167 "$rule"
run_case changed_table 102 16 "$rule"
run_case start_mismatch 102 167 "$rule"
run_case matching 102 166 "$rule"
run_case absent 102 166 ''
run_case disabled 104 167 "$rule" 0
echo 'reload policy-route preflight passed'
