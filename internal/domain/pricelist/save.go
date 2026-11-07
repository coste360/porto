package pricelist

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/store"
)

var (
	codePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,31}$`)
	keyPattern   = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,64}$`)
	periodColors = []string{"chart-1", "chart-2", "chart-3", "chart-4", "chart-5"}
)

// Document limits (m2-plan §2.4 step 14, D30).
const (
	maxRules   = 2000
	maxPeriods = 50
	maxBands   = 60
	maxRanges  = 20
)

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

// today returns the current date in the marina time zone.
func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) (calendar.Date, error) {
	tz, err := store.MarinaTimezone(ctx, db, scope)
	if err != nil {
		return calendar.Date{}, err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return calendar.In(s.d.Clock.Now(), loc), nil
}

func auditEntry(p *auth.Principal, scope store.MarinaScope, action, id string, diff any) audit.Entry {
	return audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: action, EntityType: "price_list", EntityID: id, Diff: diff,
	}
}

// write records an audit row and the config.pricing_changed event in tx.
func (s *Service) write(ctx context.Context, tx bun.IDB, p *auth.Principal, scope store.MarinaScope, action, id string, diff any, data event.PricingChangedData) error {
	if err := s.d.Audit.Write(ctx, tx, auditEntry(p, scope, action, id, diff)); err != nil {
		return err
	}
	if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.ConfigPricingChanged, data); err != nil {
		return fmt.Errorf("pricing event: %w", err)
	}
	return nil
}

// checker collects field errors.
type checker struct{ v *auth.ValidationError }

func (c checker) add(field, code string) { c.v.Add(field, code) }

func (c checker) text(field, val string, maxLen int, required bool) {
	if required && strings.TrimSpace(val) == "" {
		c.add(field, "required")
		return
	}
	if utf8.RuneCountInString(val) > maxLen || !utf8.ValidString(val) || strings.ContainsFunc(val, func(r rune) bool { return r < 0x20 }) {
		c.add(field, "invalid")
	}
}

func (c checker) i18n(field string, t catalog.I18nText) {
	c.text(field+".it", t.It, 200, true)
	if t.En != nil {
		c.text(field+".en", *t.En, 200, false)
	}
}

func (c checker) code(field, val string) {
	if !codePattern.MatchString(val) {
		c.add(field, "invalid")
	}
}

func (c checker) contractTypes(field string, cts []pricing.ContractType, required bool) {
	if required && len(cts) == 0 {
		c.add(field, "required")
	}
	seen := map[pricing.ContractType]bool{}
	for i, ct := range cts {
		if seen[ct] || !slices.Contains(pricing.ContractTypes(), ct) {
			c.add(fmt.Sprintf("%s[%d]", field, i), "invalid")
		}
		seen[ct] = true
	}
}

func (c checker) validity(from, to calendar.Date) {
	if from.IsZero() {
		c.add("valid_from", "required")
	}
	if to.IsZero() {
		c.add("valid_to", "required")
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		c.add("valid_to", "before_valid_from")
	}
}

// rowRef checks the id or key of a document row: a saved row's id must belong to the list (known), a new row
// needs a key unique in the request.
func (c checker) rowRef(field, id, key string, known map[string]bool, keys map[string]bool) {
	switch {
	case id != "":
		if !known[id] {
			c.add(field+".id", "unknown")
		}
	case key == "" || !keyPattern.MatchString(key):
		c.add(field+".key", "required")
	case keys[key]:
		c.add(field+".key", "duplicate")
	default:
		keys[key] = true
	}
}

