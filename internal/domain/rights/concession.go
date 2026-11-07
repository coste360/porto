package rights

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// Concession card and obligations (spec 14.6; m4-plan §4.7). Reads need rights.manage, writes marina.configure.
// Updating expires_on also updates marinas.concession_expires_on in the same transaction (the default end of new
// rights). Concession fees are the marina's own cost: they appear only as deadlines (DOCUMENTS' engine reads
// concession_obligations), never as holder instalments.

// Surface is a surface of the concession.
type Surface struct {
	Kind   string `json:"kind"` // water, land, building, other
	AreaCm int64  `json:"area_cm2"`
	Note   string `json:"note,omitempty"`
}

// Concession is the concession card; Version 0 means none was stored yet.
type Concession struct {
	Authority      string
	DeedKind       string
	DeedNumber     string
	GrantedOn      calendar.Date
	ExpiresOn      calendar.Date
	AnnualFeeCents int64
	Surfaces       []Surface
	Prescriptions  string
	Status         string
	Version        int
}

// ConcessionInput are the fields of the card (nil = unchanged on an existing card, default on a new one).
type ConcessionInput struct {
	Authority      *string
	DeedKind       *string
	DeedNumber     *string
	GrantedOn      *calendar.Date
	ExpiresOn      *calendar.Date
	AnnualFeeCents *int64
	Surfaces       *[]Surface
	Prescriptions  *string
	Status         *string
}

// Obligation kinds and recurrences.
var (
	ObligationKinds  = []string{"fee_instalment", "deposit", "insurance", "periodic_report", "other"}
	Recurrences      = []string{"none", "yearly"}
	surfaceKinds     = []string{"water", "land", "building", "other"}
	concessionStatus = []string{"active", "expired", "revoked"}
)

// Obligation is a concession obligation.
type Obligation struct {
	ID          string
	Kind        string
	Description string
	DueOn       calendar.Date
	Recurrence  string
	AmountCents *int64
	DoneOn      calendar.Date
	Version     int
}

// ObligationInput are the fields of an obligation.
type ObligationInput struct {
	Kind        string
	Description string
	DueOn       calendar.Date
	Recurrence  string
	AmountCents *int64
}

func concessionOf(c *store.Concession) *Concession {
	out := &Concession{
		Authority: c.Authority, DeedKind: c.DeedKind, DeedNumber: c.DeedNumber, GrantedOn: c.GrantedOn, ExpiresOn: c.ExpiresOn,
		AnnualFeeCents: c.AnnualFeeCents, Surfaces: []Surface{}, Prescriptions: c.Prescriptions, Status: c.Status, Version: c.Version,
	}
	if len(c.Surfaces) > 0 {
		_ = json.Unmarshal(c.Surfaces, &out.Surfaces)
	}
	return out
}

func obligationOf(o *store.ConcessionObligation) Obligation {
	return Obligation{
		ID: o.ID, Kind: o.Kind, Description: o.Description, DueOn: o.DueOn, Recurrence: o.Recurrence,
		AmountCents: o.AmountCents, DoneOn: o.DoneOn, Version: o.Version,
	}
}

// GetConcession returns the concession card (rights.manage); an empty card at version 0 when none exists.
func (s *Service) GetConcession(ctx context.Context, p *auth.Principal, marinaID string) (*Concession, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	c, err := store.GetConcession(ctx, s.d.DB.DB, scope)
	if errors.Is(err, store.ErrNotFound) {
		return &Concession{Surfaces: []Surface{}, Status: "active"}, nil
	}
	if err != nil {
		return nil, err
	}
	return concessionOf(c), nil
}

