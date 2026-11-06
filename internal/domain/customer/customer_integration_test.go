//go:build integration

package customer_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

type env struct {
	*fixture.Env
	files *filestore.Store
	cust  *customer.Service
	ves   *vessel.Service
	docs  *document.Service
	dl    *deadline.Service
}

func newEnv(t *testing.T, db *store.DB) *env {
	t.Helper()
	e := fixture.New(t, db)
	files := filestore.New(t.TempDir(), e.Keys)
	return &env{
		Env: e, files: files,
		cust: customer.NewService(customer.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
		ves:  vessel.NewService(vessel.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
		docs: document.NewService(document.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
		dl:   deadline.NewService(deadline.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
	}
}

func ptr[T any](v T) *T { return &v }

func person(first, last, fiscal, email string) customer.Patch {
	p := customer.Patch{Kind: ptr(customer.KindPerson), FirstName: &first, LastName: &last}
	if fiscal != "" {
		p.FiscalCode = &fiscal
	}
	if email != "" {
		p.Emails = &[]customer.Contact{{Label: "home", Value: email}}
	}
	return p
}

func fieldCode(err error, field string) string {
	var v *auth.ValidationError
	if !errors.As(err, &v) {
		return ""
	}
	for _, f := range v.Fields {
		if f.Field == field {
			return f.Code
		}
	}
	return ""
}

func (e *env) create(t *testing.T, p customer.Patch) *customer.Customer {
	t.Helper()
	res, err := e.cust.Create(fixture.Ctx(), e.Owner, e.MarinaID, p)
	if err != nil {
		t.Fatal(err)
	}
	return &res.Customer
}

func (e *env) vessel(t *testing.T, name string, parties ...vessel.PartyInput) *vessel.Vessel {
	t.Helper()
	loa, beam := int64(1200), int64(400)
	v, err := e.ves.Create(fixture.Ctx(), e.Owner, e.MarinaID, vessel.Patch{Name: &name, LOACm: &loa, BeamCm: &beam}, parties)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCustomer_Scenario(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		in := person("Mario", "Rossi", "rssmra85t10a562s", "Mario.Rossi@Example.com")
		in.Phones = &[]customer.Contact{{Label: "mobile", Value: "333 123 4567"}}
		in.IBAN = ptr("it60 x054 2811 1010 0000 0123 456")
		in.Tags = &[]string{"vip", " vip ", "socio"}
		res, err := e.cust.Create(ctx, e.Owner, e.MarinaID, in)
		if err != nil {
			t.Fatal(err)
		}
		c := res.Customer
		if c.FiscalCode != "RSSMRA85T10A562S" || c.Emails[0].Value != "mario.rossi@example.com" || c.Phones[0].Value != "+393331234567" ||
			c.IBANMasked != "IT•• •••• 3456" || c.DisplayName != "Rossi Mario" || c.Version != 1 || !slices.Equal(c.Tags, []string{"vip", "socio"}) ||
			len(res.PossibleDuplicates) != 0 || c.Locale != "it" {
			t.Fatalf("created %+v", c)
		}
		// A second customer with the same fiscal code is created and flagged.
		dup, err := e.cust.Create(ctx, e.Owner, e.MarinaID, person("M.", "Rossi", "RSSMRA85T10A562S", ""))
		if err != nil {
			t.Fatal(err)
		}
		if len(dup.PossibleDuplicates) != 1 || dup.PossibleDuplicates[0].ID != c.ID || dup.PossibleDuplicates[0].PrimaryEmail != "mario.rossi@example.com" {
			t.Fatalf("duplicates %+v", dup.PossibleDuplicates)
		}
		// Shared email is a duplicate too.
		byEmail, _ := e.cust.Create(ctx, e.Owner, e.MarinaID, person("Anna", "Bianchi", "", "MARIO.ROSSI@example.com"))
		if len(byEmail.PossibleDuplicates) != 1 {
			t.Fatalf("email duplicates %+v", byEmail.PossibleDuplicates)
		}
		// Validation codes.
		for field, p := range map[string]customer.Patch{
			"fiscal_code": person("A", "B", "RSSMRA85T10A562X", ""),
			"vat_number":  {Kind: ptr(customer.KindCompany), CompanyName: ptr("Acme"), VATNumber: ptr("IT01234567890")},
			"iban":        {Kind: ptr(customer.KindPerson), LastName: ptr("B"), IBAN: ptr("IT61X0542811101000000123456")},
			"last_name":   {Kind: ptr(customer.KindPerson)},
			"kind":        {LastName: ptr("B")},
		} {
			_, err := e.cust.Create(ctx, e.Owner, e.MarinaID, p)
			want := map[string]string{"fiscal_code": "fiscal_code_invalid", "vat_number": "vat_invalid", "iban": "iban_invalid", "last_name": "required", "kind": "required"}[field]
			if got := fieldCode(err, field); got != want {
				t.Errorf("%s: %v (code %q, want %q)", field, err, got, want)
			}
		}
		// Company with a numeric fiscal code and VAT.
		co := e.create(t, customer.Patch{Kind: ptr(customer.KindCompany), CompanyName: ptr("Nautica Srl"), FiscalCode: ptr("01234567897"), VATNumber: ptr("IT01234567897")})
		if co.DisplayName != "Nautica Srl" {
			t.Fatalf("company %+v", co)
		}
		// Patch: stale version, then a real change.
		if _, err := e.cust.Update(ctx, e.Owner, e.MarinaID, c.ID, 7, customer.Patch{Notes: ptr("x")}); !errors.Is(err, customer.ErrVersionMismatch) {
			t.Fatalf("stale patch: %v", err)
		}
		up, err := e.cust.Update(ctx, e.Owner, e.MarinaID, c.ID, 1, customer.Patch{Notes: ptr("ok"), Category: ptr("member"), IBAN: ptr("")})
		if err != nil || up.Version != 2 || up.Notes != "ok" || up.Category != "member" || up.IBANMasked != "" {
			t.Fatalf("patch %+v %v", up, err)
		}
		// Archive hides from the default list.
		if err := e.cust.Archive(ctx, e.Owner, e.MarinaID, c.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.cust.Archive(ctx, e.Owner, e.MarinaID, c.ID); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("double archive: %v", err)
		}
		active, _ := e.cust.List(ctx, e.Owner, e.MarinaID, customer.Filter{}, "", 0)
		archived, _ := e.cust.List(ctx, e.Owner, e.MarinaID, customer.Filter{Archived: true}, "", 0)
		if len(active.Items) != 3 || len(archived.Items) != 1 || archived.Items[0].ID != c.ID {
			t.Fatalf("lists %d %d", len(active.Items), len(archived.Items))
		}
		if err := e.cust.Unarchive(ctx, e.Owner, e.MarinaID, c.ID); err != nil {
			t.Fatal(err)
		}
		// Paging.
		p1, _ := e.cust.List(ctx, e.Owner, e.MarinaID, customer.Filter{}, "", 2)
		p2, _ := e.cust.List(ctx, e.Owner, e.MarinaID, customer.Filter{}, p1.NextCursor, 2)
		if len(p1.Items) != 2 || p1.NextCursor == "" || len(p2.Items) != 2 || p2.NextCursor != "" || p1.Items[1].ID == p2.Items[0].ID {
			t.Fatalf("paging %d %q %d %q", len(p1.Items), p1.NextCursor, len(p2.Items), p2.NextCursor)
		}
		// Permissions and isolation.
		viewer := e.AddUser(t, "viewer@porto.test", auth.RoleViewer, e.OrgID, e.MarinaID)
		if _, err := e.cust.Get(ctx, viewer, e.MarinaID, c.ID); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("viewer: %v", err)
		}
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		if _, err := e.cust.Get(ctx, dock, e.MarinaID, c.ID); err != nil {
			t.Fatalf("dock read: %v", err)
		}
		if _, err := e.cust.Create(ctx, dock, e.MarinaID, person("X", "Y", "", "")); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("dock write: %v", err)
		}
		_, otherMarina, otherOwner := e.OtherOrganization(t)
		if _, err := e.cust.Get(ctx, otherOwner, otherMarina, c.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina: %v", err)
		}
		if _, err := e.cust.Get(ctx, otherOwner, e.MarinaID, c.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("foreign principal: %v", err)
		}
		if n := len(e.Events(t, event.CustomerCreated)); n != 4 {
			t.Fatalf("customer.created events: %d", n)
		}
		if n := len(e.Events(t, event.CustomerUpdated)); n != 3 { // update, archive, unarchive
			t.Fatalf("customer.updated events: %d", n)
		}
	})
}

func TestCustomer_SearchByVessel(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		in := person("Niccolò", "D'Àlba", "", "nico@example.com")
		in.Phones = &[]customer.Contact{{Label: "m", Value: "+39 347 765 4321"}}
		in.Tags = &[]string{"vip"}
		owner := e.create(t, in)
		other := e.create(t, person("Giulia", "Verdi", "", ""))
		reg := "RM 12345"
		v := e.vessel(t, "Stella Maris", vessel.PartyInput{CustomerID: owner.ID, Role: "owner", ShareMillis: ptr(int64(1000))})
		if _, err := e.ves.Update(ctx, e.Owner, e.MarinaID, v.Row.ID, v.Row.Version, vessel.Patch{Registration: &reg}); err != nil {
			t.Fatal(err)
		}
		for q, want := range map[string][]string{
			"stella":   {owner.ID},
			"RM12345":  {owner.ID},
			"rm 12345": {owner.ID},
			"niccolo":  {owner.ID},
			"d'alba":   {owner.ID},
			"347 765":  {owner.ID},
			"nico@exa": {owner.ID},
			"verdi":    {other.ID},
			"nothing":  {},
			"100%":     {},
			"_":        {},
			"":         {other.ID, owner.ID},
		} {
			page, err := e.cust.List(ctx, e.Owner, e.MarinaID, customer.Filter{Search: q}, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, it := range page.Items {
				got = append(got, it.ID)
			}
			if len(got) != len(want) || (len(got) > 0 && fmt.Sprint(got) != fmt.Sprint(want)) {
				t.Errorf("q=%q: %v, want %v", q, got, want)
			}
		}
		tagged, _ := e.cust.List(ctx, e.Owner, e.MarinaID, customer.Filter{Tag: "vip"}, "", 0)
		if len(tagged.Items) != 1 || tagged.Items[0].ID != owner.ID || tagged.Items[0].VesselCount != 1 {
			t.Fatalf("tag %+v", tagged.Items)
		}
		// Vessel search and the customer filter of the vessel list.
		vs, _ := e.ves.List(ctx, e.Owner, e.MarinaID, vessel.Filter{CustomerID: owner.ID}, "", 0)
		if len(vs.Items) != 1 || len(vs.Items[0].Owners) != 1 || vs.Items[0].Owners[0].DisplayName != "D'Àlba Niccolò" {
			t.Fatalf("vessels of owner %+v", vs.Items)
		}
		// Another marina never sees these customers.
		other2 := e.AddMarina(t, "seconda")
		page, err := e.cust.List(ctx, e.Owner, other2, customer.Filter{Search: "stella"}, "", 0)
		if err != nil || len(page.Items) != 0 {
			t.Fatalf("other marina search: %v %d", err, len(page.Items))
		}
	})
}

