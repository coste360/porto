package customer

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// CSV import of customers with their vessels (spec 11.6; m4-plan §3.7, §15 D27). Each row is a customer and,
// optionally, one vessel owned by them; rows with the same fiscal code (or VAT number) are one customer with
// several vessels. Customers or vessels already in the marina (same fiscal code, VAT number or registration) are
// reported as duplicates and not created again. Reports carry row numbers and codes only, never personal data.

const (
	fKind         = "kind" // person (default), company
	fFirstName    = "first_name"
	fLastName     = "last_name"
	fCompanyName  = "company_name"
	fFiscalCode   = "fiscal_code"
	fVATNumber    = "vat_number"
	fEmail        = "email"
	fPhone        = "phone"
	fAddress      = "address_line1"
	fPostalCode   = "postal_code"
	fCity         = "city"
	fProvince     = "province"
	fCountry      = "country"
	fLocale       = "locale"
	fVesselName   = "vessel_name"
	fVesselReg    = "vessel_registration"
	fVesselLOA    = "vessel_loa_cm"
	fVesselBeam   = "vessel_beam_cm"
	fVesselDraft  = "vessel_draft_cm"
	fVesselHull   = "vessel_hull_type"
	importEntity  = "customer"
	importVessel  = "vessel"
	importDefault = "other"
)

var importHullTypes = []string{"monohull_motor", "monohull_sail", "catamaran", "trimaran", "rib", "gozzo", "other"}

// ImportHandler returns the CSV import handler of customers with their vessels (entity customers_vessels).
func (s *Service) ImportHandler() csvimport.EntityHandler { return customerImport{s: s} }

type customerImport struct{ s *Service }

func (customerImport) Entity() string { return csvimport.EntityCustomersVessels }

func (customerImport) Fields() []string {
	return []string{
		fKind, fFirstName, fLastName, fCompanyName, fFiscalCode, fVATNumber, fEmail, fPhone, fAddress, fPostalCode,
		fCity, fProvince, fCountry, fLocale, fVesselName, fVesselReg, fVesselLOA, fVesselBeam, fVesselDraft, fVesselHull,
	}
}

type importedVessel struct {
	row  *store.Vessel
	line int
}

type importedCustomer struct {
	key       string
	row       *store.Customer
	existing  bool // already in the marina: not created again
	vessels   []importedVessel
	firstLine int
}

func ptr(s string) *string { return &s }

