#!/usr/bin/env bash
# Packages Porto.app and a .dmg for macOS (universal: arm64 + amd64, window build). Needs macOS with the Xcode
# command line tools (clang, lipo, iconutil, codesign, hdiutil).
#
# Signing hooks (docs/desktop-build.md), never faked:
#   MACOS_SIGN_IDENTITY    "Developer ID Application: …": hardened-runtime signature with a timestamp
#   MACOS_NOTARY_PROFILE   notarytool keychain profile (needs the identity): notarisation and stapling
#   MACOS_NOTARY_TIMEOUT   longest wait for Apple's verdict (notarytool --timeout syntax, default 45m). A submission
#                          still pending after it fails the build with its id, so it can be inspected and retried.
# Without them the app gets the ad-hoc signature Apple Silicon requires and the file name says -unsigned.
set -euo pipefail
. "$(dirname "$0")/common.sh"

[ "$(uname -s)" = Darwin ] || die "package-macos.sh runs on macOS"
require clang lipo iconutil codesign hdiutil plutil
if [ -n "${MACOS_NOTARY_PROFILE:-}" ] && [ -z "${MACOS_SIGN_IDENTITY:-}" ]; then
	die "MACOS_NOTARY_PROFILE needs MACOS_SIGN_IDENTITY"
fi

# notarize DMG: submits to Apple and waits at most MACOS_NOTARY_TIMEOUT. Only "Accepted" passes; "Invalid" or
# "Rejected" print Apple's log, a submission still "In Progress" after the timeout is a distinct failure.
notarize() {
	local file="$1" timeout="${MACOS_NOTARY_TIMEOUT:-45m}" out rc=0 status id
	out="$work/notary-submit.json"
	log "notarising (waiting at most $timeout)"
	xcrun notarytool submit "$file" --keychain-profile "$MACOS_NOTARY_PROFILE" --wait --timeout "$timeout" \
		--output-format json > "$out" || rc=$?
	status="$(plutil -extract status raw -o - "$out" 2>/dev/null || true)"
	id="$(plutil -extract id raw -o - "$out" 2>/dev/null || true)"
	case "$status" in
	Accepted)
		[ "$rc" -eq 0 ] || die "notarytool exited with $rc although the submission ${id:-?} was accepted"
		log "notarisation accepted (${id})"
		;;
	"In Progress" | "")
		if [ -z "$id" ]; then
			cat "$out" >&2 || true
			die "notarytool submit failed (exit $rc) before a submission id was returned"
		fi
		die "notarisation of submission $id still pending after $timeout: check it later with \"xcrun notarytool info $id --keychain-profile <profile>\" and re-run the packaging (or raise MACOS_NOTARY_TIMEOUT)"
		;;
	*)
		xcrun notarytool log "$id" --keychain-profile "$MACOS_NOTARY_PROFILE" >&2 || true
		die "notarisation of submission ${id:-?} ended with status \"$status\""
		;;
	esac
}

work="$DESKTOP_DIST/macos"
rm -rf "$work"
mkdir -p "$work"

# Universal binary from two native-toolchain builds.
for arch in arm64 amd64; do
	carch="$arch"
	[ "$arch" = amd64 ] && carch=x86_64
	export CC="clang -arch $carch" MACOSX_DEPLOYMENT_TARGET=11.0
	go_build "$work/porto-desktop-$arch" darwin "$arch" webview
done
lipo -create -output "$work/porto-desktop" "$work/porto-desktop-arm64" "$work/porto-desktop-amd64"

app="$work/$APP_NAME.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$work/porto-desktop" "$app/Contents/MacOS/porto-desktop"
sed -e "s/@VERSION@/$NUMERIC_VERSION/g" -e "s/@BUNDLE_ID@/$BUNDLE_ID/g" \
	"$PACKAGING/macos/Info.plist.in" > "$app/Contents/Info.plist"

# Icon from the brand kit PNGs, copied as they are (ADR 0007).
iconset="$work/porto.iconset"
mkdir -p "$iconset"
cp "$ICON_KIT/porto_app_navy_64.png" "$iconset/icon_32x32@2x.png"
cp "$ICON_KIT/porto_app_navy_128.png" "$iconset/icon_128x128.png"
cp "$ICON_KIT/porto_app_navy_256.png" "$iconset/icon_128x128@2x.png"
cp "$ICON_KIT/porto_app_navy_256.png" "$iconset/icon_256x256.png"
cp "$ICON_KIT/porto_app_navy_512.png" "$iconset/icon_256x256@2x.png"
cp "$ICON_KIT/porto_app_navy_512.png" "$iconset/icon_512x512.png"
cp "$ICON_KIT/porto_app_navy_1024.png" "$iconset/icon_512x512@2x.png"
iconutil -c icns -o "$app/Contents/Resources/porto.icns" "$iconset"

suffix=""
if [ -n "${MACOS_SIGN_IDENTITY:-}" ]; then
	log "signing with the configured identity"
	codesign --force --options runtime --timestamp --entitlements "$PACKAGING/macos/entitlements.plist" \
		--sign "$MACOS_SIGN_IDENTITY" "$app/Contents/MacOS/porto-desktop"
	codesign --force --options runtime --timestamp --entitlements "$PACKAGING/macos/entitlements.plist" \
		--sign "$MACOS_SIGN_IDENTITY" "$app"
	[ -n "${MACOS_NOTARY_PROFILE:-}" ] || suffix="-unnotarized"
else
	log "no MACOS_SIGN_IDENTITY: ad-hoc signature (unsigned package)"
	codesign --force --sign - "$app/Contents/MacOS/porto-desktop"
	codesign --force --sign - "$app"
	suffix="-unsigned"
fi
codesign --verify --strict "$app"

dmg="$DESKTOP_DIST/${APP_NAME}_${NUMERIC_VERSION}_macos_universal${suffix}.dmg"
stage="$work/dmg"
mkdir -p "$stage"
cp -R "$app" "$stage/"
ln -s /Applications "$stage/Applications"
rm -f "$dmg"
hdiutil create -volname "$APP_NAME" -srcfolder "$stage" -ov -format UDZO "$dmg" >/dev/null

if [ -n "${MACOS_SIGN_IDENTITY:-}" ]; then
	codesign --force --timestamp --sign "$MACOS_SIGN_IDENTITY" "$dmg"
	if [ -n "${MACOS_NOTARY_PROFILE:-}" ]; then
		notarize "$dmg"
		xcrun stapler staple "$dmg"
	fi
fi
sha256_file "$dmg"
"$app/Contents/MacOS/porto-desktop" -version
echo "packaged $dmg"
