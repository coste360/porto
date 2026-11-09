package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// ---- skippers (rental.manage) ----

// contact is the encrypted contact of a skipper (rental_skippers.contact_enc, KeyRing AAD rental-skipper:<id>).
type contact struct {
	Phone string `json:"phone,omitempty"`
	Email string `json:"email,omitempty"`
}

func skipperAAD(id string) string { return "rental-skipper:" + id }

func (s *Service) sealContact(id string, c contact) (string, error) {
	if c.Phone == "" && c.Email == "" {
		return "", nil
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return s.d.Keys.EncryptString(string(raw), skipperAAD(id))
}

func (s *Service) openContact(k *store.RentalSkipper) contact {
	var c contact
	if k.ContactEnc == "" || s.d.Keys == nil {
		return c
	}
	raw, err := s.d.Keys.DecryptString(k.ContactEnc, skipperAAD(k.ID))
	if err != nil {
		s.d.Log.Warn("rental skipper contact unreadable", "skipper_id", k.ID)
		return c
	}
	_ = json.Unmarshal([]byte(raw), &c)
	return c
}

func (s *Service) skipperViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.RentalSkipper) ([]SkipperView, error) {
	out := make([]SkipperView, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, k := range rows {
		ids = append(ids, k.ID)
	}
	hours, err := store.RentalSkipperHoursOf(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		c := s.openContact(&rows[i])
		h := hours[rows[i].ID]
		if h == nil {
			h = []store.RentalSkipperHours{}
		}
		out = append(out, SkipperView{RentalSkipper: rows[i], Phone: c.Phone, Email: c.Email, Hours: h})
	}
	return out, nil
}

func (s *Service) skipperView(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (*SkipperView, error) {
	k, err := store.GetRentalSkipper(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	views, err := s.skipperViews(ctx, db, scope, []store.RentalSkipper{*k})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

// ListSkippers lists the skippers with their hours.
func (s *Service) ListSkippers(ctx context.Context, p *auth.Principal, marinaID string, f SkipperFilter) ([]SkipperView, string, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage)
	if err != nil {
		return nil, "", err
	}
	page, err := store.ListRentalSkippers(ctx, s.d.DB.DB, scope, f.Active, f.Cursor, f.Limit)
	if err != nil {
		return nil, "", err
	}
	items, err := s.skipperViews(ctx, s.d.DB.DB, scope, page.Items)
	return items, page.NextCursor, err
}

func (s *Service) validateSkipper(ctx context.Context, db bun.IDB, scope store.MarinaScope, k *store.RentalSkipper, c contact) error {
	v := &auth.ValidationError{}
	if n := len([]rune(k.Name)); n < 1 || n > 80 {
		v.Add("name", "invalid")
	}
	if len(c.Phone) > 32 || strings.ContainsAny(c.Phone, "\r\n") {
		v.Add("phone", "invalid")
	}
	if c.Email != "" {
		if a, err := mail.ParseAddress(c.Email); err != nil || a.Address != c.Email || len(c.Email) > 254 {
			v.Add("email", "invalid")
		}
	}
	if k.UserID != "" {
		if _, err := store.MarinaMembership(ctx, db, scope, k.UserID); err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			v.Add("user_id", "not_found")
		}
	}
	return v.Err()
}

func skipperDiff(k *store.RentalSkipper) map[string]any {
	d := map[string]any{"active": k.Active, "has_user": k.UserID != ""}
	if !k.LicenceExpiresOn.IsZero() {
		d["licence_expires_on"] = k.LicenceExpiresOn.String()
	}
	return d
}

func (s *Service) writeSkipper(ctx context.Context, tx bun.IDB, scope store.MarinaScope, k *store.RentalSkipper, c contact, expected int) error {
	if err := s.validateSkipper(ctx, tx, scope, k, c); err != nil {
		return err
	}
	enc, err := s.sealContact(k.ID, c)
	if err != nil {
		return err
	}
	k.ContactEnc = enc
	if expected == 0 {
		err = store.InsertRentalSkipper(ctx, tx, scope, k)
	} else {
		err = store.UpdateRentalSkipper(ctx, tx, scope, k, expected, s.now())
	}
	if err != nil {
		if store.IsUniqueViolation(err) {
			v := &auth.ValidationError{}
			v.Add("user_id", "taken")
			return v
		}
		return versionErr(err)
	}
	return nil
}

// CreateSkipper adds a skipper.
func (s *Service) CreateSkipper(ctx context.Context, p *auth.Principal, marinaID string, in SkipperInput) (*SkipperView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage)
	if err != nil {
		return nil, err
	}
	now := s.now()
	k := &store.RentalSkipper{
		ID: store.NewID(), Name: strings.TrimSpace(in.Name), LicenceExpiresOn: in.LicenceExpiresOn, UserID: in.UserID,
		Active: in.Active == nil || *in.Active, Sort: in.Sort, CreatedAt: now, UpdatedAt: now,
	}
	c := contact{Phone: strings.TrimSpace(in.Phone), Email: strings.TrimSpace(in.Email)}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := s.writeSkipper(ctx, tx, scope, k, c, 0); err != nil {
			return err
		}
		return s.audit(ctx, tx, scope, p.Actor(), "rental_skipper.create", "rental_skipper", k.ID, skipperDiff(k))
	})
	if err != nil {
		return nil, err
	}
	return s.skipperView(ctx, s.d.DB.DB, scope, k.ID)
}

