package charge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/chargeplan"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Fiscal modes (charges.fiscal_mode).
const (
	FiscalInvoice = "invoice"
	FiscalReceipt = "receipt"
)

// selfOrigins are the origins whose origin_id is the charge itself.
var selfOrigins = map[string]bool{store.ChargeOriginManual: true, store.ChargeOriginCounterSale: true}

// FormatNumber formats a charge number (A{yyyy}-{n:06}, m5-plan §16 D17).
func FormatNumber(year int, n int64) string { return fmt.Sprintf("A%04d-%06d", year, n) }

// FiscalSnapshot is the customer fiscal data frozen on a charge (encrypted, AAD charge-fiscal:<id>): refreshed at
// the first export unless the customer is anonymised.
type FiscalSnapshot struct {
	Kind        string        `json:"kind"` // person, company
	Name        string        `json:"name"`
	FiscalCode  string        `json:"fiscal_code,omitempty"`
	VATNumber   string        `json:"vat_number,omitempty"`
	SDICode     string        `json:"sdi_code,omitempty"`
	PEC         string        `json:"pec,omitempty"`
	Address     FiscalAddress `json:"address"`
	Anonymised  bool          `json:"anonymised,omitempty"`
	CustomerRef string        `json:"customer_ref,omitempty"` // customer id (for joins in memory only)
}

// FiscalAddress is the billing address of a snapshot.
type FiscalAddress struct {
	Line1      string `json:"line1"`
	Line2      string `json:"line2,omitempty"`
	PostalCode string `json:"postal_code"`
	City       string `json:"city"`
	Province   string `json:"province"`
	Country    string `json:"country"`
}

// Incomplete reports whether the snapshot lacks what an invoice needs: a fiscal code or VAT number, and an SDI code
// or PEC for companies (the preview warning customer_fiscal_incomplete).
func (f FiscalSnapshot) Incomplete() bool {
	if f.FiscalCode == "" && f.VATNumber == "" {
		return true
	}
	return f.Kind == "company" && f.SDICode == "" && f.PEC == ""
}

func snapshotOf(c *store.Customer) FiscalSnapshot {
	name := strings.TrimSpace(c.FirstName + " " + c.LastName)
	if c.Kind == "company" && c.CompanyName != "" {
		name = c.CompanyName
	}
	f := FiscalSnapshot{
		Kind: c.Kind, Name: name, FiscalCode: c.FiscalCode, VATNumber: c.VATNumber, SDICode: c.SDICode, PEC: c.PEC,
		Anonymised: !c.AnonymizedAt.IsZero(), CustomerRef: c.ID,
	}
	_ = json.Unmarshal(c.BillingAddress, &f.Address)
	return f
}

func fiscalAAD(chargeID string) string { return "charge-fiscal:" + chargeID }

// encryptSnapshot encrypts a snapshot for a charge ("" without a key ring).
func (s *Service) encryptSnapshot(f FiscalSnapshot, chargeID string) (string, error) {
	if s.d.Keys == nil {
		return "", nil
	}
	b, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	return s.d.Keys.EncryptString(string(b), fiscalAAD(chargeID))
}

// DecryptSnapshot decrypts the fiscal snapshot of a charge (zero value when absent). In memory only: never logged.
func (s *Service) DecryptSnapshot(c *store.Charge) (FiscalSnapshot, error) {
	var f FiscalSnapshot
	if c.FiscalSnapshotEnc == "" || s.d.Keys == nil {
		return f, nil
	}
	raw, err := s.d.Keys.DecryptString(c.FiscalSnapshotEnc, fiscalAAD(c.ID))
	if err != nil {
		return f, err
	}
	err = json.Unmarshal([]byte(raw), &f)
	return f, err
}

// customerSnapshot loads a customer of the marina and returns its encrypted snapshot for chargeID.
func (s *Service) customerSnapshot(ctx context.Context, tx bun.IDB, scope store.MarinaScope, customerID, chargeID string) (string, error) {
	cs, err := store.CustomersByIDs(ctx, tx, scope, []string{customerID})
	if err != nil {
		return "", err
	}
	if len(cs) == 0 {
		return "", auth404()
	}
	return s.encryptSnapshot(snapshotOf(&cs[0]), chargeID)
}

