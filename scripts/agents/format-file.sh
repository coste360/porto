#!/usr/bin/env bash
# Claude Code PostToolUse hook (matcher Edit|Write|MultiEdit).
#
# Formats the file named in the hook payload (JSON on stdin):
#   *.go (hand-written)                         gofumpt, then goimports -local github.com/coste360/porto
#   web/** ts tsx js jsx mjs cjs css json md    Prettier from web/node_modules
#   html yaml yml
#
# Best effort: a missing formatter, a formatter error or odd input prints a note on stderr and the hook still
# exits 0, so it never fails the tool call.
set -u

readonly module="github.com/coste360/porto"

payload="$(cat 2>/dev/null || true)"

note() { printf 'format-file.sh: %s\n' "$*" >&2; }

target_path() {
	if command -v jq >/dev/null 2>&1; then
		printf '%s' "$payload" | jq -r '(.tool_input // {}) | (.file_path // .path // empty)' 2>/dev/null
	elif command -v python3 >/dev/null 2>&1; then
		printf '%s' "$payload" | python3 -I -c '
import json, sys
try:
    ti = json.load(sys.stdin).get("tool_input") or {}
    print(ti.get("file_path") or ti.get("path") or "")
except Exception:
    pass
' 2>/dev/null
	fi
}

path="$(target_path)"
[ -n "$path" ] || exit 0

root="${CLAUDE_PROJECT_DIR:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
root="$(cd "${root%/}" 2>/dev/null && pwd -P)" || exit 0

case "$path" in
/*) abs="$path" ;;
*) abs="$root/${path#./}" ;;
esac
[ -f "$abs" ] || exit 0
abs="$(cd "$(dirname "$abs")" 2>/dev/null && pwd -P)/$(basename "$abs")"

# Only files of this repository; never generated code or dependency/build directories.
case "$abs" in
"$root"/*) rel="${abs#"$root"/}" ;;
*) exit 0 ;;
esac
case "$rel" in
*.gen.go | *_gen.go | web/shared/api/generated/* | */node_modules/* | */dist/* | internal/http/static/*) exit 0 ;;
esac

format_go() {
	if ! command -v go >/dev/null 2>&1; then
		note "go not found; $rel left unformatted"
		return
	fi
	(cd "$root" && go tool gofumpt -w "$abs") >/dev/null 2>&1 || note "gofumpt failed or unavailable on $rel"
	(cd "$root" && go tool goimports -local "$module" -w "$abs") >/dev/null 2>&1 ||
		note "goimports failed or unavailable on $rel"
}

format_web() {
	local prettier="$root/web/node_modules/.bin/prettier"
	if [ ! -x "$prettier" ]; then
		note "prettier not installed (make setup); $rel left unformatted"
		return
	fi
	(cd "$root/web" && "$prettier" --write --log-level warn "$abs") >/dev/null 2>&1 ||
		note "prettier failed on $rel"
}

case "$rel" in
*.go) format_go ;;
web/*)
	case "$rel" in
	*.ts | *.tsx | *.js | *.jsx | *.mjs | *.cjs | *.css | *.json | *.md | *.html | *.yaml | *.yml) format_web ;;
	esac
	;;
esac

exit 0