// UpdateConcession writes the card when its version equals ifMatch (0 creates it) (marina.configure).
func (s *Service) UpdateConcession(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, in ConcessionInput) (*Concession, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	var out *Concession
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		c, err := store.GetConcession(ctx, tx, scope)
		isNew := errors.Is(err, store.ErrNotFound)
		if err != nil && !isNew {
			return err
		}
		if isNew {
			if ifMatch != 0 {
				return ErrVersionMismatch
			}
			c = &store.Concession{ID: store.NewID(), Surfaces: store.JSON(`[]`), Status: "active", Version: 1, CreatedAt: now}
		} else if c.Version != ifMatch {
			return ErrVersionMismatch
		}
		oldExpiry := c.ExpiresOn
		v := &auth.ValidationError{}
		set := func(dst *string, src *string, field string, limit int) {
			if src == nil {
				return
			}
			t := strings.TrimSpace(*src)
			if len(t) > limit {
				v.Add(field, "too_long")
			}
			*dst = t
		}
		set(&c.Authority, in.Authority, "authority", 200)
		set(&c.DeedKind, in.DeedKind, "deed_kind", 120)
		set(&c.DeedNumber, in.DeedNumber, "deed_number", 64)
		set(&c.Prescriptions, in.Prescriptions, "prescriptions", 20000)
		if in.GrantedOn != nil {
			c.GrantedOn = *in.GrantedOn
		}
		if in.ExpiresOn != nil {
			c.ExpiresOn = *in.ExpiresOn
		}
		if !c.GrantedOn.IsZero() && !c.ExpiresOn.IsZero() && c.ExpiresOn.Before(c.GrantedOn) {
			v.Add("expires_on", "before_granted_on")
		}
		if in.AnnualFeeCents != nil {
			if *in.AnnualFeeCents < 0 {
				v.Add("annual_fee_cents", "out_of_range")
			}
			c.AnnualFeeCents = *in.AnnualFeeCents
		}
		if in.Surfaces != nil {
			if len(*in.Surfaces) > 50 {
				v.Add("surfaces", "too_many")
			}
			for _, sf := range *in.Surfaces {
				if !slices.Contains(surfaceKinds, sf.Kind) || sf.AreaCm < 0 || len(sf.Note) > 200 {
					v.Add("surfaces", "invalid")
					break
				}
			}
			c.Surfaces = store.MustJSON(*in.Surfaces)
		}
		if in.Status != nil {
			if !slices.Contains(concessionStatus, *in.Status) {
				v.Add("status", "invalid")
			}
			c.Status = *in.Status
		}
		if err := v.Err(); err != nil {
			return err
		}
		c.UpdatedAt = now
		if isNew {
			err = store.InsertConcession(ctx, tx, scope, c)
		} else {
			err = store.UpdateConcession(ctx, tx, scope, c, ifMatch)
		}
		if err != nil {
			return err
		}
		if !c.ExpiresOn.Equal(oldExpiry) || isNew {
			if err := store.SetMarinaConcessionExpiry(ctx, tx, scope, c.ExpiresOn, now); err != nil {
				return err
			}
		}
		if err := s.write(ctx, tx, actorOf(p), scope, "concession.update", "concession", c.ID,
			map[string]any{"expires_on": c.ExpiresOn, "annual_fee_cents": c.AnnualFeeCents, "status": c.Status},
			changed("concession", c.ID, "updated")); err != nil {
			return err
		}
		out = concessionOf(c)
		return nil
	})
	return out, err
}

