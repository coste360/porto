#!/usr/bin/env bash
# Prints the version stamped into the binary: `git describe` of HEAD, or "dev" outside a git checkout.
set -euo pipefail
cd "$(dirname "$0")/.."
git describe --tags --always --dirty 2>/dev/null || echo dev