func auth404() error { return notFound(store.ErrNotFound) }

// CreateTx creates a charge with its lines in tx: number, totals from the lines, fiscal snapshot, audit
// charge.create and charge.created. A computed total of 0 creates nothing (nil, nil, nil). A negative charge is
// applied at creation: offset to the charge it corrects up to its residual, the rest as customer credit
// (m5-plan §3.5). A second live primary charge of the same origin is ErrChargeExists (INV-54; callers look the
// existing one up first under the origin lock).
func (s *Service) CreateTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in CreateInput) (*store.Charge, []store.ChargeLine, error) {
	if len(in.Lines) == 0 {
		return nil, nil, invalid("lines", "required")
	}
	if in.FiscalMode == "" {
		in.FiscalMode = FiscalInvoice
	}
	if in.CustomerID == "" && in.FiscalMode != FiscalReceipt {
		return nil, nil, ErrCustomerRequired
	}
	now := s.now()
	today := s.today(ctx, tx, scope)
	if in.IssuedOn.IsZero() {
		in.IssuedOn = today
	}
	if in.DueOn.IsZero() {
		bs, err := store.GetBillingSettings(ctx, tx, scope, now)
		if err != nil {
			return nil, nil, err
		}
		in.DueOn = in.IssuedOn.AddDays(bs.DefaultDueDays)
	}
	id := store.NewID()
	lines, totals, err := buildLines(scope, id, in.Lines, now)
	if err != nil {
		return nil, nil, err
	}
	if totals.gross == 0 {
		return nil, nil, nil
	}
	if in.OriginID == "" {
		if !selfOrigins[in.OriginType] {
			return nil, nil, invalid("origin_id", "required")
		}
		in.OriginID = id
	}
	n, err := store.NextBillingNumber(ctx, tx, scope, store.SeriesCharge, in.IssuedOn.Year())
	if err != nil {
		return nil, nil, err
	}
	label, userID := actorLabel(actor)
	c := &store.Charge{
		ID: id, MarinaID: scope.ID(), Number: FormatNumber(in.IssuedOn.Year(), n), CustomerID: in.CustomerID,
		OriginType: in.OriginType, OriginID: in.OriginID, ContractID: in.ContractID, VesselID: in.VesselID,
		RightID: in.RightID, IssuedOn: in.IssuedOn, DueOn: in.DueOn, ServiceFrom: totals.from, ServiceTo: totals.to,
		TotalCents: int64(totals.gross), NetCents: int64(totals.net), VATCents: int64(totals.vat),
		PaymentStatus: store.ChargeOpen, InvoicingStatus: store.ChargeToInvoice, FiscalMode: in.FiscalMode,
		CorrectsChargeID: in.CorrectsChargeID, CorrectionKind: in.CorrectionKind, Description: in.Description,
		CreatedByUserID: userID, CreatedByActor: label, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if c.TotalCents < 0 {
		c.PaymentStatus, c.CreditedCents = store.ChargePaid, c.TotalCents
	}
	if in.CustomerID != "" {
		if c.FiscalSnapshotEnc, err = s.customerSnapshot(ctx, tx, scope, in.CustomerID, id); err != nil {
			return nil, nil, err
		}
	}
	if err := store.InsertCharge(ctx, tx, c, lines); err != nil {
		return nil, nil, mapStore(err)
	}
	if in.Invoiced {
		if err := store.MarkChargeInvoicedAtCreation(ctx, tx, scope, c.ID, in.InvoiceNumber, in.InvoiceDate, now); err != nil {
			return nil, nil, mapStore(err)
		}
		c.InvoicingStatus, c.ExternalInvoiceNumber, c.ExternalInvoiceDate, c.InvoicedAt = store.ChargeInvoiced, in.InvoiceNumber, in.InvoiceDate, now
	}
	diff := map[string]any{
		"number": c.Number, "origin_type": c.OriginType, "origin_id": c.OriginID, "total_cents": c.TotalCents,
		"net_cents": c.NetCents, "vat_cents": c.VATCents, "issued_on": c.IssuedOn, "due_on": c.DueOn, "lines": len(lines),
	}
	if c.CorrectsChargeID != "" {
		diff["corrects_charge_id"], diff["correction_kind"] = c.CorrectsChargeID, c.CorrectionKind
	}
	if err := s.write(ctx, tx, scope, actor, "charge.create", "charge", c.ID, diff); err != nil {
		return nil, nil, err
	}
	if err := s.emit(ctx, tx, scope, event.ChargeCreated, event.ChargeCreatedData{
		ChargeID: c.ID, OriginType: c.OriginType, OriginID: c.OriginID, ContractID: c.ContractID, CustomerID: c.CustomerID,
		TotalCents: c.TotalCents, IssuedOn: c.IssuedOn, DueOn: c.DueOn, CorrectsChargeID: c.CorrectsChargeID,
	}); err != nil {
		return nil, nil, err
	}
	if c.TotalCents < 0 && !in.NoCredit {
		if err := s.applyNegativeTx(ctx, tx, scope, actor, c); err != nil {
			return nil, nil, err
		}
	}
	return c, lines, nil
}

// applyNegativeTx applies a negative charge: an offset to the corrected charge up to its residual, the rest as a
// credit payment through the CreditGranter.
func (s *Service) applyNegativeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Charge) error {
	left := -c.TotalCents
	if c.CorrectsChargeID != "" {
		// Lock order as for payments (contract or right, then the charge), because the instalment follows below.
		if err := s.lockOriginParentTx(ctx, tx, scope, c.CorrectsChargeID); err != nil {
			return err
		}
		target, err := store.LockCharge(ctx, tx, scope, c.CorrectsChargeID)
		if err != nil {
			return err
		}
		if off := min(left, target.ResidualCents()); off > 0 {
			ok, err := store.ApplyOffset(ctx, tx, scope, c.ID, target.ID, off, s.now())
			if err != nil {
				return mapStore(err)
			}
			if !ok {
				return fmt.Errorf("charge: offset of %s refused", target.ID)
			}
			left -= off
			if err := s.write(ctx, tx, scope, actor, "charge.offset", "charge", target.ID, map[string]any{
				"negative_charge_id": c.ID, "amount_cents": off,
			}); err != nil {
				return err
			}
			if err := s.emit(ctx, tx, scope, event.ChargeChanged, event.ChargeChangedData{ChargeID: target.ID, Change: event.ChangeOffset}); err != nil {
				return err
			}
			if target.PaidAmountCents+target.CreditedCents+off == target.TotalCents {
				if err := s.emit(ctx, tx, scope, event.ChargePaid, event.ChargePaidData{
					ChargeID: target.ID, TotalCents: target.TotalCents, PaidAmountCents: target.PaidAmountCents,
				}); err != nil {
					return err
				}
			}
			if f, ok := s.credits.(OriginFollower); ok && f != nil {
				if err := f.FollowChargeOriginTx(ctx, tx, scope, actor, target.ID); err != nil {
					return err
				}
			}
		}
	}
	if left == 0 {
		return nil
	}
	if c.CustomerID == "" {
		return nil // walk-in receipts have no customer to credit
	}
	if s.credits == nil {
		return errors.New("charge: no credit granter registered")
	}
	return s.credits.GrantAdjustmentCreditTx(ctx, tx, scope, actor, c, money.Cents(left))
}

