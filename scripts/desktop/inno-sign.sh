#!/usr/bin/env bash
# Inno Setup "SignTool" bridge of scripts/desktop/package-windows.sh: ISCC starts this script through bash.exe with
# the Windows path of each file it has to sign (the uninstaller it embeds, then the setup), and the script runs the
# same WINDOWS_SIGN_CMD hook used for porto-desktop.exe, with the path in MSYS form. Fails when the hook fails, so
# ISCC stops instead of shipping an unsigned uninstaller. Never fakes a signature.
set -euo pipefail
file="${1:-}"
[ -n "$file" ] || { echo "inno-sign: missing file" >&2; exit 2; }
[ -n "${WINDOWS_SIGN_CMD:-}" ] || { echo "inno-sign: WINDOWS_SIGN_CMD is not set" >&2; exit 2; }
if command -v cygpath >/dev/null 2>&1; then
	file="$(cygpath -u "$file")"
fi
[ -f "$file" ] || { echo "inno-sign: no such file: $file" >&2; exit 2; }
echo "desktop: signing $(basename "$file") (Inno Setup)" >&2
# shellcheck disable=SC2086 # the command template is split on purpose, as in package-windows.sh
$WINDOWS_SIGN_CMD "$file"
