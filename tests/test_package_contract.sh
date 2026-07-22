#!/bin/sh
set -eu

require_literal() {
	file="$1"
	literal="$2"
	if ! grep -Fq "$literal" "$file"; then
		echo "missing package contract in $file: $literal" >&2
		exit 1
	fi
}

require_literal files/etc/init.d/hy2route 'SUPERVISOR=/usr/libexec/hy2route/run-xray.sh'
require_literal files/etc/init.d/hy2route 'GOMEMLIMIT=80MiB'
require_literal files/etc/init.d/hy2route 'procd_set_param command "$SUPERVISOR" "$RUNDIR/xray.json" "$test_port"'
require_literal files/usr/libexec/hy2route/generate.uc "keepAlivePeriod: number(relay.keep_alive_period, 0, 0, 60, 'keep_alive_period')"
require_literal files/etc/config/hy2route "option keep_alive_period '0'"
require_literal files/www/luci-static/resources/view/hy2route/main.js "o.datatype = 'range(0,60)';"
require_literal files/www/luci-static/resources/view/hy2route/main.js "o.default = '0';"
require_literal files/www/luci-static/resources/view/hy2route/main.js "0 表示关闭主动保活"
require_literal Makefile 'PKG_RELEASE:=12'
require_literal Makefile 'files/usr/libexec/hy2route/run-xray.sh'
require_literal Makefile '$(INSTALL_BIN) ./files/usr/libexec/hy2route/run-xray.sh $(1)/usr/libexec/hy2route/run-xray.sh'

echo 'package contract tests passed'
