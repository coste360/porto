#!/usr/bin/env bash
# Claude Code PreToolUse hook (matcher Edit|Write|MultiEdit|NotebookEdit).
#
# Reads the hook payload (JSON) on stdin and refuses, with exit code 2 and the reason on stderr, any edit to:
#   - generated Go code             *.gen.go, *_gen.go
#   - the generated TS API client   web/shared/api/generated/**
#   - a migration already on main   internal/migrations/** that exists in main (or origin/main)
#   - brand files                   assets/brand/** (rights reserved, ADR 0007)
#   - the reference repository      anything resolving inside ../arenile (ADR 0001: read-only)
#
# Fail-open by design: unparseable input, a missing path or a missing git repository all exit 0, so the hook can
# never block unrelated work. Override the reference repository location with PORTO_REFERENCE_REPO.
set -u

payload="$(cat 2>/dev/null || true)"

# Prints tool_input.file_path (or .notebook_path / .path) of the payload, or nothing.
target_path() {
	if command -v jq >/dev/null 2>&1; then
		printf '%s' "$payload" |
			jq -r '(.tool_input // {}) | (.file_path // .notebook_path // .path // empty)' 2>/dev/null
	elif command -v python3 >/dev/null 2>&1; then
		printf '%s' "$payload" | python3 -I -c '
import json, sys
try:
    ti = json.load(sys.stdin).get("tool_input") or {}
    print(ti.get("file_path") or ti.get("notebook_path") or ti.get("path") or "")
except Exception:
    pass
' 2>/dev/null
	fi
}

# Canonical absolute form of a path that may not exist yet: resolve the deepest existing ancestor with pwd -P
# (follows symlinks, e.g. /tmp -> /private/tmp on macOS) and append the remaining components.
canonical() {
	local p="$1" rest=""
	while [ ! -d "$p" ]; do
		case "$p" in
		/ | "" | .) break ;;
		esac
		rest="/$(basename "$p")$rest"
		p="$(dirname "$p")"
	done
	if [ -d "$p" ]; then
		printf '%s%s' "$(cd "$p" 2>/dev/null && pwd -P)" "$rest"
	else
		printf '%s%s' "$p" "$rest"
	fi
}

deny() {
	printf 'guard-generated.sh: edit refused for %s\n%s\n' "$shown" "$1" >&2
	exit 2
}

path="$(target_path)"
[ -n "$path" ] || exit 0

root="${CLAUDE_PROJECT_DIR:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
root="$(canonical "${root%/}")"

case "$path" in
/*) abs="$(canonical "$path")" ;;
*) abs="$(canonical "$root/$path")" ;;
esac
shown="$path"

# 1. The reference repository is never modified, whatever the path looks like.
reference="${PORTO_REFERENCE_REPO:-$root/../arenile}"
if [ -d "$reference" ]; then
	reference="$(canonical "$reference")"
	case "$abs" in
	"$reference" | "$reference"/*)
		deny "The Arenile repository is a read-only reference (ADR 0001). Read it to understand a solution, then write Porto's own code here."
		;;
	esac
fi

# 2. Everything else concerns files of this repository only.
case "$abs" in
"$root"/*) rel="${abs#"$root"/}" ;;
*) exit 0 ;;
esac
shown="$rel"

case "$rel" in
*.gen.go | *_gen.go)
	deny "Generated Go code. Change the source (api/openapi.yaml, the generator) and run 'make gen'."
	;;
web/shared/api/generated/*)
	deny "Generated TypeScript API client. Change api/openapi.yaml and run 'make gen'."
	;;
assets/brand/*)
	deny "Brand files are rights reserved and maintained by the orchestrator (ADR 0007). Use them through BrandLogo."
	;;
internal/migrations/*)
	# Ask git only when the project root is the top of a work tree, never an unrelated enclosing repository.
	top="$(git -C "$root" rev-parse --show-toplevel 2>/dev/null || true)"
	if [ -z "$top" ] || [ "$(canonical "$top")" != "$root" ]; then
		exit 0
	fi
	for ref in main origin/main; do
		if git -C "$root" rev-parse --verify --quiet "$ref^{commit}" >/dev/null 2>&1 &&
			git -C "$root" cat-file -e "$ref:$rel" 2>/dev/null; then
			deny "This migration is already on $ref and must never change. Add the next-numbered migration to both internal/migrations/sqlite and internal/migrations/postgres (skill /new-migration)."
		fi
	done
	;;
esac

exit 0
