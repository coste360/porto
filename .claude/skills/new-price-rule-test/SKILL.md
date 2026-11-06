---
name: new-price-rule-test
description: Add test cases for the pure pricing engine (bases, modifiers, services, discounts, rounding, deposits, VAT) and keep the simulator matrix aligned. Use from M2 when a price rule is added or a pricing bug is fixed.
argument-hint: "[rule or scenario]"
---
# New price rule test: $ARGUMENTS

Procedure for M2 and later; `internal/domain/pricing` does not exist in M0.

Read first: `docs/spec/12-listini-articoli-prezzi.md` (12.3 bases, 12.4 engine steps, 12.5 simulator and matrix),
`docs/invariants.md` INV-20 and INV-21, `.claude/rules/charges.md`.

1. **Locate the step** of 12.4 the scenario exercises: base price (priority → specificity → recency,
   `ErrNoPrice`), modifiers (stackable sum, non-stackable most favourable to the customer), services, discounts
   (agreement → promo code → automatic), rounding, deposit and instalments, VAT split.
2. **Write the case** in the table of `pricing_test.go` (or the JSON cases under `testdata/pricing/` if the
   suite uses files): input (vessel length/beam in cm, nights as `calendar.Date`s, berth category, channel,
   customer category), catalog and rules, expected `PriceBreakdown` lines in `money.Cents`. Name it after the
   scenario, e.g. `multihull_over_length_high_season`.
3. **Exactness.** Expected values are computed by hand in integer cents with the spec's rounding; never derive
   the expectation by running the engine.
4. **Properties** (rapid) when the rule has a general law: VAT lines sum to the total; instalments sum to the
   total (INV-31); results are independent of rule order in the input; same input + same `engine_version` ⇒
   same snapshot (INV-20).
5. **Bug fix:** the new case fails before the fix.
6. **Matrix:** the simulator matrix for the same inputs must equal the engine result (M2 acceptance:
   matrix = simulator); add the case to that comparison.
7. Keep the engine pure: no store, clock or `time.Now` (depguard). Run
   `go test ./internal/domain/pricing/... -rapid.checks=2000` and check the case count stays ≥ 200.