// concessionID returns the id of the marina's concession, creating an empty card when none exists.
func (s *Service) concessionID(ctx context.Context, tx bun.IDB, scope store.MarinaScope) (string, error) {
	c, err := store.GetConcession(ctx, tx, scope)
	if err == nil {
		return c.ID, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	now := s.now()
	c = &store.Concession{ID: store.NewID(), Surfaces: store.JSON(`[]`), Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.InsertConcession(ctx, tx, scope, c); err != nil {
		return "", err
	}
	return c.ID, nil
}

// ListObligations returns the obligations by due date (rights.manage).
func (s *Service) ListObligations(ctx context.Context, p *auth.Principal, marinaID string) ([]Obligation, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListObligations(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	out := make([]Obligation, len(rows))
	for i := range rows {
		out[i] = obligationOf(&rows[i])
	}
	return out, nil
}

func checkObligation(in ObligationInput) error {
	v := &auth.ValidationError{}
	if !slices.Contains(ObligationKinds, in.Kind) {
		v.Add("kind", "invalid")
	}
	if in.DueOn.IsZero() {
		v.Add("due_on", "required")
	}
	if in.Recurrence != "" && !slices.Contains(Recurrences, in.Recurrence) {
		v.Add("recurrence", "invalid")
	}
	if len(in.Description) > 500 {
		v.Add("description", "too_long")
	}
	if in.AmountCents != nil && *in.AmountCents < 0 {
		v.Add("amount_cents", "out_of_range")
	}
	return v.Err()
}

// CreateObligation adds an obligation (marina.configure); the concession card is created empty when missing.
func (s *Service) CreateObligation(ctx context.Context, p *auth.Principal, marinaID string, in ObligationInput) (*Obligation, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	if err := checkObligation(in); err != nil {
		return nil, err
	}
	if in.Recurrence == "" {
		in.Recurrence = "none"
	}
	var out Obligation
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cid, err := s.concessionID(ctx, tx, scope)
		if err != nil {
			return err
		}
		now := s.now()
		o := &store.ConcessionObligation{
			ID: store.NewID(), ConcessionID: cid, Kind: in.Kind, Description: strings.TrimSpace(in.Description), DueOn: in.DueOn,
			Recurrence: in.Recurrence, AmountCents: in.AmountCents, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertObligation(ctx, tx, scope, o); err != nil {
			return err
		}
		out = obligationOf(o)
		return s.write(ctx, tx, actorOf(p), scope, "concession.obligation_create", "concession_obligation", o.ID,
			map[string]any{"kind": o.Kind, "due_on": o.DueOn}, changed("concession", o.ID, "obligation"))
	})
	return &out, err
}

// UpdateObligation replaces the fields of an obligation when its version equals ifMatch (marina.configure).
func (s *Service) UpdateObligation(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in ObligationInput) (*Obligation, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	if err := checkObligation(in); err != nil {
		return nil, err
	}
	if in.Recurrence == "" {
		in.Recurrence = "none"
	}
	var out Obligation
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		o, err := store.GetObligation(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		o.Kind, o.Description, o.DueOn, o.Recurrence, o.AmountCents = in.Kind, strings.TrimSpace(in.Description), in.DueOn, in.Recurrence, in.AmountCents
		o.UpdatedAt = s.now()
		if err := store.UpdateObligation(ctx, tx, scope, o, ifMatch); err != nil {
			return err
		}
		out = obligationOf(o)
		return s.write(ctx, tx, actorOf(p), scope, "concession.obligation_update", "concession_obligation", o.ID,
			map[string]any{"kind": o.Kind, "due_on": o.DueOn}, changed("concession", o.ID, "obligation"))
	})
	return &out, err
}

// DeleteObligation deletes an obligation when its version equals ifMatch (marina.configure).
func (s *Service) DeleteObligation(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.DeleteObligation(ctx, tx, scope, id, ifMatch); err != nil {
			return err
		}
		return s.write(ctx, tx, actorOf(p), scope, "concession.obligation_delete", "concession_obligation", id, nil,
			changed("concession", id, "obligation"))
	})
}

// MarkObligationDone records that an obligation was fulfilled on doneOn (default today). A yearly obligation
// rolls its due date to the next year and stays open (marina.configure).
func (s *Service) MarkObligationDone(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, doneOn calendar.Date) (*Obligation, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	var out Obligation
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		o, err := store.GetObligation(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if doneOn.IsZero() {
			doneOn = s.today(ctx, tx, scope)
		}
		if o.Recurrence == "yearly" {
			o.DueOn = calendar.New(o.DueOn.Year()+1, o.DueOn.Month(), o.DueOn.Day())
			o.DoneOn = calendar.Date{}
		} else {
			if !o.DoneOn.IsZero() {
				return ErrInvalidTransition
			}
			o.DoneOn = doneOn
		}
		o.UpdatedAt = s.now()
		if err := store.UpdateObligation(ctx, tx, scope, o, ifMatch); err != nil {
			return err
		}
		out = obligationOf(o)
		return s.write(ctx, tx, actorOf(p), scope, "concession.obligation_done", "concession_obligation", o.ID,
			map[string]any{"done_on": doneOn, "due_on": o.DueOn}, changed("concession", o.ID, "obligation_done"))
	})
	return &out, err
}
