#!/bin/sh
set -eu

grep -Fq 'go 1.25.0' go.mod
grep -Fq 'toolchain go1.25.12' go.mod
grep -Fq 'github.com/apernet/hysteria/core/v2 v2.10.0' go.mod
grep -Fq 'github.com/miekg/dns v1.1.72' go.mod
grep -Fq 'github.com/google/nftables v0.3.0' go.mod
grep -Fq "tcp_sessions: number(main.tcp_sessions, 256, 64, 4096, 'tcp_sessions')" files/usr/libexec/hy2route/generate.uc
grep -Fq 'MaxActive: cfg.Limits.TCPSessions' cmd/hy2route-core/application.go
! grep -Fq 'MaxActive: cfg.HY2.MaxConcurrentDials' cmd/hy2route-core/application.go
GOTOOLCHAIN=go1.25.12 go test ./internal/buildinfo
echo 'Go core contract tests passed'
