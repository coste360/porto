#!/usr/bin/env bash
# Builds the desktop launcher into dist/desktop/bin/<goos>_<goarch>/ (make desktop-build).
#
# DESKTOP_TAGS=""        browser launcher, CGO_ENABLED=0 (cross-compiles anywhere)
# DESKTOP_TAGS=webview   window launcher, CGO_ENABLED=1, native toolchain of the host OS
# GOOS / GOARCH          target (default: the host)
set -euo pipefail
. "$(dirname "$0")/common.sh"

goos="${GOOS:-$(go env GOOS)}"
goarch="${GOARCH:-$(go env GOARCH)}"
tags="${DESKTOP_TAGS:-}"
ext=""
[ "$goos" = windows ] && ext=".exe"
out="$DESKTOP_DIST/bin/${goos}_${goarch}/porto-desktop$ext"
go_build "$out" "$goos" "$goarch" "$tags"
echo "built $out $VERSION (tags: ${tags:-none})"
