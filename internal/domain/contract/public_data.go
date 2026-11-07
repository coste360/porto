package contract

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Data the customer provides on the public quote acceptance page (m4-plan §3.2, spec 13.3 step 2): the required
// fields of the contract settings that the registry does not hold yet. Text data is saved on the customer and
// vessel in the acceptance transaction; documents (insurance, identity) come from the upload operation.

// PublicData is what the customer fills in when accepting through a quote link.
type PublicData struct {
	Address            *customer.Address
	FiscalCode         string
	VesselRegistration string
}

// identityDocTypes are the M1 document types that satisfy identity_document.
var identityDocTypes = []string{"identity_card", "passport", "driving_licence"}

// missingFields returns the required fields with no data for the customer and vessel of a quote starting on
// starts. birth_date is not stored by the registry and is never reported missing.
func missingFields(ctx context.Context, db bun.IDB, scope store.MarinaScope, required []string, customerID, vesselID string, starts calendar.Date) ([]string, error) {
	out := []string{}
	if len(required) == 0 {
		return out, nil
	}
	cu, err := store.GetCustomer(ctx, db, scope, customerID)
	if err != nil {
		return nil, err
	}
	var ve *store.Vessel
	if vesselID != "" {
		if ve, err = store.GetVessel(ctx, db, scope, vesselID); err != nil {
			return nil, err
		}
	}
	var insured *bool
	for _, f := range required {
		missing := false
		switch f {
		case "address":
			var a customer.Address
			_ = json.Unmarshal(cu.BillingAddress, &a)
			missing = strings.TrimSpace(a.Line1) == ""
		case "fiscal_code":
			missing = cu.FiscalCode == "" && cu.VATNumber == ""
		case "vessel_registration":
			missing = ve != nil && ve.Registration == ""
		case "insurance_expiry", "insurance_document":
			if insured == nil {
				ok, err := deadline.InsuranceValid(ctx, db, scope, vesselID, starts)
				if err != nil {
					return nil, err
				}
				insured = &ok
			}
			missing = vesselID != "" && !*insured
		case "identity_document":
			docs, err := store.DocumentsOfCustomer(ctx, db, scope, customerID)
			if err != nil {
				return nil, err
			}
			missing = !slices.ContainsFunc(docs, func(d store.Document) bool { return slices.Contains(identityDocTypes, d.Type) })
		}
		if missing {
			out = append(out, f)
		}
	}
	return out, nil
}

// clip trims s and reports whether it fits n runes.
func clip(s string, n int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, len([]rune(s)) <= n
}

// savePublicDataTx stores the data given on the acceptance page on the customer and vessel (audit and event in
// tx, actor system: the customer acted through the link), then checks that no required field is still missing.
// Registry data already present is never overwritten from a link.
func (s *Service) savePublicDataTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, required []string, customerID, vesselID string, starts calendar.Date, in PublicData) error {
	missing, err := missingFields(ctx, tx, scope, required, customerID, vesselID, starts)
	if err != nil || len(missing) == 0 {
		return err
	}
	v := &auth.ValidationError{}
	cu, err := store.GetCustomer(ctx, tx, scope, customerID)
	if err != nil {
		return err
	}
	var custFields []string
	if slices.Contains(missing, "address") && in.Address != nil {
		a := *in.Address
		ok := true
		for _, p := range []struct {
			v *string
			n int
		}{{&a.Line1, 200}, {&a.Line2, 200}, {&a.PostalCode, 16}, {&a.City, 100}, {&a.Province, 8}, {&a.Country, 2}} {
			var fits bool
			*p.v, fits = clip(*p.v, p.n)
			ok = ok && fits
		}
		switch {
		case !ok:
			v.Add("customer.address", "too_long")
		case a.Line1 != "":
			cu.BillingAddress = store.MustJSON(a)
			custFields = append(custFields, "billing_address")
		}
	}
	if slices.Contains(missing, "fiscal_code") && in.FiscalCode != "" {
		fc := customer.NormalizeCode(in.FiscalCode)
		valid := customer.ValidPersonFiscalCode(fc)
		if cu.Kind == customer.KindCompany {
			valid = customer.ValidCompanyFiscalCode(fc)
		}
		if !valid {
			v.Add("customer.fiscal_code", "fiscal_code_invalid")
		} else {
			cu.FiscalCode = fc
			custFields = append(custFields, "fiscal_code")
		}
	}
	var ve *store.Vessel
	if slices.Contains(missing, "vessel_registration") && in.VesselRegistration != "" && vesselID != "" {
		reg, ok := clip(strings.ToUpper(in.VesselRegistration), 64)
		if !ok {
			v.Add("vessel.registration", "too_long")
		} else if ve, err = store.GetVessel(ctx, tx, scope, vesselID); err != nil {
			return err
		} else {
			ve.Registration = reg
		}
	}
	if err := v.Err(); err != nil {
		return err
	}
	now := s.now()
	if len(custFields) > 0 {
		cu.UpdatedAt, cu.SearchText = now, customer.SearchText(cu)
		if err := store.UpdateCustomer(ctx, tx, scope, cu.Version, cu); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, linkActor, "customer.update", "customer", cu.ID,
			map[string]any{"fields": custFields, "via": ViaLink}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, event.CustomerUpdated, event.CustomerUpdatedData{CustomerID: cu.ID, Change: event.ChangeUpdated}); err != nil {
			return err
		}
	}
	if ve != nil {
		ve.UpdatedAt = now
		if err := store.UpdateVessel(ctx, tx, scope, ve.Version, ve); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, linkActor, "vessel.update", "vessel", ve.ID,
			map[string]any{"fields": []string{"registration"}, "via": ViaLink}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, event.VesselChanged, event.VesselChangedData{VesselID: ve.ID, Change: event.ChangeUpdated}); err != nil {
			return err
		}
	}
	still, err := missingFields(ctx, tx, scope, required, customerID, vesselID, starts)
	if err != nil {
		return err
	}
	for _, f := range still {
		v.Add("missing_fields."+f, "required")
	}
	return v.Err()
}