// checkSave validates the shape of a save request. Semantic problems (overlaps, unknown codes, missing rules)
// are pricing.Validate issues, not field errors: a draft may be saved incomplete.
func checkSave(req *SaveRequest, existing *store.PriceListChildren) error {
	c := checker{&auth.ValidationError{}}
	req.Code = strings.TrimSpace(req.Code)
	c.code("code", req.Code)
	c.i18n("name", req.Name)
	c.validity(req.ValidFrom, req.ValidTo)
	c.contractTypes("contract_types", req.ContractTypes, false)
	if len(req.Periods) > maxPeriods {
		c.add("periods", "too_many")
	}
	if len(req.Bands) > maxBands {
		c.add("bands", "too_many")
	}
	if len(req.Rules) > maxRules {
		c.add("rules", "too_many")
	}
	if err := c.v.Err(); err != nil {
		return err
	}
	known := map[string]bool{}
	for _, p := range existing.Periods {
		known[p.ID] = true
	}
	for _, b := range existing.Bands {
		known[b.ID] = true
	}
	for _, r := range existing.Rules {
		known[r.ID] = true
	}
	keys := map[string]bool{}
	ids := map[string]bool{}
	dupID := func(field, id string) {
		if id == "" {
			return
		}
		if ids[id] {
			c.add(field+".id", "duplicate")
		}
		ids[id] = true
	}
	for i := range req.Periods {
		p := &req.Periods[i]
		f := fmt.Sprintf("periods[%d]", i)
		c.rowRef(f, p.ID, p.Key, known, keys)
		dupID(f, p.ID)
		p.Code = strings.TrimSpace(p.Code)
		c.code(f+".code", p.Code)
		c.i18n(f+".name", p.Name)
		if p.Kind != pricing.PeriodSeason && p.Kind != pricing.PeriodEvent {
			c.add(f+".kind", "invalid")
		}
		if p.Color == "" {
			p.Color = periodColors[i%len(periodColors)]
		} else if !slices.Contains(periodColors, p.Color) {
			c.add(f+".color", "invalid")
		}
		if len(p.Ranges) == 0 || len(p.Ranges) > maxRanges {
			c.add(f+".ranges", "invalid")
		}
		for j, r := range p.Ranges {
			if r.From.IsZero() || r.To.IsZero() {
				c.add(fmt.Sprintf("%s.ranges[%d]", f, j), "required")
			}
		}
	}
	for i := range req.Bands {
		b := &req.Bands[i]
		f := fmt.Sprintf("bands[%d]", i)
		c.rowRef(f, b.ID, b.Key, known, keys)
		dupID(f, b.ID)
		b.Code = strings.TrimSpace(b.Code)
		c.code(f+".code", b.Code)
		if b.MaxCm != nil && (*b.MaxCm < 1 || *b.MaxCm > 20000) {
			c.add(f+".max_cm", "out_of_range")
		}
		if b.Category != "" {
			c.code(f+".category", b.Category)
		}
	}
	for i := range req.Rules {
		r := &req.Rules[i]
		f := fmt.Sprintf("rules[%d]", i)
		c.rowRef(f, r.ID, r.Key, known, keys)
		dupID(f, r.ID)
		r.Name = strings.TrimSpace(r.Name)
		c.text(f+".name", r.Name, 120, true)
		switch r.Stage {
		case pricing.StageBase:
			if !store.ValidID(r.ItemID) {
				c.add(f+".item_id", "required")
			}
		case pricing.StageModifier, pricing.StageDiscount:
		default:
			c.add(f+".stage", "invalid")
		}
		if r.Priority < -1000 || r.Priority > 1000 {
			c.add(f+".priority", "out_of_range")
		}
		r.PromoCode = strings.ToUpper(strings.TrimSpace(r.PromoCode))
		if len(r.PromoCode) > 32 {
			c.add(f+".promo_code", "invalid")
		}
	}
	return c.v.Err()
}

// ---- reads ----

// ListFilter narrows List.
type ListFilter struct {
	Status       string
	ContractType string
	On           calendar.Date
}

// List returns the price lists of a marina, newest validity first (pricing.read).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string, f ListFilter) ([]Summary, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingRead)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListPriceLists(ctx, s.d.DB.DB, scope, store.PriceListFilter{Status: f.Status, ContractType: f.ContractType, On: f.On})
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(rows))
	for _, r := range rows {
		out = append(out, Summary{
			ID: r.ID, Code: r.Code, Name: unmarshal[catalog.I18nText](r.Name), Status: r.Status, ValidFrom: r.ValidFrom,
			ValidTo: r.ValidTo, ContractTypes: contractTypesOf(r.ContractTypes), RuleCount: r.RuleCount, Version: r.Version,
			SourcePriceListID: strPtr(r.SourcePriceListID),
		})
	}
	return out, nil
}

// loadDocument reads a list with its children and validation issues.
func loadDocument(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string) (*Document, error) {
	l, err := store.GetPriceList(ctx, tx, scope, id)
	if err != nil {
		return nil, err
	}
	c, err := store.LoadPriceListChildren(ctx, tx, scope, id)
	if err != nil {
		return nil, err
	}
	cat, err := catalogOf(ctx, tx, scope, l, c)
	if err != nil {
		return nil, err
	}
	issues, err := validate(ctx, tx, scope, cat)
	if err != nil {
		return nil, err
	}
	return documentOf(l, c, issues), nil
}

// Get returns a price list document with its issues (pricing.read).
func (s *Service) Get(ctx context.Context, p *auth.Principal, marinaID, id string) (*Document, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingRead)
	if err != nil {
		return nil, err
	}
	var out *Document
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		out, err = loadDocument(ctx, tx, scope, id)
		return err
	})
	return out, err
}

// ---- create ----

// CreateInput are the fields of a new draft.
type CreateInput struct {
	Code          string
	Name          catalog.I18nText
	ValidFrom     calendar.Date
	ValidTo       calendar.Date
	ContractTypes []pricing.ContractType
}

