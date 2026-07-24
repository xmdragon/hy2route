#!/bin/sh
set -eu

bin="${1:-/tmp/hy2route-core}"
config="${2:-/tmp/hy2route-core-test.json}"

command -v dig >/dev/null 2>&1 || {
	echo 'dig is required for the DNS integration test' >&2
	exit 2
}

"$bin" check --config "$config"
"$bin" serve --config "$config" --dns-only >/tmp/hy2route-core-dns.log 2>&1 &
pid=$!
trap 'kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true' EXIT INT TERM
sleep 1
dig @127.0.0.1 -p 2053 wechat.com A +short | grep -Eq '^[0-9]+\.'
test -z "$(dig @127.0.0.1 -p 2053 wechat.com AAAA +short)"
kill "$pid"
wait "$pid"
trap - EXIT INT TERM
echo 'core DNS integration passed'
