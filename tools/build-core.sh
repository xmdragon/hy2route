#!/bin/sh
set -eu
repo="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
out="$repo/build"
mkdir -p "$out"
commit="$(git -C "$repo" rev-parse --short=12 HEAD)"
toolchain="${HY2ROUTE_GO_TOOLCHAIN:-go1.25.12}"
(
	cd "$repo"
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOARM64=v8.0 GOTOOLCHAIN="$toolchain" go build -trimpath \
		-ldflags="-s -w -buildid= -X github.com/xmdragon/hy2route/internal/buildinfo.Version=0.2.0 -X github.com/xmdragon/hy2route/internal/buildinfo.Commit=$commit" \
		-o "$out/hy2route-core" ./cmd/hy2route-core
	GOTOOLCHAIN="$toolchain" go run ./cmd/hy2route-data --domains data/china-domains.txt --ipv4 data/china4.txt --output "$out/hy2route-data.bin"
)
sha256sum "$out/hy2route-core" "$out/hy2route-data.bin"