// lockOriginParentTx locks the contract or the berth right of an instalment or holder-instalment charge.
func (s *Service) lockOriginParentTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, chargeID string) error {
	ch, err := store.GetCharge(ctx, tx, scope, chargeID)
	if err != nil {
		return mapStore(err)
	}
	switch {
	case ch.OriginType == store.ChargeOriginInstallment && ch.ContractID != "":
		_, err = store.LockContract(ctx, tx, scope, ch.ContractID)
	case ch.OriginType == store.ChargeOriginHolderInstallment && ch.RightID != "":
		_, err = store.LockBerthRight(ctx, tx, scope, ch.RightID)
	}
	return err
}

type lineTotals struct {
	net, vat, gross money.Cents
	from, to        calendar.Date
}

// buildLines turns line inputs into rows with their sequence and the charge totals; net + vat = gross per line.
func buildLines(scope store.MarinaScope, chargeID string, in []LineInput, now time.Time) ([]store.ChargeLine, lineTotals, error) {
	var t lineTotals
	out := make([]store.ChargeLine, 0, len(in))
	for i, l := range in {
		if l.Net+l.VAT != l.Gross {
			return nil, t, fmt.Errorf("charge: line %d net + vat != gross", i+1)
		}
		if (l.VATRateBP == 0) != (l.VATNature != "") {
			return nil, t, invalid(fmt.Sprintf("lines[%d].vat_nature", i), "invalid")
		}
		qty := l.Qty
		if qty == 0 {
			qty = 1000
		}
		out = append(out, store.ChargeLine{
			ID: store.NewID(), MarinaID: scope.ID(), ChargeID: chargeID, Seq: i + 1, ItemID: l.ItemID, ItemCode: l.ItemCode,
			Description: l.Description, QtyMilli: int64(qty), UnitCents: int64(l.Unit), DiscountCents: int64(l.Discount),
			NetCents: int64(l.Net), VATCents: int64(l.VAT), GrossCents: int64(l.Gross), VATRateBP: l.VATRateBP,
			VATNature: l.VATNature, PriceIncludesVAT: l.PriceIncludesVAT, RevenueClassCode: l.RevenueClassCode,
			ExternalCode: l.ExternalCode, ServiceFrom: l.ServiceFrom, ServiceTo: l.ServiceTo, PlanKey: l.PlanKey,
			CreatedAt: now, UpdatedAt: now,
		})
		t.net += l.Net
		t.vat += l.VAT
		t.gross += l.Gross
		if !l.ServiceFrom.IsZero() && (t.from.IsZero() || l.ServiceFrom.Before(t.from)) {
			t.from = l.ServiceFrom
		}
		if !l.ServiceTo.IsZero() && (t.to.IsZero() || l.ServiceTo.After(t.to)) {
			t.to = l.ServiceTo
		}
	}
	return out, t, nil
}

