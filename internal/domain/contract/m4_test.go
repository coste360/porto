package contract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

func md(s string) calendar.Date { return calendar.MustParse(s) }

func TestTermination_Amounts(t *testing.T) {
	proRata := CancellationPolicy{Refund: RefundProRata}
	cases := []struct {
		name           string
		total, paid    money.Cents
		nights, unused int
		pol            CancellationPolicy
		want           money.Cents
	}{
		{"refund of the unused share of what was paid", 365_000, 100_000, 365, 73, proRata, -20_000},
		{"no refund", 365_000, 100_000, 365, 73, CancellationPolicy{Refund: RefundNone}, 0},
		{"penalty on the remaining total", 365_000, 0, 365, 73, CancellationPolicy{Refund: RefundNone, PenaltyBP: 1000}, 7_300},
		{"penalty minus refund", 365_000, 365_000, 365, 73, CancellationPolicy{Refund: RefundProRata, PenaltyBP: 5000}, 36_500 - 73_000},
		{"half-even", 100, 1, 3, 1, proRata, 0}, // 1 × 1/3 = 0.33 → 0
		{"nothing unused", 1000, 1000, 10, 0, proRata, 0},
		{"unused clamped", 1000, 1000, 10, 20, proRata, -1000},
	}
	for _, c := range cases {
		if got := TerminationAmount(c.total, c.paid, c.nights, c.unused, c.pol); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

func TestRenewal_NextPeriod(t *testing.T) {
	cases := []struct {
		term, starts, ends, wantS, wantE string
	}{
		{TermAnnual, "2027-01-01", "2028-01-01", "2028-01-01", "2029-01-01"},
		{TermAnnual, "2027-03-01", "2028-02-29", "2028-02-29", "2029-02-28"},
		{TermMonthly, "2027-01-31", "2027-02-28", "2027-02-28", "2027-03-28"},
		{TermSeasonal, "2027-05-01", "2027-10-01", "2028-05-01", "2028-10-01"},
		{TermPeriod, "2027-06-10", "2027-07-20", "2028-06-10", "2028-07-20"},
	}
	for _, c := range cases {
		s, e := NextPeriod(c.term, md(c.starts), md(c.ends), nil)
		if s.String() != c.wantS || e.String() != c.wantE {
			t.Errorf("%s %s..%s: %s..%s, want %s..%s", c.term, c.starts, c.ends, s, e, c.wantS, c.wantE)
		}
	}
	fixed := md("2028-02-01")
	if s, e := NextPeriod(TermAnnual, md("2027-01-01"), md("2028-01-01"), &fixed); s != fixed || e.String() != "2029-02-01" {
		t.Errorf("fixed start: %s..%s", s, e)
	}
}

func TestRenewal_ExclusionRules(t *testing.T) {
	cases := []struct {
		f    exclusionFacts
		want string
	}{
		{exclusionFacts{}, ""},
		{exclusionFacts{Arrears: true}, ExcludeArrears},
		{exclusionFacts{InsuranceExpired: true}, ExcludeInsuranceExpired},
		{exclusionFacts{NoPrice: true}, ExcludeNoPrice},
		{exclusionFacts{BerthUnavailable: true}, ExcludeBerthUnavailable},
		{exclusionFacts{AlreadyRenewed: true, Arrears: true, NoPrice: true}, ExcludeAlreadyRenewed},
		{exclusionFacts{Arrears: true, InsuranceExpired: true}, ExcludeArrears},
		{exclusionFacts{InsuranceExpired: true, NoPrice: true, BerthUnavailable: true}, ExcludeInsuranceExpired},
	}
	for _, c := range cases {
		if got := exclusionReason(c.f); got != c.want {
			t.Errorf("%+v: %q, want %q", c.f, got, c.want)
		}
	}
}

func TestCharter_PricingModes(t *testing.T) {
	placed := []money.Cents{100_000, 120_000}
	spare := []money.Cents{90_000}
	if got := CharterPrice(CharterPerBerth, placed, spare, 0); got != 220_000 {
		t.Errorf("per_berth %d", got)
	}
	if got := CharterPrice(CharterPerVessel, placed, spare, 0); got != 310_000 {
		t.Errorf("per_vessel %d", got)
	}
	if got := CharterPrice(CharterFlat, placed, spare, 150_000); got != 150_000 {
		t.Errorf("flat %d", got)
	}
}

func TestPolicy_V2RoundTrip(t *testing.T) {
	bp := int64(3000)
	in := Policy{Version: PolicyVersion, ReleasePolicy: ReleaseWithConsent, AllowSegments: true, Payment: &PaymentPolicy{
		Deposit: &DepositPolicy{BP: &bp}, DepositDueDays: 7, Schedule: SchedulePolicy{Mode: "interval", EveryMonths: 3, Count: 3},
		IssueDaysBeforeDue: 15, RoundingUnitCents: 1,
	}, Renewal: &RenewalPolicy{Tacit: true, NoticeDays: 60}}
	out, err := DecodePolicy(store.MustJSON(in))
	if err != nil || out.Payment == nil || out.Payment.Deposit == nil || *out.Payment.Deposit.BP != 3000 || !out.Renewal.Tacit {
		t.Fatalf("%+v %v", out, err)
	}
	// v1 rows (M3) are read with the defaults: no clause, resolved later.
	v1, err := DecodePolicy(store.JSON(`{"version":1,"reprice_on_move":false,"release_policy":"never","allow_segments":true}`))
	if err != nil || v1.Payment != nil {
		t.Fatalf("v1 %+v %v", v1, err)
	}
	pay, ren, can := effective(v1, nil)
	if pay.Schedule.Mode != "single" || ren.NoticeDays != 60 || can.Refund != RefundProRata {
		t.Fatalf("defaults %+v %+v %+v", pay, ren, can)
	}
}

// TestInstallment_SettleOnlyManualDriver is INV-32 at the source level: the paid status is written only by
// store.SettleInstallmentRow, called only from settle.go of this package (and the holder instalments of rights).
func TestInstallment_SettleOnlyManualDriver(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	allowedCallers := map[string]bool{
		filepath.Join("internal", "domain", "contract", "settle.go"): true,
	}
	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
		if err != nil {
			return nil //nolint:nilerr // a file being written by another builder: skip it
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "SettleInstallmentRow" && !allowedCallers[rel] {
				t.Errorf("%s calls SettleInstallmentRow", rel)
			}
			return true
		})
		// No other code sets an instalment row to paid by a literal assignment.
		if rel != filepath.Join("internal", "store", "installments.go") && !strings.HasPrefix(rel, filepath.Join("internal", "store")) &&
			strings.Contains(string(src), "Status = store.InstallmentPaid") {
			t.Errorf("%s assigns InstallmentPaid", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
