// Package tmpl is Porto's closed template language for print templates (m4-plan §6.1): {{path}} with filters,
// {{#each}} and {{#if}} blocks, {{signature "role"}}, {{pagebreak}}, {{logo}}, {{page}}/{{pages}}, {{today}}.
// Paths are validated against the field catalogue of the kind; inserted values are escaped so they can never
// create Markdown structure. No function calls, includes, file or network access; output and iterations are
// bounded.
//
// It is pure: no store, no clock, no randomness (depguard pure-engines).
package tmpl
