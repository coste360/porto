package charge

import (
	"context"
	"errors"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// The charges.exporter job (m5-plan §3.6): exports created with an exporter_code are sent to the registered
// ext.ChargeExporter after commit (never inside a transaction), then polled for the invoice references, applied
// like a reconciliation with source exporter. Porto ships no exporter (spec 33 D4): without one the job is a no-op.

// exporterActor is the actor of the job.
var exporterActor = audit.Actor{Type: audit.ActorSystem, ID: "charges.exporter"}

func (s *Service) exporter(code string) ext.ChargeExporter {
	for _, x := range s.d.Exporters {
		if x != nil && x.Code() == code {
			return x
		}
	}
	return nil
}

// ExporterCodes returns the codes of the registered exporters.
func (s *Service) ExporterCodes() []string {
	var out []string
	for _, x := range s.d.Exporters {
		if x != nil {
			out = append(out, x.Code())
		}
	}
	return out
}

// RunExporter is the job charges.exporter: deliver pending exporter exports and poll sent ones, every marina.
func (s *Service) RunExporter(ctx context.Context) error {
	if len(s.d.Exporters) == 0 {
		return nil
	}
	ids, err := store.MarinasWithBillingWork(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, id)
		if err != nil {
			continue
		}
		if err := s.runExporterMarina(ctx, scope); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) runExporterMarina(ctx context.Context, scope store.MarinaScope) error {
	db := s.d.DB.DB
	pending, err := store.ExporterExports(ctx, db, scope, "pending", 20)
	if err != nil {
		return err
	}
	for i := range pending {
		e := &pending[i]
		x := s.exporter(e.ExporterCode)
		if x == nil {
			continue
		}
		batch, err := s.batchOf(ctx, db, scope, e)
		if err != nil {
			return err
		}
		status, code, ref := "sent", "", ""
		receipt, err := x.Export(ctx, batch)
		if err != nil {
			status, code = "failed", errorCode(err)
		} else if receipt != nil {
			ref = receipt.Ref
		}
		if err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			if err := store.SetChargeExportDelivery(ctx, tx, scope, e.ID, status, code, ref, s.now()); err != nil {
				return err
			}
			return s.write(ctx, tx, scope, exporterActor, "charge_export.deliver", "charge_export", e.ID, map[string]any{
				"exporter_code": e.ExporterCode, "status": status, "error_code": code,
			})
		}); err != nil {
			return err
		}
	}
	sent, err := store.ExporterExports(ctx, db, scope, "sent", 20)
	if err != nil {
		return err
	}
	for i := range sent {
		e := &sent[i]
		x := s.exporter(e.ExporterCode)
		if x == nil {
			continue
		}
		refs, err := x.Poll(ctx, ext.ExportReceipt{Ref: e.DeliveryRef, SentAt: e.UpdatedAt})
		if err != nil || len(refs) == 0 {
			continue
		}
		rows := make([]ReconcileRow, 0, len(refs))
		for j, r := range refs {
			rows = append(rows, ReconcileRow{Row: j + 1, ChargeID: r.ChargeID, InvoiceNumber: r.Number, InvoiceDate: r.Date})
		}
		if err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			_, _, err := s.reconcileTx(ctx, tx, scope, exporterActor, "exporter", "", e.ID, rows, false)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// errorCode reduces an exporter error to a short code (never its message, which may echo data).
func errorCode(err error) string {
	var c interface{ Code() string }
	if errors.As(err, &c) {
		return truncate(c.Code(), 64)
	}
	return "exporter_error"
}

// batchOf builds the batch of an export (fiscal data decrypted in memory only for the call).
func (s *Service) batchOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, e *store.ChargeExport) (ext.ChargeBatch, error) {
	charges, err := s.LoadExportCharges(ctx, db, scope, e.ID)
	if err != nil {
		return ext.ChargeBatch{}, err
	}
	out := ext.ChargeBatch{MarinaID: scope.ID(), ExportID: e.ID, Number: e.Number, Currency: "EUR"}
	for _, r := range ExportRows(charges) {
		c := r.Charge
		if len(out.Charges) == 0 || out.Charges[len(out.Charges)-1].ChargeID != c.Charge.ID {
			f := c.Fiscal
			out.Charges = append(out.Charges, ext.ExportedCharge{
				ChargeID: c.Charge.ID, Number: c.Charge.Number, IssuedOn: c.Charge.IssuedOn.String(), DueOn: c.Charge.DueOn.String(),
				OriginType: c.Charge.OriginType, CorrectsNumber: c.CorrectsNumber, CommercialDocument: c.FiscalDocument != "",
				Customer: ext.FiscalParty{
					Name: f.Name, FiscalCode: f.FiscalCode, VATNumber: f.VATNumber, SDICode: f.SDICode, PEC: f.PEC,
					Street: strings.TrimSpace(f.Address.Line1 + " " + f.Address.Line2), PostalCode: f.Address.PostalCode,
					City: f.Address.City, Province: f.Address.Province, Country: f.Address.Country, Company: f.Kind == "company",
				},
				TotalCents: c.Charge.TotalCents, NetCents: c.Charge.NetCents, VATCents: c.Charge.VATCents,
			})
		}
		l := r.Line
		last := &out.Charges[len(out.Charges)-1]
		last.Lines = append(last.Lines, ext.ExportedLine{
			Seq: l.Seq, ItemCode: l.ItemCode, ExternalCode: l.ExternalCode, Description: l.Description, QtyMilli: l.QtyMilli,
			UnitCents: l.UnitCents, DiscountCents: l.DiscountCents, NetCents: l.NetCents, VATCents: l.VATCents,
			GrossCents: l.GrossCents, VATRateBP: l.VATRateBP, VATNature: l.VATNature, RevenueClass: l.RevenueClassCode,
			ServiceFrom: dateText(l.ServiceFrom.String(), l.ServiceFrom.IsZero()), ServiceTo: dateText(l.ServiceTo.String(), l.ServiceTo.IsZero()),
		})
	}
	return out, nil
}

func dateText(s string, zero bool) string {
	if zero {
		return ""
	}
	return s
}
