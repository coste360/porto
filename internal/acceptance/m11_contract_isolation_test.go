//go:build integration

package acceptance

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_CrossMarinaResourceIdsM11 (INV-73, M11 contract) extends the resource-id sweep to the rental routes:
// marina B calls every rental route under its own {marinaId} with the ids of real rows of marina A (unit, skipper,
// formula, extra, booking, block, damage), inserted directly because the CONTRACT stubs create nothing. Each call is
// 404 not_found, also while the handlers are still stubs.
func TestAcceptance_CrossMarinaResourceIdsM11(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		w := newDeskWorld(t, db, 2)
		o, M := w.owner, w.M
		classes := jsonList(o.expect(t, 200, "GET", M+"/revenue-classes", nil), "items")
		item := o.expect(t, 201, "POST", M+"/items", map[string]any{
			"code": "NOL-GOM", "name": map[string]string{"it": "Noleggio gommone"}, "item_type": "service", "unit": "hour",
			"vat_rate_bp": 2200, "revenue_class_id": str(classes[0].(map[string]any), "id"), "price_includes_vat": true,
		})
		vessel := w.vessel(t, "Gommone 1", 580, 240)
		now := store.UTC(w.clock.Now())
		day := calendar.MustParse("2027-06-12")
		unit := &store.RentalUnit{
			ID: store.NewID(), MarinaID: w.marina, VesselID: vessel, Code: "GOM1", Category: store.RentalCategoryRIB, Seats: 6,
			Licence: store.RentalLicenceThreshold, FuelType: store.RentalFuelPetrol, TankMilli: 60_000, Status: store.RentalUnitAvailable,
			ItemID: str(item, "id"), Ownership: store.RentalOwnershipMarina, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		skipper := &store.RentalSkipper{ID: store.NewID(), MarinaID: w.marina, Name: "Skipper", Active: true, Version: 1, CreatedAt: now, UpdatedAt: now}
		formula := &store.RentalFormula{
			ID: store.NewID(), MarinaID: w.marina, Code: "ORARIA", Name: store.JSON(`{"it":"Oraria"}`), Kind: store.RentalKindHourly,
			MinSlots: 2, MinDays: 1, MaxDays: 30, Skipper: store.RentalSkipperOptional, Fuel: store.RentalFuelIncluded,
			GraceMinutes: 15, LateStepMinutes: 30, Categories: store.JSON(`[]`), Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		extra := &store.RentalExtra{
			ID: store.NewID(), MarinaID: w.marina, ItemID: str(item, "id"), ChargeMode: store.RentalExtraPerRental,
			DefaultQtyMilli: 1000, MaxQtyMilli: 5000, Categories: store.JSON(`[]`), Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		booking := &store.RentalBooking{
			ID: store.NewID(), MarinaID: w.marina, Number: 1, Year: 2027, UnitID: unit.ID, FormulaID: formula.ID, StartsOn: day,
			StartSlot: 20, EndsOn: day, EndSlot: 26, Status: store.RentalStatusConfirmed, Channel: store.RentalChannelDesk,
			PriceSnapshot: store.JSON(`{}`), EngineVersion: 1, DepositStatus: store.RentalDepositStatusNone, Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		block := &store.RentalBlock{
			ID: store.NewID(), MarinaID: w.marina, UnitID: unit.ID, FromOn: day.AddDays(1), FromSlot: 16, ToOn: day.AddDays(1), ToSlot: 38,
			Reason: store.RentalBlockMaintenance, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		damage := &store.RentalDamage{
			ID: store.NewID(), MarinaID: w.marina, BookingID: booking.ID, Description: "Graffio", EstimateCents: 18000,
			Status: store.RentalDamageOpen, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		for _, row := range []any{unit, skipper, formula, extra, booking, block, damage} {
			if _, err := db.NewInsert().Model(row).Exec(ctx); err != nil {
				t.Fatalf("insert %T: %v", row, err)
			}
		}

		ids := map[string]string{
			"{rentalUnitId}": unit.ID, "{rentalSkipperId}": skipper.ID, "{rentalFormulaId}": formula.ID,
			"{rentalExtraId}": extra.ID, "{rentalBookingId}": booking.ID, "{rentalBlockId}": block.ID,
			"{rentalDamageId}": damage.ID, "{phase}": "checkout",
		}
		b, err := w.h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "b@porto.test", Name: "Owner B", Password: password, Organization: "Org B", MarinaName: "Marina B", MarinaSlug: "marina-b",
		})
		if err != nil {
			t.Fatal(err)
		}
		bc := w.h.cookieClient()
		bc.login(t, "b@porto.test")
		router, ok := w.h.app.Router.(chi.Routes)
		if !ok {
			t.Fatal("router is not a chi router")
		}
		placeholder := regexp.MustCompile(`\{[A-Za-z]+\}`)
		checked := map[string]bool{}
		routes := 0
		err = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			if !strings.Contains(route, "{marinaId}/rental/") {
				return nil
			}
			path, uses := strings.ReplaceAll(route, "{marinaId}", b.MarinaID), false
			for p, id := range ids {
				if strings.Contains(path, p) {
					path, uses = strings.ReplaceAll(path, p, id), true
					checked[p] = true
				}
			}
			if !uses || placeholder.MatchString(path) {
				return nil
			}
			var body any
			if method != "GET" && method != "DELETE" {
				body = map[string]any{}
			}
			routes++
			r := bc.do(t, method, path, body, "If-Match", `"1"`)
			if r.status != http.StatusNotFound || r.problemType() != "not_found" {
				t.Errorf("%s %s with a resource of marina A: status %d %s, want 404 not_found", method, route, r.status, r.body)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for p := range ids {
			if !checked[p] {
				t.Errorf("no route checked with %s", p)
			}
		}
		if routes < 28 {
			t.Errorf("only %d rental routes with ids checked", routes)
		}
	})
}
