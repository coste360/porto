#!/usr/bin/env bash
# Runs `go test` on the given packages with the given flags:
#
#   scripts/gotest.sh "<packages>" [go test flags...]
#
# With GOTEST_RERUN_FAILS=N (set only by the main, nightly and manual CI runs, ADR 0019) it runs through gotestsum
# and re-runs each failed test up to N times, never the whole package; a test that passes only on a re-run is listed
# in the log as such and must be fixed. Unset (pull requests, local runs): plain go test, a failure is a failure.
set -euo pipefail

pkgs="${1:?usage: scripts/gotest.sh \"<packages>\" [go test flags...]}"
shift

rerun="${GOTEST_RERUN_FAILS:-0}"
if [ "$rerun" = "0" ]; then
	# shellcheck disable=SC2086 # the package list is space separated on purpose
	exec go test "$@" $pkgs
fi
# gotestsum stops re-running when more than --rerun-fails-max-failures tests failed: many failures are a real break.
exec go tool gotestsum --format testname --rerun-fails="$rerun" --rerun-fails-max-failures=5 \
	--packages="$pkgs" -- "$@"
