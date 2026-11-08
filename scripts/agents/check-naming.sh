#!/usr/bin/env bash
# Fails when names from the reference project leak into Porto's code (m0-plan §0.1, ADR 0001).
#
# Porto speaks of "marina", never "establishment"; env vars are PORTO_*, never ARENILE_*; the word "arenile" has no
# place in code, contracts, web sources or deployment files. Docs, ADRs and agent files may mention Arenile.
#
# Uses git grep on tracked and untracked (not ignored) files, so build outputs never produce false hits.
# Usage: scripts/agents/check-naming.sh        exit 0 clean, 1 hits found
set -euo pipefail

cd "$(dirname "$0")/../.."

pattern='arenile|establishment'
# M6b: the desktop e2e specs and their support (remote mode, kiosk, local CA) are code too.
paths=(cmd internal pkg api web/admin/src web/booking/src web/shared/src deploy compose.yaml scripts/desktop packaging/desktop .github/workflows/desktop.yml e2e/tests-desktop e2e/support/desktop.ts)

existing=()
for p in "${paths[@]}"; do
	[ -e "$p" ] && existing+=("$p")
done
if [ "${#existing[@]}" -eq 0 ]; then
	echo "check-naming: nothing to scan yet"
	exit 0
fi

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	hits="$(git grep --untracked -n -I -i -E "$pattern" -- "${existing[@]}" || true)"
else
	hits="$(grep -r -n -I -i -E --exclude-dir=node_modules --exclude-dir=dist "$pattern" "${existing[@]}" || true)"
fi

if [ -n "$hits" ]; then
	echo "check-naming: forbidden names found (use marina / PORTO_* / Porto names):" >&2
	printf '%s\n' "$hits" >&2
	exit 1
fi
echo "check-naming: ok (${existing[*]})"
