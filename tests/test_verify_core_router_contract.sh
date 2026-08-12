#!/bin/sh
set -eu

script=tools/verify-core-router.sh
sh -n "$script"
"$script" --dry-run --router 192.168.80.1 --expect legacy | grep -Fq 'legacy verification'
for literal in 'hy2route-core status' '[/]usr/bin/xray' '127.0.0.1#1053' 'core_state' 'hy2_connected' 'hy2_state' 'hy2_last_success' 'idle|connected|degraded' '--expect'; do
	grep -Fq -- "$literal" "$script"
done

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM
cat > "$tmp/ssh" <<'EOF'
#!/bin/sh
case "$*" in
	*'hy2route-core status'*) printf '%s\n' "$FAKE_STATUS" ;;
	*) exit 0 ;;
esac
EOF
chmod +x "$tmp/ssh"

verify_status() {
	FAKE_STATUS="$1" PATH="$tmp:$PATH" "$script" --router 192.168.80.1 --expect core >/dev/null 2>&1
}
verify_status '{"mode":"proxy","hy2_connected":false,"hy2_state":"idle"}'
verify_status '{"mode":"proxy","hy2_connected":true,"hy2_state":"connected","hy2_last_success":"2026-08-11T07:00:00.123Z"}'
verify_status '{"mode":"proxy","hy2_connected":false,"hy2_state":"degraded","hy2_last_error":"2026-08-11T07:01:00Z"}'
for invalid in \
	'{"mode":"proxy","hy2_connected":false,"hy2_state":"connected","hy2_last_success":"2026-08-11T07:00:00Z"}' \
	'{"mode":"proxy","hy2_connected":true,"hy2_state":"connected"}' \
	'{"mode":"proxy","hy2_connected":false,"hy2_state":"degraded"}' \
	'{"mode":"proxy","hy2_connected":false,"hy2_state":"unknown"}'
do
	if verify_status "$invalid"; then
		echo "invalid status accepted: $invalid" >&2
		exit 1
	fi
done
echo 'core router verification contract passed'