// UpdateSkipper changes a skipper (If-Match).
func (s *Service) UpdateSkipper(ctx context.Context, p *auth.Principal, marinaID, skipperID string, ifMatch int, in SkipperPatch) (*SkipperView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage, store.RentalTableSkippers, skipperID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		k, err := store.GetRentalSkipper(ctx, tx, scope, skipperID)
		if err != nil {
			return notFound(err)
		}
		if k.Version != ifMatch {
			return ErrVersionMismatch
		}
		c := s.openContact(k)
		setStr(&k.Name, in.Name)
		setStr(&c.Phone, in.Phone)
		setStr(&c.Email, in.Email)
		if in.LicenceExpiresOn != nil {
			k.LicenceExpiresOn = *in.LicenceExpiresOn
		}
		if in.ClearLicence {
			k.LicenceExpiresOn = calendar.Date{}
		}
		setStr(&k.UserID, in.UserID)
		if in.Active != nil {
			k.Active = *in.Active
		}
		setInt(&k.Sort, in.Sort)
		if err := s.writeSkipper(ctx, tx, scope, k, c, ifMatch); err != nil {
			return err
		}
		return s.audit(ctx, tx, scope, p.Actor(), "rental_skipper.update", "rental_skipper", k.ID, skipperDiff(k))
	})
	if err != nil {
		return nil, err
	}
	return s.skipperView(ctx, s.d.DB.DB, scope, skipperID)
}

// validateHours checks the weekly rows: weekday 1..7, 0 <= from < to <= 48, no overlap on the same weekday.
func validateHours(rows []HoursRow) error {
	v := &auth.ValidationError{}
	if len(rows) > 7*8 {
		v.Add("hours", "too_many")
		return v
	}
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(a, b HoursRow) int {
		if a.Weekday != b.Weekday {
			return a.Weekday - b.Weekday
		}
		return a.FromSlot - b.FromSlot
	})
	for i, r := range sorted {
		if r.Weekday < 1 || r.Weekday > 7 {
			v.Add("hours.weekday", "out_of_range")
			break
		}
		if r.FromSlot < 0 || r.ToSlot > 48 || r.FromSlot >= r.ToSlot {
			v.Add("hours.slots", "invalid")
			break
		}
		if i > 0 && sorted[i-1].Weekday == r.Weekday && sorted[i-1].ToSlot > r.FromSlot {
			v.Add("hours", "overlap")
			break
		}
	}
	return v.Err()
}

// PutSkipperHours replaces the weekly hours of a skipper (If-Match on the skipper version).
func (s *Service) PutSkipperHours(ctx context.Context, p *auth.Principal, marinaID, skipperID string, ifMatch int, rows []HoursRow) (*SkipperView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage, store.RentalTableSkippers, skipperID)
	if err != nil {
		return nil, err
	}
	if err := validateHours(rows); err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.TouchRentalSkipper(ctx, tx, scope, skipperID, ifMatch, s.now()); err != nil {
			return versionErr(err)
		}
		recs := make([]store.RentalSkipperHours, 0, len(rows))
		for _, r := range rows {
			recs = append(recs, store.RentalSkipperHours{Weekday: r.Weekday, FromSlot: r.FromSlot, ToSlot: r.ToSlot})
		}
		if err := store.ReplaceRentalSkipperHours(ctx, tx, scope, skipperID, recs); err != nil {
			return err
		}
		return s.audit(ctx, tx, scope, p.Actor(), "rental_skipper.hours", "rental_skipper", skipperID, map[string]any{"rows": len(rows)})
	})
	if err != nil {
		return nil, err
	}
	return s.skipperView(ctx, s.d.DB.DB, scope, skipperID)
}