// linesFromParts converts planned parts into line inputs (plan key kept).
func linesFromParts(parts []chargeplan.Part) []LineInput {
	out := make([]LineInput, 0, len(parts))
	for _, p := range parts {
		l := p.Line
		out = append(out, LineInput{
			ItemID: l.ItemID, ItemCode: l.ItemCode, Description: l.Description, Qty: p.Qty, Unit: p.Unit,
			Net: p.Net, VAT: p.VAT, Gross: p.Gross, VATRateBP: l.VATRateBP, VATNature: l.VATNature,
			PriceIncludesVAT: l.PriceIncludesVAT, RevenueClassCode: l.RevenueClass, ExternalCode: l.ExternalCode,
			ServiceFrom: l.ServiceFrom, ServiceTo: l.ServiceTo, PlanKey: l.Key,
		})
	}
	return out
}

// chargedOf converts charge lines with a plan key into charged lines for chargeplan.Remaining.
func chargedOf(lines []store.ChargeLine) []chargeplan.ChargedLine {
	out := make([]chargeplan.ChargedLine, 0, len(lines))
	for _, l := range lines {
		if l.PlanKey == "" {
			continue
		}
		pl := planLineOf(l)
		out = append(out, chargeplan.ChargedLine{Key: l.PlanKey, GrossCents: money.Cents(l.GrossCents), VATCents: money.Cents(l.VATCents), Line: &pl})
	}
	return out
}

// planLineOf describes a stored line as a plan line (item and VAT data, amounts of the line).
func planLineOf(l store.ChargeLine) chargeplan.Line {
	return chargeplan.Line{
		Key: l.PlanKey, ItemID: l.ItemID, ItemCode: l.ItemCode, Description: l.Description, RevenueClass: l.RevenueClassCode,
		ExternalCode: l.ExternalCode, Qty: qtyOf(l.QtyMilli), VATRateBP: l.VATRateBP, VATNature: l.VATNature,
		PriceIncludesVAT: l.PriceIncludesVAT, Net: money.Cents(l.NetCents), VAT: money.Cents(l.VATCents),
		Gross: money.Cents(l.GrossCents), ServiceFrom: l.ServiceFrom, ServiceTo: l.ServiceTo,
	}
}
