package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/booking"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// seedOnlineBookings adds online bookings made on the demo page /b/demo (m8-plan §5), so that the desk list of the
// online bookings has every state: each goes through the public flow (search, hold, details with the consents,
// payment start: without a checkout provider the booking waits for the desk's approval), then the desk approves
// (payment link pending), approves and records the bank transfer with the manual driver (confirmed), or rejects.
// Customers use the reserved .test domain, so no mail can ever leave. A no-op when the marina already has online
// bookings or online booking is not enabled.
func (a *App) seedOnlineBookings(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.Booking == nil || a.Contracts == nil {
		return nil
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := a.DB.NewSelect().TableExpr("online_bookings").Where("marina_id = ?", scope.ID()).Count(ctx); err != nil || n > 0 {
		return err
	}
	cfg, err := a.Booking.Config(ctx, DemoMarinaSlug)
	if err != nil || !cfg.Enabled {
		return nil //nolint:nilerr // booking not enabled on this marina: nothing to show
	}
	var consents booking.Consents
	for _, t := range cfg.Texts {
		switch t.Kind {
		case "terms":
			consents.TermsVersion = t.Version
		case "privacy":
			consents.PrivacyVersion = t.Version
		}
	}
	today := calendar.In(a.Clock.Now(), seedPlannerLocation(ctx, a, scope))
	type demo struct {
		first, last, country, locale, boat, hull string
		loa, beam                                geo.Cm
		arrive, nights                           int
		desk                                     string // "" waits for approval, approve, confirm, reject
	}
	demos := []demo{
		{"Pierre", "Dubois", "FR", "en", "Mistral II", "monohull_sail", 1180, 380, 4, 3, ""},
		{"Hannah", "Schmidt", "DE", "en", "Seeadler", "monohull_sail", 1250, 400, 9, 5, ""},
		{"Lorenzo", "Ferri", "IT", "it", "Gabbiano Blu", "monohull_motor", 1020, 350, 6, 2, "approve"},
		{"Sophie", "Martin", "FR", "en", "Belle Ile", "monohull_sail", 1100, 360, 2, 4, "confirm"},
		{"Giovanni", "Esposito", "IT", "it", "Tramonto", "monohull_motor", 1300, 420, 12, 7, "confirm"},
		{"James", "Walker", "GB", "en", "Sea Breeze", "monohull_sail", 1150, 370, 1, 2, "confirm"},
		{"Klaus", "Weber", "DE", "en", "Nordwind", "monohull_motor", 1450, 450, 15, 3, "reject"},
	}
	m := res.MarinaID
	for i, d := range demos {
		arr := today.AddDays(d.arrive)
		dep := arr.AddDays(d.nights)
		v := booking.Vessel{LOACm: d.loa, BeamCm: d.beam, DraftCm: d.loa / 6, HullType: d.hull}
		av, err := a.Booking.Availability(ctx, DemoMarinaSlug, booking.AvailabilityInput{Arrival: arr, Departure: dep, Vessel: v})
		if err != nil {
			return fmt.Errorf("online availability: %w", err)
		}
		category := ""
		for _, c := range av.Categories {
			if c.Available {
				category = c.Code
				break
			}
		}
		if category == "" {
			continue
		}
		key := fmt.Sprintf("seed-online-%d-%s", i, arr)
		cr, err := a.Booking.Create(ctx, DemoMarinaSlug, booking.CreateInput{
			Arrival: arr, Departure: dep, Vessel: v, Category: category, Locale: d.locale, IdempotencyKey: key, BodySHA256: key,
		})
		if err != nil {
			if seedSkippable(err) || errors.Is(err, booking.ErrBusy) {
				continue
			}
			return fmt.Errorf("online booking: %w", err)
		}
		tok := booking.Tokens{Booking: cr.BookingToken}
		email := strings.ToLower(d.first+"."+d.last) + "@" + DemoEmailDomain
		if _, err := a.Booking.SubmitDetails(ctx, DemoMarinaSlug, cr.BookingID, tok, booking.DetailsInput{
			Customer: booking.CustomerDetails{
				FirstName: d.first, LastName: d.last, Email: email, Phone: fmt.Sprintf("+39 333 00000%02d", i), Country: d.country, Locale: d.locale,
			},
			Vessel:        booking.VesselDetails{Name: d.boat, Flag: d.country, HullType: d.hull, DraftCm: v.DraftCm},
			PeopleOnBoard: 2 + i%4, ExpectedArrivalTime: "16:00", Consents: consents,
		}); err != nil {
			return fmt.Errorf("online booking details: %w", err)
		}
		if _, err := a.Booking.StartPayment(ctx, DemoMarinaSlug, cr.BookingID, tok, booking.PayInput{IdempotencyKey: key + "-pay"}); err != nil {
			return fmt.Errorf("online booking payment: %w", err)
		}
		if d.desk == "" {
			continue
		}
		var contractID string
		if err := a.DB.NewSelect().TableExpr("online_bookings").ColumnExpr("CAST(contract_id AS TEXT)").Where("marina_id = ?", scope.ID()).
			Where("id = ?", cr.BookingID).Scan(ctx, &contractID); err != nil {
			return err
		}
		c, err := store.GetContract(ctx, a.DB.DB, scope, contractID)
		if err != nil {
			return err
		}
		if d.desk == "reject" {
			if _, err := a.Booking.Reject(ctx, p, m, c.ID, c.Version, "Nessun posto adatto alle misure per quelle date", false); err != nil {
				return fmt.Errorf("online booking reject: %w", err)
			}
			continue
		}
		if _, err := a.Booking.Approve(ctx, p, m, c.ID, c.Version); err != nil {
			return fmt.Errorf("online booking approve: %w", err)
		}
		if d.desk != "confirm" {
			continue
		}
		cur, err := store.GetContract(ctx, a.DB.DB, scope, c.ID)
		if err != nil {
			return err
		}
		if _, err := a.Contracts.ConfirmTransit(ctx, p, m, c.ID, cur.Version, contract.ManualConfirmation{Reference: "Bonifico " + strings.ToUpper(d.last)}); err != nil {
			return fmt.Errorf("online booking confirm: %w", err)
		}
	}
	return nil
}
