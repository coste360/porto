//go:build integration

package pricelist

import (
	"errors"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestPublicMatrix_OnlyWithPublishMatrix: no list or a list without publish_matrix is ErrNoPublicMatrix; with the
// flag the public matrix has the cells of getPriceMatrix (transit, bands, online channel), labels and no ids.
func TestPublicMatrix_OnlyWithPublishMatrix(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		scope := e.scope(t)
		on := calendar.New(2027, 7, 10)
		if _, err := e.pl.PublicTransitMatrix(ctx, scope, "it", on); !errors.Is(err, ErrNoPublicMatrix) {
			t.Fatalf("no list: %v", err)
		}
		req := e.transitDoc("PUB-2027", 2027)
		doc := e.createList(t, req)
		doc = e.activate(t, doc, calendar.Date{}).Document
		if _, err := e.pl.PublicTransitMatrix(ctx, scope, "it", on); !errors.Is(err, ErrNoPublicMatrix) {
			t.Fatalf("list without publish_matrix: %v", err)
		}

		// A second marina-wide list with the flag (same validity is refused, so 2028).
		req28 := e.transitDoc("PUB-2028", 2028)
		req28.Settings.PublishMatrix = true
		req28.Periods[1].Name.En = ptr("High")
		doc28 := e.activate(t, e.createList(t, req28), calendar.Date{}).Document
		if _, err := e.pl.PublicTransitMatrix(ctx, scope, "it", on); !errors.Is(err, ErrNoPublicMatrix) {
			t.Fatalf("flagged list not valid on the day: %v", err)
		}
		on28 := calendar.New(2028, 7, 10)
		pm, err := e.pl.PublicTransitMatrix(ctx, scope, "en", on28)
		if err != nil {
			t.Fatal(err)
		}
		m, err := e.pl.Matrix(ctx, e.Owner, e.MarinaID, doc28.ID, MatrixParams{
			ContractType: pricing.ContractTransit, Rows: pricing.RowsBands, Channel: pricing.ChannelOnline, QuotedOn: on28,
		})
		if err != nil {
			t.Fatal(err)
		}
		if pm.PriceListID != doc28.ID || pm.QuotedOn != on28 || !pm.PriceIncludesVAT || pm.Currency == "" {
			t.Fatalf("header %+v", pm)
		}
		if len(pm.Cells) != len(m.Cells) || len(pm.Cells) == 0 {
			t.Fatalf("cells %d vs %d", len(pm.Cells), len(m.Cells))
		}
		for i, c := range m.Cells {
			p := pm.Cells[i]
			if p.Row != c.Row || p.Col != c.Col || (c.TotalCents == nil) != (p.TotalCents == nil) ||
				(c.TotalCents != nil && int64(*c.TotalCents) != *p.TotalCents) {
				t.Fatalf("cell %d: public %+v, matrix %+v", i, p, c)
			}
		}
		if len(pm.Rows) != 3 || pm.Rows[0].Key != "L10" || pm.Rows[0].MaxLoaCm == nil || *pm.Rows[0].MaxLoaCm != 1000 || pm.Rows[2].MaxLoaCm != nil {
			t.Fatalf("rows %+v", pm.Rows)
		}
		labels := map[string]string{}
		for _, c := range pm.Columns {
			labels[c.Key] = c.Label
		}
		if labels["ALTA"] != "High" || labels["BASSA"] != "Bassa" {
			t.Fatalf("column labels %+v", labels)
		}
		_ = doc
	})
}
