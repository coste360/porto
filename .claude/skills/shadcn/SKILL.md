---
name: shadcn
description: Add or use shadcn/ui components in Porto's web workspaces with the official CLI, exported from the @porto/shared barrel, styled only with theme tokens. Use whenever a screen needs a UI component that is not in web/shared yet.
argument-hint: "[component ...]"
---
# shadcn/ui in Porto: $ARGUMENTS

Read first: `.claude/rules/frontend.md`, `web/shared/components.json`, `docs/m0-plan.md` §7.2 and §7.4.

1. **Check first** whether the component already exists: `ls web/shared/src/ui` and the barrel export
   (`web/shared/src/ui/index.ts`). Reuse before adding.
2. **Add with the CLI only**, from the shared workspace:
   ```bash
   cd web/shared && npx shadcn@latest add $ARGUMENTS
   ```
   Never copy component files from another project (including Arenile) or from the docs website by hand. If the
   CLI asks to overwrite an existing file, answer no and investigate.
3. **Export** each new component from the barrel so that `admin` and `booking` import from `@porto/shared`.
   Apps never import from `web/shared/src/ui/...` paths directly.
4. **Styling.** Keep the generated classes; customise only with variants and Tailwind classes on theme tokens
   (`bg-primary`, `text-muted-foreground`, `border-border`, ...). No custom CSS, `style={}`, raw colours or
   arbitrary values: `npm --prefix web run lint` (css-guard) fails otherwise. A missing token is a change to
   `globals.css` following the brand tokens (ADR 0007), not an inline value.
5. **Composition.** Screen-level patterns (page header, data table, empty state, form field with error) live in
   `web/shared/src/ui/patterns` and are built from shadcn primitives.
6. **i18n and a11y.** No literal strings in components that apps render: accept labels as props or use keys.
   Keep the Radix accessibility props (labels, `aria-*`, focus management).
7. **Licence.** New transitive packages pulled by the CLI must pass `npm --prefix web run licenses`.
8. **Verify.** `npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run test`; add a
   Vitest render test for any pattern you compose.