func TestCustomer_MergeMovesEverything(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		target := e.create(t, person("Mario", "Rossi", "RSSMRA85T10A562S", ""))
		source := e.create(t, person("Mario", "Rossi", "RSSMRA85T10A562S", "m@example.com"))
		shared := e.vessel(t, "Condivisa",
			vessel.PartyInput{CustomerID: target.ID, Role: "owner", ShareMillis: ptr(int64(400))},
			vessel.PartyInput{CustomerID: source.ID, Role: "owner", ShareMillis: ptr(int64(600))})
		own := e.vessel(t, "Sua", vessel.PartyInput{CustomerID: source.ID, Role: "skipper"})
		exp := calendar.MustParse("2027-01-10")
		doc, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{OwnerType: "customer", CustomerID: source.ID, Type: "passport", ExpiresOn: exp})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.cust.RecordConsent(ctx, e.Owner, e.MarinaID, source.ID, customer.ConsentInput{Kind: "privacy", PolicyVersion: "v1", Granted: true, Source: "desk"}); err != nil {
			t.Fatal(err)
		}
		// Stale target version, self merge and unknown ids are refused.
		if _, err := e.cust.Merge(ctx, e.Owner, e.MarinaID, target.ID, 9, []string{source.ID}); !errors.Is(err, customer.ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		if _, err := e.cust.Merge(ctx, e.Owner, e.MarinaID, target.ID, 1, []string{target.ID}); fieldCode(err, "source_ids") != "invalid" {
			t.Fatalf("self: %v", err)
		}
		if _, err := e.cust.Merge(ctx, e.Owner, e.MarinaID, target.ID, 1, []string{"01900000-0000-7000-8000-000000000009"}); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("unknown: %v", err)
		}
		merged, err := e.cust.Merge(ctx, e.Owner, e.MarinaID, target.ID, 1, []string{source.ID})
		if err != nil {
			t.Fatal(err)
		}
		if merged.Version != 2 {
			t.Fatalf("target version %d", merged.Version)
		}
		sv, _ := e.ves.Get(ctx, e.Owner, e.MarinaID, shared.Row.ID)
		if len(sv.Parties) != 1 || sv.Parties[0].CustomerID != target.ID || sv.Parties[0].ShareMillis != 1000 {
			t.Fatalf("shared vessel parties %+v", sv.Parties)
		}
		ov, _ := e.ves.Get(ctx, e.Owner, e.MarinaID, own.Row.ID)
		if len(ov.Parties) != 1 || ov.Parties[0].CustomerID != target.ID || ov.Parties[0].Role != "skipper" {
			t.Fatalf("own vessel parties %+v", ov.Parties)
		}
		d, _ := e.docs.Get(ctx, e.Owner, e.MarinaID, doc.Row.ID)
		if d.Row.CustomerID != target.ID {
			t.Fatalf("document owner %s", d.Row.CustomerID)
		}
		dls, _ := e.dl.ForCustomer(ctx, e.Owner, e.MarinaID, target.ID, nil, true)
		if len(dls) != 1 || dls[0].Customer.ID != target.ID {
			t.Fatalf("deadlines %+v", dls)
		}
		cs, _ := e.cust.ListConsents(ctx, e.Owner, e.MarinaID, target.ID)
		if len(cs) != 1 {
			t.Fatalf("consents %d", len(cs))
		}
		src, _ := e.cust.Get(ctx, e.Owner, e.MarinaID, source.ID)
		if src.MergedIntoID != target.ID || src.ArchivedAt.IsZero() {
			t.Fatalf("source %+v", src)
		}
		// A merged customer can no longer change.
		if _, err := e.cust.Update(ctx, e.Owner, e.MarinaID, source.ID, src.Version, customer.Patch{Notes: ptr("x")}); !errors.Is(err, customer.ErrMerged) {
			t.Fatalf("update merged: %v", err)
		}
		if _, err := e.cust.Merge(ctx, e.Owner, e.MarinaID, target.ID, 2, []string{source.ID}); !errors.Is(err, customer.ErrMerged) {
			t.Fatalf("merge again: %v", err)
		}
		evs := e.Events(t, event.CustomerMerged)
		if len(evs) != 1 || !strings.Contains(string(evs[0].Payload), source.ID) {
			t.Fatalf("merged events %+v", evs)
		}
		if e.AuditCount(t, "customer.merge") != 1 || e.AuditCount(t, "customer.merged_into") != 1 {
			t.Fatal("merge audit missing")
		}
	})
}

