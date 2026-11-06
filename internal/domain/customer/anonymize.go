package customer

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Anonymize erases the personal data of a customer (customers.write, explicit confirmation; m1-plan §4.4,
// D15) in one transaction: names, contacts, fiscal data, IBAN, address and notes are blanked and the search
// text emptied; the customer's documents (with their files), deadlines and vessel parties are deleted. The
// consent history is kept as legal proof (it holds no personal data). File blobs are removed after commit.
// Audit rows of customers only ever hold field names, so nothing personal is left in the audit log.
func (s *Service) Anonymize(ctx context.Context, p *auth.Principal, marinaID, id string, confirm bool) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetCustomer(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if !confirm {
			return &auth.ValidationError{Fields: []auth.FieldError{{Field: "confirm", Code: "required"}}}
		}
		if !row.AnonymizedAt.IsZero() {
			return ErrAnonymized
		}
		now := s.now()
		docs, err := store.DocumentsOfCustomer(ctx, tx, scope, row.ID)
		if err != nil {
			return err
		}
		if err := store.DeleteDeadlinesOfCustomer(ctx, tx, scope, row.ID); err != nil {
			return err
		}
		var keys []string
		for i := range docs {
			d := &docs[i]
			if err := store.DeleteDeadlinesOfSource(ctx, tx, scope, "document", d.ID, ""); err != nil {
				return err
			}
			if err := store.DeleteDocument(ctx, tx, scope, d.ID, 0); err != nil {
				return err
			}
			if d.FileID != "" {
				key, err := store.SoftDeleteStoredFile(ctx, tx, scope, d.FileID, now)
				if err == nil {
					keys = append(keys, key)
				} else if !errors.Is(err, store.ErrNotFound) {
					return err
				}
			}
		}
		if err := store.DeletePartiesOfCustomer(ctx, tx, scope, row.ID); err != nil {
			return err
		}
		blank(row, now)
		if err := store.UpdateCustomer(ctx, tx, scope, row.Version, row); err != nil {
			return versionErr(err)
		}
		if err := s.writeAudit(ctx, tx, scope, p, "customer.anonymize", row.ID, map[string]any{"documents_deleted": len(docs)}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.CustomerAnonymized, event.CustomerData{CustomerID: row.ID}); err != nil {
			return err
		}
		store.AfterCommit(ctx, func() {
			for _, k := range keys {
				if err := s.d.Files.Remove(k); err != nil {
					s.d.Log.WarnContext(ctx, "customer: remove anonymised document blob", "err", err)
				}
			}
		})
		return nil
	})
}

// blank removes every personal field of a customer row.
func blank(c *store.Customer, now time.Time) {
	c.FirstName, c.LastName, c.CompanyName = "", "", ""
	c.FiscalCode, c.VATNumber, c.SDICode, c.PEC = "", "", "", ""
	c.BillingAddress = store.MustJSON(Address{})
	c.Nationality = ""
	c.Emails, c.Phones = store.JSON("[]"), store.JSON("[]")
	c.Tags = store.Strings{}
	c.IBANEnc, c.IBANLast4 = "", ""
	c.Notes = ""
	c.SearchText = ""
	c.AnonymizedAt = now
	c.UpdatedAt = now
}
