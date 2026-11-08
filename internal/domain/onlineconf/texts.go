package onlineconf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// TextKinds are the kinds of booking texts.
var TextKinds = []string{store.BookingTextTerms, store.BookingTextPrivacy, store.BookingTextMarketing}

// MaxTextRunes bounds one language of a booking text.
const MaxTextRunes = 100_000

// publishAttempts bounds the retries of a publish racing another publish of the same kind.
const publishAttempts = 3

func validKind(k string) bool {
	for _, x := range TextKinds {
		if x == k {
			return true
		}
	}
	return false
}

// TextSHA256 is the hex SHA-256 of the canonical JSON of a text ({"it":..,"en":..}); it is stored with the text and
// copied on every consent that accepts it.
func TextSHA256(t LongText) string {
	b, _ := json.Marshal(t) // a struct of strings always marshals
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func textFromRow(r store.BookingText) (Text, error) {
	var lt LongText
	if err := json.Unmarshal(r.Texts, &lt); err != nil {
		return Text{}, fmt.Errorf("onlineconf: decode booking text %s: %w", r.ID, err)
	}
	return Text{
		ID: r.ID, Kind: r.Kind, Version: r.Version, Texts: lt, SHA256: r.SHA256, PublishedAt: r.PublishedAt,
		PublishedBy: r.PublishedByUserID,
	}, nil
}

func textsFromRows(rows []store.BookingText) ([]Text, error) {
	out := make([]Text, 0, len(rows))
	for _, r := range rows {
		t, err := textFromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// ListTexts returns the published versions of the texts, newest first, optionally of one kind (marina.configure).
func (s *Service) ListTexts(ctx context.Context, p *auth.Principal, marinaID, kind string) ([]Text, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	if kind != "" && !validKind(kind) {
		v := &auth.ValidationError{}
		v.Add("kind", "invalid")
		return nil, v
	}
	rows, err := store.ListBookingTexts(ctx, s.d.DB.DB, scope, kind)
	if err != nil {
		return nil, err
	}
	return textsFromRows(rows)
}

// PublishText publishes the next version of a text kind (marina.configure). Published versions are immutable
// (insert-only, trigger); a new version replaces the current one for new consents only.
func (s *Service) PublishText(ctx context.Context, p *auth.Principal, marinaID, kind string, texts LongText) (*Text, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	texts.It, texts.En = strings.TrimSpace(texts.It), strings.TrimSpace(texts.En)
	v := &auth.ValidationError{}
	if !validKind(kind) {
		v.Add("kind", "invalid")
	}
	if texts.It == "" {
		v.Add("texts.it", "required")
	}
	if utf8.RuneCountInString(texts.It) > MaxTextRunes || utf8.RuneCountInString(texts.En) > MaxTextRunes {
		v.Add("texts", "too_long")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	var out *Text
	for attempt := 0; attempt < publishAttempts; attempt++ {
		err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			if err := store.LockBookingTextKind(ctx, tx, scope, kind); err != nil {
				return err
			}
			next, err := store.NextBookingTextVersion(ctx, tx, scope, kind)
			if err != nil {
				return err
			}
			now := store.UTC(s.d.Clock.Now())
			row := &store.BookingText{
				ID: store.NewID(), MarinaID: scope.ID(), Kind: kind, Version: next, Texts: store.MustJSON(texts),
				SHA256: TextSHA256(texts), PublishedAt: now, PublishedByUserID: p.UserID, CreatedAt: now,
			}
			if err := store.InsertBookingText(ctx, tx, row); err != nil {
				return err
			}
			if s.d.Audit != nil {
				if err := s.d.Audit.Write(ctx, tx, audit.Entry{
					OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "booking_text.publish",
					EntityType: "booking_text", EntityID: row.ID, Diff: map[string]any{"kind": kind, "version": next, "sha256": row.SHA256},
				}); err != nil {
					return err
				}
			}
			t, err := textFromRow(*row)
			out = &t
			return err
		})
		if !errors.Is(err, store.ErrConflict) {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CurrentTexts returns the current published version of each kind in scope (public config, consent check).
func CurrentTexts(ctx context.Context, db bun.IDB, scope store.MarinaScope) ([]Text, error) {
	rows, err := store.CurrentBookingTexts(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	return textsFromRows(rows)
}

// CheckConsentVersion returns the current text of kind when version names it, ErrTextOutdated when it names
// another version or no text of the kind is published.
func CheckConsentVersion(current []Text, kind string, version int) (*Text, error) {
	for i := range current {
		if current[i].Kind == kind {
			if current[i].Version == version {
				return &current[i], nil
			}
			return nil, ErrTextOutdated
		}
	}
	return nil, ErrTextOutdated
}

// UserNames returns the display names of the staff users that published texts, approved bookings or verified
// documents of the marina (desk views; staff names are not customer data). The principal must see the marina.
func (s *Service) UserNames(ctx context.Context, p *auth.Principal, marinaID string, ids []string) (map[string]string, error) {
	if _, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaRead); err != nil {
		return nil, err
	}
	return store.UserNames(ctx, s.d.DB.DB, ids)
}
