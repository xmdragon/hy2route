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
		prepared_reload|normal_start)
			prepare_config() {
				if grep -q validated "$events"; then
					echo 'configuration prepared twice' >&2; return 1
				fi
				echo validated >> "$events"
			}
			# Stop before filesystem/procd work; only exercise the preparation path.
			ip() {
				case "$*" in
					'rule show') printf '%s\n' "$existing_rule" ;;
					*) echo "ip $*" >> "$events"; return 1 ;;
				esac
			}
			start() { echo started >> "$events"; start_service; }
			if [ "$1" = prepared_reload ]; then
				if reload_service; then exit 1; fi
				grep -q stopped "$events"
			else
				if start_service; then exit 1; fi
			fi
			test "$(grep -c validated "$events")" = 1
			grep -q '^ip route replace ' "$events"
			;;
		changed_mark|changed_table|different_selector)
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
run_case different_selector 102 166 '10066: not from all fwmark 0x66 lookup 166'
run_case different_selector 102 166 '10066: from 192.168.88.0/24 fwmark 0x66 lookup 166'
run_case different_selector 102 166 '10066: from all iif br-lan fwmark 0x66 lookup 166'
run_case different_selector 102 166 '10066: from all fwmark 0x66/0xff lookup 166'
run_case different_selector 102 166 "$rule
$rule"
run_case start_mismatch 102 167 "$rule"
run_case matching 102 166 "$rule"
run_case matching 102 166 "$(printf '10066:\tfrom  all fwmark 0x66 lookup 166')"
run_case absent 102 166 ''
run_case disabled 104 167 "$rule" 0
run_case prepared_reload 102 166 "$rule"
run_case normal_start 102 166 "$rule"
echo 'reload policy-route preflight passed'
