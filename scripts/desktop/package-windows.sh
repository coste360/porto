#!/usr/bin/env bash
# Packages the Windows x64 per-user installer (Inno Setup). Runs in Git Bash / MSYS2 with a MinGW-w64 gcc
# (CGO for WebView2), windres and Inno Setup 6 (iscc).
#
# Signing hook (docs/desktop-build.md), never faked:
#   WINDOWS_SIGN_CMD   command run with the file path appended (signtool, AzureSignTool…), for the exe, the
#                      uninstaller and the installer. Without it the file name says -unsigned. The uninstaller and
#                      the installer are signed by Inno Setup itself (SignTool, SignedUninstaller) through
#                      scripts/desktop/inno-sign.sh, which runs the same hook.
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
sign_args=()
if [ -n "${WINDOWS_SIGN_CMD:-}" ]; then
	suffix=""
	# ISCC runs the sign tool with CreateProcess, not through this shell: start bash.exe on inno-sign.sh, which
	# inherits WINDOWS_SIGN_CMD. $q is a quote and $f the quoted path of the file, expanded by ISCC.
	bash_w="$(cygpath -w "$(command -v bash)")"
	hook_w="$(cygpath -w "$DESKTOP_ROOT/scripts/desktop/inno-sign.sh")"
	sign_args=("/DPortoSign=1" "/Sporto=\$q${bash_w}\$q \$q${hook_w}\$q \$f")
fi
name="${APP_NAME}_${NUMERIC_VERSION}_windows_amd64_setup${suffix}"
# ISCC resolves relative paths from the directory of porto.iss, not from here: pass absolute ones.
work_abs="$(cd "$work" && pwd)"
dist_abs="$(cd "$DESKTOP_DIST" && pwd)"
# Every path below is already in Windows form: keep MSYS from rewriting the "/X" switches as paths.
MSYS2_ARG_CONV_EXCL='*' MSYS_NO_PATHCONV=1 "$ISCC" /Q "/DAppVersion=$NUMERIC_VERSION" \
	"/DSourceDir=$(cygpath -w "$work_abs" 2>/dev/null || echo "$work_abs")" \
	"/DOutputDir=$(cygpath -w "$dist_abs" 2>/dev/null || echo "$dist_abs")" "/DOutputName=$name" \
	"${sign_args[@]}" "$PACKAGING/windows/porto.iss"
installer="$DESKTOP_DIST/$name.exe"
sha256_file "$installer"
echo "packaged $installer"
