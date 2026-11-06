---
paths:
  - "**"
---
# Arenile is a reference, not a source (ADR 0001)

- `../arenile` shares Porto's stack and architectural patterns (single binary, outbox, pure engines, DB
  constraints, SSE, idempotent offline, desktop with webview). Read it to understand how a problem was solved:
  structure, edge cases, which tests are worth having.
- Never copy its files or large chunks of its code. Write Porto's own implementation for the marina domain, with
  Porto's names: `marina` (never `establishment`), `PORTO_*`, `porto_*` cookies, `po_live_` / `po_dev_`,
  `Porto-Signature`, module `github.com/coste360/porto`.
- Never modify anything under `../arenile` (settings deny and the PreToolUse hook block it).
- Copying version numbers of dependencies or actions is fine; copying code, comments or tests is not.
- shadcn/ui components come from the official CLI (`npx shadcn@latest add`), not from Arenile's tree.
- `scripts/agents/check-naming.sh` (CI job `naming`) rejects `arenile` and `establishment` in code, contracts,
  web sources and deployment files.
- When the spec says "come Arenile" it describes a behaviour to reproduce; if the behaviour is unclear, read the
  Arenile spec/ADR first, then its code, and ask when they disagree with Porto's spec.
