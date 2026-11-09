package report

import (
	"context"
	"fmt"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/report/calc"
)

// R-rental (M11, m11-plan §11) on the M7 machinery: one read transaction, the pure builder calc.BuildRental, tiles
// rental_utilisation and rental_revenue = Totals of their drill views (INV-62). Money is visible with
// reports.rental as well as reports.all. Deposit retentions are never rental revenue (INV-98).

// RentalResult is the rental report.
type RentalResult = calc.RentalResult

// authorizeRental checks reports.all, else reports.rental (money visible with both). Another marina is
// auth.ErrNotFound.
func (s *Service) authorizeRental(ctx context.Context, p *auth.Principal, marinaID string) (access, error) {
	scope, _, err := p.AuthorizeAny(ctx, s.d.DB.DB, marinaID, auth.PermReportsAll, auth.PermReportsRental)
	if err != nil {
		return access{}, err
	}
	return access{Scope: scope, Full: true}, nil
}

// CheckRental authorises p for the rental report (handlers call it before answering a parse error).
func (s *Service) CheckRental(ctx context.Context, p *auth.Principal, marinaID string) error {
	_, err := s.authorizeRental(ctx, p, marinaID)
	return err
}

// hasRental reports whether the principal reads the rental report on the marina of acc.
func hasRental(p *auth.Principal, acc access) bool {
	return p.Can(acc.Scope.OrgID(), acc.Scope.ID(), auth.PermReportsAll) || p.Can(acc.Scope.OrgID(), acc.Scope.ID(), auth.PermReportsRental)
}

// Rental returns the rental report (reports.rental or reports.all).
func (s *Service) Rental(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*RentalResult, error) {
	acc, err := s.authorizeRental(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if q.Report != "" && q.Report != calc.KeyRental {
		return nil, filterErr("report")
	}
	q.Report = calc.KeyRental
	if err := checkPeriod(q.From, q.To); err != nil {
		return nil, err
	}
	if !calc.RentalGroupAllowed(orDefault(q.View, calc.RentalViewUtilisation), q.GroupBy) {
		return nil, filterErr("group_by")
	}
	var out *RentalResult
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r := s.newRun(ctx, tx, p, acc)
		var err error
		if q.From, q.To, err = r.period(q.From, q.To); err != nil {
			return err
		}
		res, _, err := r.runRental(q)
		if err != nil {
			return err
		}
		out = res
		return finish(res, q, r.today)
	})
	return out, err
}

// rentalNumber formats a booking number as the booking side does (NL<year>-NNNNNN, D23).
func rentalNumber(year int, n int64) string { return fmt.Sprintf("NL%04d-%06d", year, n) }

func (r *run) runRental(q Params) (*RentalResult, calc.Facts, error) {
	view := orDefault(q.View, calc.RentalViewUtilisation)
	in := calc.RentalInput{
		From: q.From, To: q.To, View: view, GroupBy: q.GroupBy, UnitID: q.UnitID, Category: q.Dims.Category, FormulaID: q.FormulaID,
	}
	set, err := store.LoadRentalSettings(r.ctx, r.tx, r.scope())
	if err != nil {
		return nil, nil, err
	}
	switch view {
	case calc.RentalViewUtilisation:
		err = r.rentalUtilisation(&in, set)
	case calc.RentalViewRevenue:
		err = r.rentalRevenue(&in, set)
	case calc.RentalViewDamages:
		err = r.rentalDamages(&in)
	case calc.RentalViewDeposits:
		err = r.rentalDeposits(&in)
	}
	if err != nil {
		return nil, nil, err
	}
	res, facts := calc.BuildRental(in)
	return &res, facts, nil
}

