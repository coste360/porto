# Pricing corpus

Hand-derived cases for the pure engine in `internal/domain/pricing` (m2-plan §2.7). The test
`TestQuote_Corpus` runs every case as `<file>/<case name>`. `TestCorpus_AtLeast200` checks the per-file quotas,
and every case must carry a `why` and a `spec` reference.

## The rule

You compute the expectations by hand, from the semantics in `internal/domain/pricing/doc.go`, in integer cents with
every half-even rounding written out in `why`. A spreadsheet is fine. You never take an expectation from the engine's
output. When the engine and a case disagree, derive the case again by hand before you change either one.

## Layout

- `catalogs/<name>.json` holds a `pricing.Catalog` in the engine JSON (`price_list`, `items`, `rules`, with the
  API tags `*_cents`, `*_cm`, `*_bp`, `*_milli`). Every fixture must pass `Validate` with no errors, except the
  ones named `invalid-*`, which must have at least one error.
- `cases/<NN>-<group>.json` holds `{"cases": [...]}`. The prefix `NN` is the group of the quota table.

## Case fields

| Field | Meaning |
|---|---|
| `name` | unique snake_case scenario name |
| `spec` | the spec or plan section it exercises |
| `catalog` | fixture name |
| `patch` | optional RFC 7386 merge patch of the fixture. Arrays are replaced and `null` deletes a key. |
| `add_items`, `add_rules` | optional items or rules appended after the patch |
| `input` | a `pricing.Input` (`loa_cm`, `beam_cm`, `qty_milli`, ...) |
| `expect` | the subset of the `Breakdown` JSON to compare, plus the optional `trace_codes` (a subsequence of the trace codes) or `error` |
| `why` | the hand derivation |

How `expect` is compared:
- Objects compare only the fields you list.
- Arrays must have the same length, and their elements are compared the same way.
- `total_cents` is mandatory unless the case expects an `error`.
- An `error` is `no_price:<reason>`, `invalid_input:<code>`, `promo_not_applicable` or `overflow`.

## Adding a case

1. Pick the group, then the fixture, or add a new one.
2. Derive the numbers by hand and write them in `why`.
3. Run `go test ./internal/domain/pricing/ -run 'TestQuote_Corpus|TestCorpus_' -count=1`.