// Create creates an empty draft with the default settings (pricing.configure).
func (s *Service) Create(ctx context.Context, p *auth.Principal, marinaID string, in CreateInput) (*Document, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return nil, err
	}
	in.Code = strings.TrimSpace(in.Code)
	c := checker{&auth.ValidationError{}}
	c.code("code", in.Code)
	c.i18n("name", in.Name)
	c.validity(in.ValidFrom, in.ValidTo)
	c.contractTypes("contract_types", in.ContractTypes, true)
	if err := c.v.Err(); err != nil {
		return nil, err
	}
	var out *Document
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		l := &store.PriceList{
			ID: store.NewID(), Code: in.Code, Name: store.MustJSON(in.Name), Status: store.PriceListDraft, ValidFrom: in.ValidFrom,
			ValidTo: in.ValidTo, ContractTypes: stringsOf(in.ContractTypes), Settings: store.MustJSON(pricing.DefaultSettings()),
			NextRuleSeq: 1, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertPriceList(ctx, tx, scope, l); err != nil {
			return err
		}
		if out, err = loadDocument(ctx, tx, scope, l.ID); err != nil {
			return err
		}
		return s.write(ctx, tx, p, scope, "price_list.create", l.ID, map[string]any{"contract_types": l.ContractTypes},
			event.PricingChangedData{PriceListID: l.ID, Version: l.Version, Change: event.ChangeCreated})
	})
	return out, err
}

// ---- save ----

// buildChildren turns a save request into rows: kept rows keep their ids, new rows get one (idMap key → id).
// Rule recency: new rules and rules whose body changed get next_rule_seq++, in document order.
func buildChildren(req *SaveRequest, existing *store.PriceListChildren, nextSeq *int64, now time.Time) (*store.PriceListChildren, map[string]string) {
	idMap := map[string]string{}
	idOf := func(id, key string) string {
		if id != "" {
			return id
		}
		nid := store.NewID()
		idMap[key] = nid
		return nid
	}
	created := map[string]time.Time{}
	for _, p := range existing.Periods {
		created[p.ID] = p.CreatedAt
	}
	for _, b := range existing.Bands {
		created[b.ID] = b.CreatedAt
	}
	oldRules := map[string]*store.PriceRule{}
	for i := range existing.Rules {
		oldRules[existing.Rules[i].ID] = &existing.Rules[i]
		created[existing.Rules[i].ID] = existing.Rules[i].CreatedAt
	}
	createdAt := func(id string) time.Time {
		if t, ok := created[id]; ok {
			return t
		}
		return now
	}
	out := &store.PriceListChildren{
		Periods: make([]store.RatePeriod, 0, len(req.Periods)), Bands: make([]store.PriceListBand, 0, len(req.Bands)),
		Rules: make([]store.PriceRule, 0, len(req.Rules)),
	}
	for i, p := range req.Periods {
		id := idOf(p.ID, p.Key)
		ranges := p.Ranges
		if ranges == nil {
			ranges = []pricing.DateRange{}
		}
		out.Periods = append(out.Periods, store.RatePeriod{
			ID: id, Code: p.Code, Name: store.MustJSON(p.Name), Kind: string(p.Kind), Color: p.Color, Ranges: store.MustJSON(ranges),
			SortOrder: i, CreatedAt: createdAt(id), UpdatedAt: now,
		})
	}
	for i, b := range req.Bands {
		id := idOf(b.ID, b.Key)
		var maxCm int64
		if b.MaxCm != nil {
			maxCm = int64(*b.MaxCm)
		}
		out.Bands = append(out.Bands, store.PriceListBand{
			ID: id, Code: b.Code, MaxCm: maxCm, CategoryCode: b.Category, SortOrder: i, CreatedAt: createdAt(id), UpdatedAt: now,
		})
	}
	for _, r := range req.Rules {
		id := idOf(r.ID, r.Key)
		e := r.Rule
		e.ID, e.Active, e.PromoCode = id, r.active(), r.PromoCode
		old := oldRules[r.ID]
		seq := int64(0)
		if old != nil && old.Stage == string(e.Stage) && sameBody(old.Body, e) {
			seq = old.Seq
		} else {
			seq = *nextSeq
			*nextSeq++
		}
		e.Seq = seq
		itemID := ""
		if e.Stage == pricing.StageBase {
			itemID = e.ItemID
		}
		out.Rules = append(out.Rules, store.PriceRule{
			ID: id, Seq: seq, Stage: string(e.Stage), Name: e.Name, ItemID: itemID, Priority: e.Priority, Stackable: e.Stackable,
			Active: e.Active, Body: ruleBody(e), CreatedAt: createdAt(id), UpdatedAt: now,
		})
	}
	return out, idMap
}

