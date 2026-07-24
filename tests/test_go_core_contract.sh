#!/bin/sh
set -eu

grep -Fq 'go 1.25.0' go.mod
grep -Fq 'toolchain go1.25.12' go.mod
grep -Fq 'github.com/apernet/hysteria/core/v2 v2.10.0' go.mod
grep -Fq 'github.com/miekg/dns v1.1.72' go.mod
grep -Fq 'github.com/google/nftables v0.3.0' go.mod
GOTOOLCHAIN=go1.25.12 go test ./internal/buildinfo
echo 'Go core contract tests passed'