// rawRow returns every column of a row as text.
func rawRow(t *testing.T, db *store.DB, table, id string) string {
	t.Helper()
	row := map[string]any{}
	if err := db.NewSelect().Table(table).Where("id = ?", id).Scan(context.Background(), &row); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for k, v := range row {
		if bs, ok := v.([]byte); ok {
			v = string(bs)
		}
		fmt.Fprintf(&b, "%s=%v\n", k, v)
	}
	return b.String()
}

func auditText(t *testing.T, db *store.DB) string {
	t.Helper()
	var rows []map[string]any
	if err := db.NewSelect().Table("audit_log").Scan(context.Background(), &rows); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, r := range rows {
		for k, v := range r {
			if bs, ok := v.([]byte); ok {
				v = string(bs)
			}
			fmt.Fprintf(&b, "%s=%v\n", k, v)
		}
	}
	return b.String()
}

func TestCustomer_AnonymizeScrubsPII(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		pii := []string{
			"Ermenegilda", "Scognamiglio", "RSSMRA85T10A562S", "ermenegilda@example.com", "393471112233", "3471112233",
			"IT60X0542811101000000123456", "Via Segreta 7", "nota riservata", "AB1234567",
		}
		in := person("Ermenegilda", "Scognamiglio", "RSSMRA85T10A562S", "ermenegilda@example.com")
		in.Phones = &[]customer.Contact{{Label: "m", Value: "347 111 2233"}}
		in.IBAN = ptr("IT60X0542811101000000123456")
		in.BillingAddress = &customer.Address{Line1: "Via Segreta 7", City: "Roma", Country: "IT"}
		in.Notes = ptr("nota riservata")
		c := e.create(t, in)
		up, err := e.cust.Update(ctx, e.Owner, e.MarinaID, c.ID, 1, customer.Patch{PEC: ptr("ermenegilda@example.com")})
		if err != nil {
			t.Fatal(err)
		}
		v := e.vessel(t, "Barca", vessel.PartyInput{CustomerID: c.ID, Role: "owner", ShareMillis: ptr(int64(1000))})
		doc, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{OwnerType: "customer", CustomerID: c.ID, Type: "identity_card", Number: "AB1234567", ExpiresOn: calendar.MustParse("2027-05-01")})
		if err != nil {
			t.Fatal(err)
		}
		pdf := []byte("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n")
		if _, err := e.docs.UploadFile(ctx, e.Owner, e.MarinaID, doc.Row.ID, "application/pdf", "carta.pdf", bytes.NewReader(pdf)); err != nil {
			t.Fatal(err)
		}
		if _, err := e.cust.RecordConsent(ctx, e.Owner, e.MarinaID, c.ID, customer.ConsentInput{Kind: "privacy", PolicyVersion: "v1", Granted: true, Source: "desk"}); err != nil {
			t.Fatal(err)
		}
		if err := e.cust.Anonymize(ctx, e.Owner, e.MarinaID, c.ID, false); fieldCode(err, "confirm") != "required" {
			t.Fatalf("without confirm: %v", err)
		}
		if err := e.cust.Anonymize(ctx, e.Owner, e.MarinaID, c.ID, true); err != nil {
			t.Fatal(err)
		}
		raw := rawRow(t, db, "customers", c.ID)
		audits := auditText(t, db)
		for _, s := range pii {
			if strings.Contains(strings.ToLower(raw), strings.ToLower(s)) {
				t.Errorf("customer row still holds %q", s)
			}
			if strings.Contains(strings.ToLower(audits), strings.ToLower(s)) {
				t.Errorf("audit log holds %q", s)
			}
		}
		got, _ := e.cust.Get(ctx, e.Owner, e.MarinaID, c.ID)
		if got.AnonymizedAt.IsZero() || got.DisplayName != "" || got.IBANMasked != "" || len(got.Emails) != 0 || got.Version != up.Version+1 {
			t.Fatalf("anonymised %+v", got)
		}
		if _, err := e.docs.Get(ctx, e.Owner, e.MarinaID, doc.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("document kept: %v", err)
		}
		blobs := 0
		_ = filepath.WalkDir(e.files.Dir(), func(_ string, d fs.DirEntry, _ error) error {
			if d != nil && !d.IsDir() {
				blobs++
			}
			return nil
		})
		if blobs != 0 {
			t.Fatalf("%d blobs left", blobs)
		}
		dls, _ := e.dl.ForCustomer(ctx, e.Owner, e.MarinaID, c.ID, nil, true)
		vv, _ := e.ves.Get(ctx, e.Owner, e.MarinaID, v.Row.ID)
		cs, _ := e.cust.ListConsents(ctx, e.Owner, e.MarinaID, c.ID)
		if len(dls) != 0 || len(vv.Parties) != 0 || len(cs) != 1 {
			t.Fatalf("deadlines %d parties %d consents %d", len(dls), len(vv.Parties), len(cs))
		}
		if page, _ := e.cust.List(ctx, e.Owner, e.MarinaID, customer.Filter{Search: "ermenegilda"}, "", 0); len(page.Items) != 0 {
			t.Fatal("anonymised customer still found by name")
		}
		if err := e.cust.Anonymize(ctx, e.Owner, e.MarinaID, c.ID, true); !errors.Is(err, customer.ErrAnonymized) {
			t.Fatalf("second anonymise: %v", err)
		}
		if _, err := e.cust.RecordConsent(ctx, e.Owner, e.MarinaID, c.ID, customer.ConsentInput{Kind: "privacy", PolicyVersion: "v2", Granted: true, Source: "desk"}); !errors.Is(err, customer.ErrAnonymized) {
			t.Fatalf("consent after anonymise: %v", err)
		}
		if len(e.Events(t, event.CustomerAnonymized)) != 1 {
			t.Fatal("customer.anonymized not in the outbox")
		}
	})
}