func (r *run) rentalUtilisation(in *calc.RentalInput, set *store.RentalSettings) error {
	units, err := store.AllRentalUnits(r.ctx, r.tx, r.scope())
	if err != nil {
		return err
	}
	for _, u := range units {
		in.Units = append(in.Units, calc.RentalUnitInfo{
			ID: u.ID, Code: u.Code, Category: u.Category, VesselID: u.VesselID, Retired: u.Status == store.RentalUnitRetired,
			CreatedOn: calendar.In(u.CreatedAt, r.loc),
		})
	}
	in.WindowDay = int64(set.OpenToSlot - set.OpenFromSlot)
	rows, err := store.ReportRentalSlotDays(r.ctx, r.tx, r.scope(), in.From, in.To, set.OpenFromSlot, set.OpenToSlot)
	if err != nil {
		return err
	}
	for _, x := range rows {
		in.SlotDays = append(in.SlotDays, calc.RentalSlotDay{UnitID: x.UnitID, Date: x.Date, Rented: x.Rented, Blocked: x.Blocked})
	}
	return nil
}

// rentalLineKind classifies a rental charge line: the plan key "rental:<kind>" when the charge side set one, else
// the item (unit, skipper, fuel, late fee, damage item), else extra; adjustment charges are adjustment.
func rentalLineKind(l store.ReportRentalChargeLine, set *store.RentalSettings) string {
	if k, ok := strings.CutPrefix(l.PlanKey, "rental:"); ok && k != "" {
		return k
	}
	if l.LinkKind == "adjustment" || l.CorrectsID != "" {
		return calc.RentalLineAdjustment
	}
	switch l.ItemID {
	case "":
		return calc.RentalLineExtra
	case l.UnitItemID:
		return calc.RentalLineUnit
	case set.SkipperItemID:
		return calc.RentalLineSkipper
	case l.FuelItemID:
		return calc.RentalLineFuel
	case l.LateItemID:
		return calc.RentalLineLate
	case set.DamageItemID:
		return calc.RentalLineDamage
	}
	return calc.RentalLineExtra
}

func (r *run) rentalRevenue(in *calc.RentalInput, set *store.RentalSettings) error {
	rows, err := store.ReportRentalChargeLines(r.ctx, r.tx, r.scope(), in.From, in.To)
	if err != nil {
		return err
	}
	for _, l := range rows {
		in.ChargeLine = append(in.ChargeLine, calc.RentalChargeLine{
			ChargeID: l.ChargeID, IssuedOn: l.IssuedOn, BookingID: l.BookingID, BookingNumber: rentalNumber(l.BookingYear, l.BookingNumber),
			UnitID: l.UnitID, UnitCode: l.UnitCode, Category: l.Category, VesselID: l.VesselID, FormulaID: l.FormulaID,
			FormulaCode: l.FormulaCode, LineKind: rentalLineKind(l, set), Net: money.Cents(l.NetCents), VAT: money.Cents(l.VATCents),
			Gross: money.Cents(l.GrossCents),
		})
	}
	return nil
}

func (r *run) rentalDamages(in *calc.RentalInput) error {
	rows, err := store.ReportRentalDamages(r.ctx, r.tx, r.scope(), r.localDayStart(in.From), r.localDayStart(in.To.AddDays(1)))
	if err != nil {
		return err
	}
	for _, d := range rows {
		in.Damages = append(in.Damages, calc.RentalDamageRow{
			ID: d.ID, Date: calendar.In(d.CreatedAt, r.loc), BookingID: d.BookingID, BookingNumber: rentalNumber(d.BookingYear, d.BookingNumber),
			UnitID: d.UnitID, UnitCode: d.UnitCode, Category: d.Category, VesselID: d.VesselID, Status: d.Status,
			Estimate: money.Cents(d.EstimateCents), Retained: money.Cents(d.RetainedCents),
		})
	}
	return nil
}

func (r *run) rentalDeposits(in *calc.RentalInput) error {
	rows, err := store.ReportRentalDeposits(r.ctx, r.tx, r.scope(), in.From, in.To)
	if err != nil {
		return err
	}
	for _, d := range rows {
		in.Deposits = append(in.Deposits, calc.RentalDepositRow{
			BookingID: d.BookingID, BookingNumber: rentalNumber(d.BookingYear, d.BookingNumber), Date: d.StartsOn, UnitID: d.UnitID,
			UnitCode: d.UnitCode, Category: d.Category, VesselID: d.VesselID, FormulaID: d.FormulaID, FormulaCode: d.FormulaCode,
			Mode: d.Mode, Status: d.Status, Deposit: money.Cents(d.DepositCents), Retained: money.Cents(d.RetainedCents),
		})
	}
	return nil
}
