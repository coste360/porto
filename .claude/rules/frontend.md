---
paths:
  - "web/**"
  - "e2e/**"
---
# Frontend (Porto)

Spec: 29 (real time, offline, i18n), 31.3 (phone and tablet); ADR 0007 (brand); `docs/m0-plan.md` §7.

- Workspaces `web/shared` (`@porto/shared`), `web/admin` (`/admin/`), `web/booking` (`/b/{slug}`).
  Vite + React + TypeScript strict, TanStack Router (code-based route tree) + TanStack Query.
- UI is shadcn/ui only. Add components with the official CLI from `web/shared`:
  `npx shadcn@latest add <name>`, then export them from the barrel. Never copy components from another
  project or paste them by hand (skill `/shadcn`).
- Styling: Tailwind classes on theme tokens only (`globals.css`, from the brand developer tokens). No custom CSS
  files, no `style={}`, no hex/rgb/hsl colours, no arbitrary values (`-[...]`). `web/scripts/css-guard.mjs` fails
  the lint otherwise.
- i18n: every user-facing string is an i18next key; Italian is the source, English is required; both files
  have the same keys (`web/scripts/i18n-check.mjs`). `eslint-plugin-i18next` forbids literal strings in
  `admin/src` and `booking/src`. Problem types are shown via `errors.<slug>`.
- The logo is used only through `BrandLogo` (`web/shared/src/ui/patterns/Brand.tsx`); never redraw, recolour or
  inline the SVG (ADR 0007). Files under `assets/brand/**` are read-only.
- API calls only through the generated `openapi-fetch` client (`web/shared/api/generated`) and Query hooks in
  `web/shared`. No hand-written API types. Cookie writes send `X-CSRF-Token` from the `porto_csrf` cookie.
- Forms: react-hook-form + zod; validation messages are i18n keys.
- SSE (`/api/admin/v1/stream`) only invalidates queries; it carries no data to render.
- Lengths are integer cm and money integer cents in state; format only at the edge.
- Accessibility: keyboard reachable, labelled controls, contrast via tokens, light and dark themes. The a11y
  Playwright spec (axe) must stay free of serious/critical findings.
- Tests: Vitest + Testing Library next to the component; Playwright specs in `e2e/`.
- Done: `npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run test && npm --prefix web run build`.
