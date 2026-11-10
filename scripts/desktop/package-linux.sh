#!/usr/bin/env bash
# Packages the Linux x86_64 AppImage with two launchers: the WebKitGTK 4.1 window build and the browser build;
# AppRun starts the window build when it can start on the host (it probes it with -version, so a missing
# libwebkit2gtk-4.1 or an older glibc are both caught), else the browser one.
# Needs libwebkit2gtk-4.1-dev and libgtk-3-dev for the window build, and appimagetool.
#
# APPIMAGETOOL          path to an appimagetool binary you trust; or
# APPIMAGETOOL_SHA256   expected sha256 of the pinned download below (the script never runs an unverified tool)
# Signing hook (docs/desktop-build.md), never faked:
#   LINUX_SIGN_KEY      GPG key id: appimagetool --sign --sign-key. Without it the file name says -unsigned.
set -euo pipefail
. "$(dirname "$0")/common.sh"

[ "$(uname -s)" = Linux ] || die "package-linux.sh runs on Linux"
[ "$(uname -m)" = x86_64 ] || die "the AppImage is built on x86_64"
require gcc pkg-config
pkg-config --exists webkit2gtk-4.1 || die "libwebkit2gtk-4.1-dev is required for the window build"

APPIMAGETOOL_VERSION="1.9.0"
APPIMAGETOOL_URL="https://github.com/AppImage/appimagetool/releases/download/${APPIMAGETOOL_VERSION}/appimagetool-x86_64.AppImage"

work="$DESKTOP_DIST/linux"
rm -rf "$work"
appdir="$work/Porto.AppDir"
mkdir -p "$appdir/usr/bin" "$appdir/usr/share/metainfo" "$appdir/usr/share/icons/hicolor/256x256/apps"

# webview_go asks pkg-config for webkit2gtk-4.0, which current distributions (Ubuntu 24.04) no longer ship; 4.1 has
# the same API for what the window uses. A local alias lets the window build link against 4.1.
if ! pkg-config --exists webkit2gtk-4.0; then
	pcdir="$work/pkgconfig"
	mkdir -p "$pcdir"
	cat > "$pcdir/webkit2gtk-4.0.pc" <<PC
Name: webkit2gtk-4.0
Description: alias of webkit2gtk-4.1 for webview_go
Version: $(pkg-config --modversion webkit2gtk-4.1)
Requires: webkit2gtk-4.1
PC
	export PKG_CONFIG_PATH="$pcdir${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
fi
go_build "$appdir/usr/bin/porto-desktop-webview" linux amd64 webview
go_build "$appdir/usr/bin/porto-desktop" linux amd64 ""
"$appdir/usr/bin/porto-desktop" -version

install -m 0755 "$PACKAGING/linux/AppRun" "$appdir/AppRun"
cp "$PACKAGING/linux/porto.desktop" "$appdir/porto.desktop"
sed -e "s/@VERSION@/$NUMERIC_VERSION/g" "$PACKAGING/linux/it.coste360.porto.metainfo.xml" \
	> "$appdir/usr/share/metainfo/it.coste360.porto.metainfo.xml"
cp "$ICON_KIT/porto_app_navy_256.png" "$appdir/porto.png"
cp "$ICON_KIT/porto_app_navy_256.png" "$appdir/usr/share/icons/hicolor/256x256/apps/porto.png"

tool="${APPIMAGETOOL:-}"
if [ -z "$tool" ]; then
	[ -n "${APPIMAGETOOL_SHA256:-}" ] || die "set APPIMAGETOOL (a trusted binary) or APPIMAGETOOL_SHA256 (checksum of $APPIMAGETOOL_URL); the AppDir is ready in $appdir"
	require curl sha256sum
	tool="$work/appimagetool"
	curl -fsSL -o "$tool" "$APPIMAGETOOL_URL"
	echo "$APPIMAGETOOL_SHA256  $tool" | sha256sum -c - >/dev/null || die "appimagetool checksum mismatch"
	chmod +x "$tool"
fi

suffix="-unsigned"
sign_args=()
if [ -n "${LINUX_SIGN_KEY:-}" ]; then
	sign_args=(--sign --sign-key "$LINUX_SIGN_KEY")
	suffix=""
fi
out="$DESKTOP_DIST/${APP_NAME}_${NUMERIC_VERSION}_linux_x86_64${suffix}.AppImage"
ARCH=x86_64 APPIMAGE_EXTRACT_AND_RUN=1 "$tool" --no-appstream "${sign_args[@]}" "$appdir" "$out"
sha256_file "$out"
echo "packaged $out"
