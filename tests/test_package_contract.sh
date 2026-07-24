#!/bin/sh
set -eu
grep -Fq 'PKG_VERSION:=0.2.0' Makefile
grep -Fq '$(INSTALL_BIN) ./build/hy2route-core' Makefile
grep -Fq '$(INSTALL_DATA) ./build/hy2route-data.bin' Makefile
! grep -F 'DEPENDS:=' Makefile | grep -Eq 'xray|chinadns'
grep -Fq 'PROG=/usr/bin/hy2route-core' files/etc/init.d/hy2route
! grep -Eq 'XRAY|CHINADNS|run-xray' files/etc/init.d/hy2route
echo 'package contract tests passed'
