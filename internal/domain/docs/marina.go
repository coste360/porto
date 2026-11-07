package docs

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/store"
)

type address struct {
	Line1, Line2, PostalCode, City, Province, Country string
}

func (a address) String() string {
	var parts []string
	street := strings.TrimSpace(strings.TrimSpace(a.Line1) + " " + strings.TrimSpace(a.Line2))
	if street != "" {
		parts = append(parts, street)
	}
	city := strings.TrimSpace(a.PostalCode + " " + a.City)
	if a.Province != "" {
		city += " (" + a.Province + ")"
	}
	if strings.TrimSpace(city) != "" {
		parts = append(parts, strings.TrimSpace(city))
	}
	return strings.Join(parts, ", ")
}

// FormatAddress formats a postal address JSON ({line1, line2, postal_code, city, province, country}).
func FormatAddress(raw []byte) string {
	var a struct {
		Line1      string `json:"line1"`
		Line2      string `json:"line2"`
		PostalCode string `json:"postal_code"`
		City       string `json:"city"`
		Province   string `json:"province"`
		Country    string `json:"country"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return address{a.Line1, a.Line2, a.PostalCode, a.City, a.Province, a.Country}.String()
}

// MarinaHeader returns the issuing marina data of a document (name, legal data of the organization, contacts and
// the bank details of the contract settings). Data sources of every package use it for views.Marina.
func MarinaHeader(ctx context.Context, db bun.IDB, scope store.MarinaScope, keys *crypto.KeyRing) (views.Marina, error) {
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil {
		return views.Marina{}, err
	}
	out := views.Marina{Name: m.Name, Address: FormatAddress(m.Address), ConcessionExpiresOn: m.ConcessionExpiresOn}
	var c struct {
		Email   string `json:"email"`
		Phone   string `json:"phone"`
		Website string `json:"website"`
	}
	_ = json.Unmarshal(m.Contacts, &c)
	out.Email, out.Phone, out.Website = c.Email, c.Phone, c.Website
	if org, err := getOrg(ctx, db, scope); err == nil {
		out.LegalName, out.VATNumber, out.FiscalCode = org.Name, org.VATNumber, org.FiscalCode
	}
	if cs, err := store.GetContractSettings(ctx, db, scope, m.UpdatedAt); err == nil && cs.BankDetailsEnc != "" && keys != nil {
		if raw, err := keys.DecryptString(cs.BankDetailsEnc, "bank-details:"+scope.ID()); err == nil {
			var b views.Bank
			if json.Unmarshal([]byte(raw), &b) == nil {
				out.Bank = b
			}
		}
	}
	return out, nil
}

// PartyOf returns a customer as printed on a document (first email and phone only).
func PartyOf(c *store.Customer) views.Party {
	p := views.Party{
		Code: shortCode(c.ID), Kind: c.Kind, FirstName: c.FirstName, LastName: c.LastName, CompanyName: c.CompanyName,
		FiscalCode: c.FiscalCode, VATNumber: c.VATNumber, Address: FormatAddress(c.BillingAddress),
	}
	p.DisplayName = strings.TrimSpace(c.FirstName + " " + c.LastName)
	if c.Kind == "company" && c.CompanyName != "" {
		p.DisplayName = c.CompanyName
	}
	p.Email = FirstContact(c.Emails)
	p.Phone = FirstContact(c.Phones)
	return p
}

// FirstContact returns the first value of an [{label, value}] contact list.
func FirstContact(raw []byte) string {
	var list []struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &list) != nil {
		return ""
	}
	for _, e := range list {
		if v := strings.TrimSpace(e.Value); v != "" {
			return v
		}
	}
	return ""
}

func shortCode(id string) string {
	if len(id) < 8 {
		return id
	}
	return strings.ToUpper(id[len(id)-8:])
}

// marinaSource is the data source of the marina kinds owned by docs: general conditions, harbour rules and
// letters to a customer (subject letter = the customer id).
type marinaSource struct{ s *Service }

func (marinaSource) Kinds() []Kind {
	return []Kind{KindGeneralConditions, KindHarbourRules, KindLetter}
}

func (m marinaSource) View(ctx context.Context, tx bun.IDB, scope store.MarinaScope, kind Kind, _ SubjectType, subjectID, _ string) (any, error) {
	mv, err := MarinaHeader(ctx, tx, scope, m.s.d.Keys)
	if err != nil {
		return nil, err
	}
	if kind != KindLetter {
		return views.MarinaView{Marina: mv}, nil
	}
	c, err := store.GetCustomer(ctx, tx, scope, subjectID)
	if err != nil {
		return nil, notFound(err)
	}
	return views.LetterView{Marina: mv, Customer: PartyOf(c)}, nil
}

func (marinaSource) DemoView(kind Kind, _ string) (any, error) { return DemoView(kind), nil }

func getOrg(ctx context.Context, db bun.IDB, scope store.MarinaScope) (*store.Organization, error) {
	os, err := store.ResolveOrgScope(ctx, db, scope.OrgID())
	if err != nil {
		return nil, err
	}
	return store.GetOrganization(ctx, db, os)
}