// plan validates the rows and builds the customers and vessels to create (no writes).
func (h customerImport) plan(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []csvimport.Row, rep *csvimport.Report) ([]*importedCustomer, error) {
	var codes, regs []string
	for _, r := range rows {
		for _, f := range []string{fFiscalCode, fVATNumber} {
			if v := strings.ToUpper(strings.ReplaceAll(r.Get(f), " ", "")); v != "" {
				codes = append(codes, v)
			}
		}
		if v := strings.ToUpper(r.Get(fVesselReg)); v != "" {
			regs = append(regs, v)
		}
	}
	existingCustomers, err := store.CustomerIDsByFiscalCode(ctx, db, scope, codes)
	if err != nil {
		return nil, err
	}
	existingVessels, err := store.VesselIDsByRegistration(ctx, db, scope, regs)
	if err != nil {
		return nil, err
	}
	byKey := map[string]*importedCustomer{}
	var out []*importedCustomer
	seenRegs := map[string]int{}
	now := h.s.now()
	for _, r := range rows {
		kind := strings.ToLower(r.Get(fKind))
		if kind == "" {
			kind = KindPerson
		}
		if kind != KindPerson && kind != KindCompany {
			rep.Reject(r.Line, "kind_invalid", fKind, "")
			continue
		}
		fiscal := strings.ToUpper(strings.ReplaceAll(r.Get(fFiscalCode), " ", ""))
		vat := strings.ToUpper(strings.ReplaceAll(r.Get(fVATNumber), " ", ""))
		key := fiscal
		if key == "" {
			key = vat
		}
		if key == "" {
			key = "row:" + strconv.Itoa(r.Line)
		}
		c := byKey[key]
		if c == nil {
			c = &importedCustomer{key: key, firstLine: r.Line}
			if id := existingCustomers[fiscal]; fiscal != "" && id != "" {
				rep.Duplicate(r.Line, "customer_exists", "")
				c.existing, c.row = true, &store.Customer{ID: id}
			} else if id := existingCustomers[vat]; vat != "" && id != "" {
				rep.Duplicate(r.Line, "customer_exists", "")
				c.existing, c.row = true, &store.Customer{ID: id}
			} else {
				row := &store.Customer{ID: store.NewID(), MarinaID: scope.ID(), Version: 1, CreatedAt: now, UpdatedAt: now}
				p := Patch{Kind: ptr(kind)}
				set := func(dst **string, f string) {
					if v := r.Get(f); v != "" {
						*dst = ptr(v)
					}
				}
				set(&p.FirstName, fFirstName)
				set(&p.LastName, fLastName)
				set(&p.CompanyName, fCompanyName)
				if fiscal != "" {
					p.FiscalCode = &fiscal
				}
				if vat != "" {
					p.VATNumber = &vat
				}
				set(&p.Locale, fLocale)
				if v := r.Get(fEmail); v != "" {
					p.Emails = &[]Contact{{Label: "import", Value: v}}
				}
				if v := r.Get(fPhone); v != "" {
					p.Phones = &[]Contact{{Label: "import", Value: v}}
				}
				if a := (Address{Line1: r.Get(fAddress), PostalCode: r.Get(fPostalCode), City: r.Get(fCity), Province: r.Get(fProvince), Country: r.Get(fCountry)}); a != (Address{}) {
					p.BillingAddress = &a
				}
				if _, err := h.s.apply(row, p, true); err != nil {
					rejectFields(rep, r.Line, err)
					continue
				}
				c.row = row
			}
			byKey[key] = c
			out = append(out, c)
		}
		name := r.Get(fVesselName)
		reg := strings.ToUpper(r.Get(fVesselReg))
		if name == "" && reg == "" && r.Get(fVesselLOA) == "" {
			continue
		}
		loa, errL := strconv.ParseInt(r.Get(fVesselLOA), 10, 64)
		beam, errB := strconv.ParseInt(r.Get(fVesselBeam), 10, 64)
		var draft int64
		if d := r.Get(fVesselDraft); d != "" {
			var errD error
			if draft, errD = strconv.ParseInt(d, 10, 64); errD != nil || draft < 0 || draft > 5000 {
				rep.Reject(r.Line, "invalid", fVesselDraft, "")
				continue
			}
		}
		switch {
		case name == "":
			rep.Reject(r.Line, "required", fVesselName, "")
			continue
		case errL != nil || loa <= 0 || loa > 20000:
			rep.Reject(r.Line, "invalid", fVesselLOA, "")
			continue
		case errB != nil || beam <= 0 || beam > 5000:
			rep.Reject(r.Line, "invalid", fVesselBeam, "")
			continue
		}
		hull := strings.ToLower(r.Get(fVesselHull))
		if hull == "" {
			hull = importDefault
		}
		if !contains(importHullTypes, hull) {
			rep.Reject(r.Line, "invalid", fVesselHull, "")
			continue
		}
		if reg != "" {
			if existingVessels[reg] != "" {
				rep.Duplicate(r.Line, "vessel_exists", reg)
				continue
			}
			if prev, ok := seenRegs[reg]; ok {
				rep.Reject(r.Line, "duplicate_in_file", fVesselReg, strconv.Itoa(prev))
				continue
			}
			seenRegs[reg] = r.Line
		}
		v := &store.Vessel{
			ID: store.NewID(), MarinaID: scope.ID(), Name: name, Registration: reg, HullType: hull, LOACm: loa,
			BeamCm: beam, DraftCm: draft, DimensionsSource: "declared", Engines: store.JSON("[]"), Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		v.SearchText = strings.Join(strings.Fields(Fold(name)+" "+Fold(reg)+" "+strings.ReplaceAll(Fold(reg), " ", "")), " ")
		c.vessels = append(c.vessels, importedVessel{row: v, line: r.Line})
	}
	return out, nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// rejectFields records the field codes of a validation error (never the values).
func rejectFields(rep *csvimport.Report, line int, err error) {
	var v *auth.ValidationError
	if errors.As(err, &v) && len(v.Fields) > 0 {
		for _, f := range v.Fields {
			rep.Reject(line, f.Code, f.Field, "")
		}
		return
	}
	rep.Reject(line, "invalid", "", "")
}

func (h customerImport) DryRun(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []csvimport.Row, rep *csvimport.Report) error {
	_, err := h.plan(ctx, db, scope, rows, rep)
	return err
}

func (h customerImport) Apply(ctx context.Context, tx bun.IDB, scope store.MarinaScope, rows []csvimport.Row, rep *csvimport.Report) ([]csvimport.Created, error) {
	plan, err := h.plan(ctx, tx, scope, rows, rep)
	if err != nil || len(rep.Rejected) > 0 {
		return nil, err
	}
	var out []csvimport.Created
	now := h.s.now()
	for _, c := range plan {
		if !c.existing {
			if err := store.InsertCustomer(ctx, tx, c.row); err != nil {
				return nil, err
			}
			out = append(out, csvimport.Created{EntityType: importEntity, EntityID: c.row.ID})
		}
		for _, v := range c.vessels {
			if err := store.InsertVessel(ctx, tx, v.row); err != nil {
				return nil, err
			}
			if err := store.ReplaceVesselParties(ctx, tx, scope, v.row.ID, []store.VesselParty{{
				ID: store.NewID(), MarinaID: scope.ID(), VesselID: v.row.ID, CustomerID: c.row.ID, Role: "owner",
				ShareMillis: 1000, CreatedAt: now, UpdatedAt: now,
			}}); err != nil {
				return nil, err
			}
			out = append(out, csvimport.Created{EntityType: importVessel, EntityID: v.row.ID})
		}
	}
	return out, nil
}

// Undo deletes the imported vessels and customers while they are untouched (version 1 and no later records
// pointing at them); otherwise csvimport.ErrNotUndoable and the transaction rolls back.
func (h customerImport) Undo(ctx context.Context, tx bun.IDB, scope store.MarinaScope, created []csvimport.Created) error {
	for i := len(created) - 1; i >= 0; i-- {
		c := created[i]
		switch c.EntityType {
		case importVessel:
			if err := store.ReplaceVesselParties(ctx, tx, scope, c.EntityID, nil); err != nil {
				return err
			}
			ok, err := store.DeleteRow(ctx, tx, scope, "vessels", c.EntityID, 1)
			if err := undoErr(ok, err); err != nil {
				return err
			}
		case importEntity:
			ok, err := store.DeleteRow(ctx, tx, scope, "customers", c.EntityID, 1)
			if err := undoErr(ok, err); err != nil {
				return err
			}
		}
	}
	return nil
}

func undoErr(ok bool, err error) error {
	if err != nil {
		if store.IsForeignKeyViolation(err) {
			return csvimport.ErrNotUndoable
		}
		return err
	}
	if !ok {
		return csvimport.ErrNotUndoable
	}
	return nil
}
