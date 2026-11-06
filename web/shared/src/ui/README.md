# Porto UI components

Everything in this folder (except `patterns/`, `index.ts` and this file) is **output of the
shadcn CLI**. Styling in Porto is shadcn/ui plus Tailwind utilities on the theme tokens of
`../styles/globals.css` (AGENTS.md, ADR 0007). `npm run lint` (`web/scripts/css-guard.mjs`)
rejects other stylesheets, inline styles, colour literals and arbitrary Tailwind values outside
the CLI output.

## Adding a component

```bash
cd web/shared
npx shadcn@latest add <name>
```

Then:

1. Fix the utils import if the CLI wrote `from "cn"` (it resolves the `utils` alias of
   `components.json` to a bare package): it must be `from "@porto/shared/lib/utils"`.
   Never install the unrelated `cn` npm package.
2. Add the file to `index.ts` (`export * from './<name>';`).
3. Run `npm run lint && npm run typecheck && npm test` from `web/`.

Do not edit component internals. Restyle through variants of the component, never with a
`className` that changes colours from a page. Pages compose components from `patterns/`.

## Local changes to CLI output

| File         | Change                                                         | Why                                                    |
| ------------ | -------------------------------------------------------------- | ------------------------------------------------------ |
| all          | `from "cn"` → `from "@porto/shared/lib/utils"`                 | CLI alias resolution (see above)                       |
| `sonner.tsx` | `useTheme` from `@porto/shared/theme` instead of `next-themes` | Porto has its own ThemeProvider; no Next.js dependency |

The React Compiler lint rules `react-hooks/purity` and `react-hooks/set-state-in-effect` are
off for this folder and `../hooks/` (upstream patterns in `sidebar.tsx` and `use-mobile.ts`).
Some components carry English screen-reader text from upstream (e.g. the dialog close button);
pass a translated `aria-label` where the component accepts one.

## Patterns

`patterns/` holds Porto's own compositions: `BrandLogo` (the only way to show the mark),
`PageHeader`, `EmptyState`, `ConfirmDialog`, `SecretReveal`, `DataTable`, `FormField`,
`ProblemAlert`.
