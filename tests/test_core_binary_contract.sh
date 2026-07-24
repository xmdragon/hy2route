#!/bin/sh
set -eu
bin="${1:-build/hy2route-core}"
data="${2:-build/hy2route-data.bin}"
test -x "$bin"
test -s "$data"
file "$bin" | grep -Fq 'ARM aarch64'
file "$bin" | grep -Fq 'statically linked'
grep -Fq 'PKG_VERSION:=0.2.0' Makefile
grep -Fq '$(INSTALL_BIN) ./build/hy2route-core $(1)/usr/bin/hy2route-core' Makefile
grep -Fq '$(INSTALL_DATA) ./build/hy2route-data.bin $(1)/usr/share/hy2route/routing.bin' Makefile
! grep -F 'DEPENDS:=' Makefile | grep -Eq 'xray|chinadns'
echo 'core binary contract passed'
