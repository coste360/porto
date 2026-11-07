#!/usr/bin/env bash
# Packages the Windows x64 per-user installer (Inno Setup). Runs in Git Bash / MSYS2 with a MinGW-w64 gcc
# (CGO for WebView2), windres and Inno Setup 6 (iscc).
#
# Signing hook (docs/desktop-build.md), never faked:
#   WINDOWS_SIGN_CMD   command run with the file path appended (signtool, AzureSignTool…), for the exe and the
#                      installer. Without it the file name says -unsigned.
set -euo pipefail
. "$(dirname "$0")/common.sh"

case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) ;; *) die "package-windows.sh runs in Git Bash or MSYS2 on Windows" ;; esac
require gcc windres go
ISCC="${ISCC:-iscc}"
command -v "$ISCC" >/dev/null 2>&1 || ISCC="/c/Program Files (x86)/Inno Setup 6/ISCC.exe"
[ -x "$ISCC" ] || command -v "$ISCC" >/dev/null 2>&1 || die "Inno Setup 6 (iscc) is required"

sign() {
	[ -n "${WINDOWS_SIGN_CMD:-}" ] || return 0
	log "signing $(basename "$1")"
	# shellcheck disable=SC2086 # the command template is split on purpose
	$WINDOWS_SIGN_CMD "$1"
}

work="$DESKTOP_DIST/windows"
rm -rf "$work"
mkdir -p "$work"

# Icon and version resources: the .syso is picked up by the Go linker and removed afterwards.
go run ./scripts/desktop/mkico -o "$work/porto.ico" \
	"$ICON_KIT/porto_app_navy_64.png" "$ICON_KIT/porto_app_navy_128.png" "$ICON_KIT/porto_app_navy_256.png"
cp "$PACKAGING/windows/porto.exe.manifest" "$work/"
IFS=. read -r vmaj vmin vpat <<<"$NUMERIC_VERSION"
sed -e "s/@VERSION@/$NUMERIC_VERSION/g" -e "s/@VERSION_COMMA@/$vmaj,$vmin,$vpat,0/g" \
	"$PACKAGING/windows/porto.rc.in" > "$work/porto.rc"
syso="cmd/porto-desktop/rsrc_windows_amd64.syso"
trap 'rm -f "$syso"' EXIT
(cd "$work" && windres -O coff -o porto.syso porto.rc)
mv "$work/porto.syso" "$syso"

exe="$work/porto-desktop.exe"
export CC="${CC:-gcc}"
go_build "$exe" windows amd64 webview "-H windowsgui"
rm -f "$syso"
"$exe" -version
sign "$exe"

suffix="-unsigned"
[ -n "${WINDOWS_SIGN_CMD:-}" ] && suffix=""
name="${APP_NAME}_${NUMERIC_VERSION}_windows_amd64_setup${suffix}"
"$ISCC" /Q "/DAppVersion=$NUMERIC_VERSION" "/DSourceDir=$(cygpath -w "$work" 2>/dev/null || echo "$work")" \
	"/DOutputDir=$(cygpath -w "$DESKTOP_DIST" 2>/dev/null || echo "$DESKTOP_DIST")" "/DOutputName=$name" \
	"$PACKAGING/windows/porto.iss"
installer="$DESKTOP_DIST/$name.exe"
sign "$installer"
sha256_file "$installer"
echo "packaged $installer"
