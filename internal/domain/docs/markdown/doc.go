// Package markdown parses the Markdown subset of print templates (headings, paragraphs, emphasis, lists, pipe
// tables, rules, page breaks, logo and signature fields) into a block AST, and writes escaped HTML for emails
// (m4-plan §6.3). Anything outside the subset is literal text.
//
// It is pure: no store, no clock, no randomness (depguard pure-engines).
package markdown