// Save replaces the document of a draft when its version equals ifMatch (pricing.configure). The draft is saved
// even with validation issues; they are returned.
func (s *Service) Save(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, req SaveRequest) (*SaveResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return nil, err
	}
	var out SaveResult
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		l, err := store.GetPriceList(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if l.Status != store.PriceListDraft {
			return ErrImmutable
		}
		if l.Version != ifMatch {
			return ErrVersionMismatch
		}
		existing, err := store.LoadPriceListChildren(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if err := checkSave(&req, existing); err != nil {
			return err
		}
		if err := s.checkItemRefs(ctx, tx, scope, &req); err != nil {
			return err
		}
		now := s.now()
		next := l.NextRuleSeq
		rows, idMap := buildChildren(&req, existing, &next, now)
		l.Code, l.Name, l.ValidFrom, l.ValidTo = req.Code, store.MustJSON(req.Name), req.ValidFrom, req.ValidTo
		l.ContractTypes, l.Settings, l.NextRuleSeq, l.UpdatedAt = stringsOf(req.ContractTypes), store.MustJSON(req.Settings), next, now
		// The header first: it takes the row lock, so concurrent saves with the same If-Match have one winner.
		if err := store.UpdatePriceListHeader(ctx, tx, scope, l, ifMatch); err != nil {
			return mapListErr(err)
		}
		if err := store.ReplacePriceListChildren(ctx, tx, scope, id, rows, existing); err != nil {
			return mapListErr(err)
		}
		doc, err := loadDocument(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		out = SaveResult{Document: doc, IDMap: idMap, Issues: doc.Issues}
		return s.write(ctx, tx, p, scope, "price_list.save", id, map[string]any{
			"periods": len(rows.Periods), "bands": len(rows.Bands), "rules": len(rows.Rules), "version": l.Version,
		}, event.PricingChangedData{PriceListID: id, Version: l.Version, Change: event.ChangeSaved})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// checkItemRefs refuses item ids of another marina (or unknown ones) in base rules: they are a field error,
// because the database would refuse them anyway (composite foreign key).
func (s *Service) checkItemRefs(ctx context.Context, tx bun.IDB, scope store.MarinaScope, req *SaveRequest) error {
	var ids []string
	for _, r := range req.Rules {
		if r.Stage == pricing.StageBase {
			ids = append(ids, r.ItemID)
		}
	}
	rows, err := store.ItemsByIDs(ctx, tx, scope, ids)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, r := range rows {
		known[r.ID] = true
	}
	v := &auth.ValidationError{}
	for i, r := range req.Rules {
		if r.Stage == pricing.StageBase && !known[r.ItemID] {
			v.Add(fmt.Sprintf("rules[%d].item_id", i), "unknown")
		}
	}
	return v.Err()
}

func mapListErr(err error) error {
	switch {
	case errors.Is(err, store.ErrVersionMismatch):
		return ErrVersionMismatch
	case errors.Is(err, store.ErrImmutable):
		return fmt.Errorf("%w: %w", ErrImmutable, err)
	}
	return err
}

// Validate validates a document without writing (pricing.read): req, or the saved document when req is nil.
func (s *Service) Validate(ctx context.Context, p *auth.Principal, marinaID, id string, req *SaveRequest) ([]pricing.Issue, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingRead)
	if err != nil {
		return nil, err
	}
	var out []pricing.Issue
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		l, err := store.GetPriceList(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		existing, err := store.LoadPriceListChildren(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		var cat pricing.Catalog
		if req == nil {
			if cat, err = catalogOf(ctx, tx, scope, l, existing); err != nil {
				return err
			}
		} else {
			if err := checkSave(req, existing); err != nil {
				return err
			}
			cat = engineCatalog(l.ID, req.Code, req.ValidFrom, req.ValidTo, req.ContractTypes, req.Settings, req.Periods, req.Bands, req.Rules)
			if err := attachItems(ctx, tx, scope, &cat); err != nil {
				return err
			}
		}
		out, err = validate(ctx, tx, scope, cat)
		return err
	})
	return out, err
}

// Delete deletes a draft when its version equals ifMatch (pricing.configure); published lists are immutable.
func (s *Service) Delete(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		l, err := store.GetPriceList(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if l.Status != store.PriceListDraft {
			return ErrImmutable
		}
		if l.Version != ifMatch {
			return ErrVersionMismatch
		}
		if err := store.ClearPriceListSource(ctx, tx, scope, id); err != nil {
			return err
		}
		if err := store.DeletePriceList(ctx, tx, scope, id, ifMatch); err != nil {
			return mapListErr(err)
		}
		return s.write(ctx, tx, p, scope, "price_list.delete", id, nil,
			event.PricingChangedData{PriceListID: id, Change: event.ChangeDeleted})
	})
}