func TestCustomer_AuditDiffHasFieldNamesOnly(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		in := person("Gualtiero", "Pistacchio", "RSSMRA85T10A562S", "gualtiero@example.com")
		in.IBAN = ptr("IT60X0542811101000000123456")
		c := e.create(t, in)
		if _, err := e.cust.Update(ctx, e.Owner, e.MarinaID, c.ID, 1, customer.Patch{LastName: ptr("Mandorla"), Notes: ptr("segreto")}); err != nil {
			t.Fatal(err)
		}
		v := e.vessel(t, "Nome Barca")
		if _, err := e.ves.Update(ctx, e.Owner, e.MarinaID, v.Row.ID, 1, vessel.Patch{Name: ptr("Nuovo Nome"), Registration: ptr("GE99999")}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{OwnerType: "customer", CustomerID: c.ID, Type: "passport", Number: "YA7654321"}); err != nil {
			t.Fatal(err)
		}
		audits := auditText(t, db)
		for _, s := range []string{"Gualtiero", "Pistacchio", "Mandorla", "segreto", "RSSMRA85T10A562S", "gualtiero@", "IT60X", "Nuovo Nome", "GE99999", "YA7654321"} {
			if strings.Contains(audits, s) {
				t.Errorf("audit log holds %q", s)
			}
		}
		for _, field := range []string{"last_name", "notes", "registration", "number"} {
			if !strings.Contains(audits, `"`+field+`"`) {
				t.Errorf("audit log misses the field name %q", field)
			}
		}
	})
}

// 100 concurrent updates with the same If-Match: exactly one wins, the version goes up by one.
func TestConcurrency_CustomerUpdateSingleWinner(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		c := e.create(t, person("Mario", "Rossi", "", ""))
		const n = 100
		errs := make(chan error, n)
		for i := range n {
			go func() {
				_, err := e.cust.Update(fixture.Ctx(), e.Owner, e.MarinaID, c.ID, 1, customer.Patch{Notes: ptr(fmt.Sprintf("note %d", i))})
				errs <- err
			}()
		}
		ok, stale := 0, 0
		for range n {
			switch err := <-errs; {
			case err == nil:
				ok++
			case errors.Is(err, customer.ErrVersionMismatch):
				stale++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}
		got, _ := e.cust.Get(fixture.Ctx(), e.Owner, e.MarinaID, c.ID)
		if ok != 1 || stale != n-1 || got.Version != 2 {
			t.Fatalf("ok %d stale %d version %d", ok, stale, got.Version)
		}
		if n := e.AuditCount(t, "customer.update"); n != 1 {
			t.Fatalf("audit rows %d", n)
		}
	})
}
