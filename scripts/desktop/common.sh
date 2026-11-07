#!/usr/bin/env bash
# Shared settings of the desktop build and packaging scripts (docs/desktop-build.md). Sourced, never run.
#
# VERSION           version stamped into the binary (default: scripts/version.sh)
# DESKTOP_DIST      output folder (default: dist/desktop)
set -euo pipefail

DESKTOP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$DESKTOP_ROOT"

VERSION="${VERSION:-$(scripts/version.sh)}"
# Numeric MAJOR.MINOR.PATCH for Info.plist, the Windows resources and the installer (0.0.0 for dev builds).
NUMERIC_VERSION="$(printf '%s' "$VERSION" | sed -nE 's/^v?([0-9]+)\.([0-9]+)\.([0-9]+).*/\1.\2.\3/p')"
NUMERIC_VERSION="${NUMERIC_VERSION:-0.0.0}"
DESKTOP_DIST="${DESKTOP_DIST:-dist/desktop}"
APP_NAME="Porto"
BUNDLE_ID="it.coste360.porto"
ICON_KIT="assets/brand/porto/05_PNG_Icone"
PACKAGING="packaging/desktop"

# Reproducible builds: no paths, no VCS stamp, no build id; SOURCE_DATE_EPOCH from the last commit.
DESKTOP_LDFLAGS="-s -w -buildid= -X main.version=${VERSION}"
export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || echo 0)}"

log() { printf 'desktop: %s\n' "$*" >&2; }
die() { printf 'desktop: error: %s\n' "$*" >&2; exit 1; }

require() {
	for tool in "$@"; do
		command -v "$tool" >/dev/null 2>&1 || die "$tool is required (see docs/desktop-build.md)"
	done
}

# sha256_file FILE: writes FILE.sha256 ("<hex>  <name>").
sha256_file() {
	local f="$1" sum
	if command -v sha256sum >/dev/null 2>&1; then
		sum="$(sha256sum "$f" | cut -d' ' -f1)"
	else
		sum="$(shasum -a 256 "$f" | cut -d' ' -f1)"
	fi
	printf '%s  %s\n' "$sum" "$(basename "$f")" > "$f.sha256"
	log "sha256 $(basename "$f") $sum"
}

# go_build OUT GOOS GOARCH TAGS [EXTRA_LDFLAGS]: builds cmd/porto-desktop; TAGS=webview needs CGO.
go_build() {
	local out="$1" goos="$2" goarch="$3" tags="$4" extra="${5:-}" cgo=0
	[ -n "$tags" ] && cgo=1
	mkdir -p "$(dirname "$out")"
	CGO_ENABLED="$cgo" GOOS="$goos" GOARCH="$goarch" go build -tags "$tags" -trimpath -buildvcs=false \
		-ldflags "$DESKTOP_LDFLAGS $extra" -o "$out" ./cmd/porto-desktop
	log "built $out ($goos/$goarch, tags: ${tags:-none})"
}
