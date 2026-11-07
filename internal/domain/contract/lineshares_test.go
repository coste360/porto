package contract

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// TestLineShares_FromSnapshot: the contract lines carry their share of the snapshot (a single area segment of an
// annual contract gets the whole berth price, not 0), services and land resources the lines of their item, and
// adjustments are spread so that the shares sum to the total.
func TestLineShares_FromSnapshot(t *testing.T) {
	seg := store.ContractLine{ID: "l1", Seq: 1, LineKind: "resource", ResourceType: "dynamic_area", ResourceID: "a1"}
	cases := []struct {
		name  string
		lines []store.ContractLine
		snap  *PriceSnapshot
		total int64
		want  []int64
	}{
		{
			name:  "annual on an area, included service at 0",
			lines: []store.ContractLine{seg},
			snap: &PriceSnapshot{Breakdown: pricing.Breakdown{Lines: []pricing.Line{
				{Seq: 1, Kind: pricing.LineBerth, ItemID: "ann", Gross: 11_481_00},
				{Seq: 2, Kind: pricing.LineIncluded, ItemID: "utf", Gross: 0},
			}}},
			total: 11_481_00, want: []int64{11_481_00},
		},
		{
			name: "berth, parking and service lines",
			lines: []store.ContractLine{
				seg,
				{ID: "l2", Seq: 2, LineKind: "resource", ItemID: "park", ResourceType: store.ResourceLand, ResourceID: "p1"},
				{ID: "l3", Seq: 3, LineKind: "service", ItemID: "water"},
			},
			snap: &PriceSnapshot{Breakdown: pricing.Breakdown{Lines: []pricing.Line{
				{Seq: 1, Kind: pricing.LineBerth, ItemID: "ann", Gross: 5000_00},
				{Seq: 2, Kind: pricing.LineService, ItemID: "park", Gross: 300_00},
				{Seq: 3, Kind: pricing.LineService, ItemID: "water", Gross: 200_00},
			}}},
			total: 5500_00, want: []int64{5000_00, 300_00, 200_00},
		},
		{
			name:  "an adjustment (move) is spread by weight",
			lines: []store.ContractLine{seg, {ID: "l2", Seq: 2, LineKind: "service", ItemID: "water"}},
			snap: &PriceSnapshot{Breakdown: pricing.Breakdown{Lines: []pricing.Line{
				{Seq: 1, Kind: pricing.LineBerth, ItemID: "ann", Gross: 900_00},
				{Seq: 2, Kind: pricing.LineService, ItemID: "water", Gross: 100_00},
			}}},
			total: 1100_00, want: []int64{990_00, 110_00},
		},
		{
			name: "a land resource without item is not priced",
			lines: []store.ContractLine{
				{ID: "l0", Seq: 1, LineKind: "resource", ResourceType: store.ResourceBerth, ResourceID: "b1"},
				{ID: "l2", Seq: 2, LineKind: "resource", ResourceType: store.ResourceLand, ResourceID: "p1"},
			},
			snap:  &PriceSnapshot{Breakdown: pricing.Breakdown{Lines: []pricing.Line{{Seq: 1, Kind: pricing.LineBerth, ItemID: "ann", Gross: 3184_00}}}},
			total: 3184_00, want: []int64{3184_00, 0},
		},
		{name: "no snapshot", lines: []store.ContractLine{seg}, total: 700_00, want: []int64{700_00}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := withLineShares(tc.lines, tc.snap, tc.total)
			for i, w := range tc.want {
				if got[i].AmountCents != w {
					t.Fatalf("line %d: %d, want %d (%+v)", i, got[i].AmountCents, w, got)
				}
			}
			if tc.lines[0].AmountCents != 0 {
				t.Fatal("input lines changed")
			}
		})
	}
}

// TestLineShares_SumToTotal (property): whatever the lines and the breakdown, the shares sum to the total.
func TestLineShares_SumToTotal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		items := []string{"", "a", "b", "c"}
		n := rapid.IntRange(1, 5).Draw(t, "lines")
		lines := make([]store.ContractLine, n)
		for i := range lines {
			kind := rapid.SampledFrom([]string{"resource", "service"}).Draw(t, "kind")
			lines[i] = store.ContractLine{Seq: i + 1, LineKind: kind, ItemID: rapid.SampledFrom(items).Draw(t, "item")}
		}
		var snap *PriceSnapshot
		if rapid.Bool().Draw(t, "snap") {
			snap = &PriceSnapshot{}
			for j := range rapid.IntRange(0, 5).Draw(t, "blines") {
				snap.Breakdown.Lines = append(snap.Breakdown.Lines, pricing.Line{
					Seq: j + 1, Kind: rapid.SampledFrom([]string{pricing.LineBerth, pricing.LineService, pricing.LineIncluded}).Draw(t, "bkind"),
					ItemID: rapid.SampledFrom(items).Draw(t, "bitem"), Gross: money.Cents(rapid.Int64Range(-1_000_00, 10_000_00).Draw(t, "gross")),
				})
			}
		}
		total := rapid.Int64Range(-5_000_00, 50_000_00).Draw(t, "total")
		var sum int64
		for _, l := range withLineShares(lines, snap, total) {
			sum += l.AmountCents
		}
		if sum != total {
			t.Fatalf("Σ shares %d, total %d", sum, total)
		}
	})
}
