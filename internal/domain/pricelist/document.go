package pricelist

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// The price list document (m2-plan §3.3, §4.4). Field names and JSON tags are those of the contract
// (PriceListSave, PriceListDocument, RatePeriod, Band, PriceRule), so internal/http/view converts with a strict
// JSON round trip. Saved rows carry their id, new rows in a save a client key.

// Period is a rate period of a document.
type Period struct {
	ID     string              `json:"id,omitempty"`
	Key    string              `json:"key,omitempty"`
	Code   string              `json:"code"`
	Name   catalog.I18nText    `json:"name"`
	Kind   pricing.PeriodKind  `json:"kind"`
	Color  string              `json:"color,omitempty"`
	Ranges []pricing.DateRange `json:"ranges"`
}

// Band is a length band of a document.
type Band struct {
	ID       string  `json:"id,omitempty"`
	Key      string  `json:"key,omitempty"`
	Code     string  `json:"code"`
	MaxCm    *geo.Cm `json:"max_cm,omitempty"`
	Category string  `json:"category,omitempty"`
}

// Rule is a rule of a document: the engine rule plus the client key. Active is a pointer so that an absent
// "active" means true.
type Rule struct {
	pricing.Rule
	Key    string `json:"key,omitempty"`
	Active *bool  `json:"active,omitempty"`
}

// active reports whether the rule is active (absent = true).
func (r *Rule) active() bool { return r.Active == nil || *r.Active }

// SaveRequest is the editable document of a draft.
type SaveRequest struct {
	Code          string                 `json:"code"`
	Name          catalog.I18nText       `json:"name"`
	ValidFrom     calendar.Date          `json:"valid_from"`
	ValidTo       calendar.Date          `json:"valid_to"`
	ContractTypes []pricing.ContractType `json:"contract_types"`
	Settings      pricing.ListSettings   `json:"settings"`
	Periods       []Period               `json:"periods"`
	Bands         []Band                 `json:"bands"`
	Rules         []Rule                 `json:"rules"`
}

// Document is a stored price list with its validation issues.
type Document struct {
	ID                string                 `json:"id"`
	Code              string                 `json:"code"`
	Name              catalog.I18nText       `json:"name"`
	Status            string                 `json:"status"`
	ValidFrom         calendar.Date          `json:"valid_from"`
	ValidTo           calendar.Date          `json:"valid_to"`
	ContractTypes     []pricing.ContractType `json:"contract_types"`
	Settings          pricing.ListSettings   `json:"settings"`
	Periods           []Period               `json:"periods"`
	Bands             []Band                 `json:"bands"`
	Rules             []Rule                 `json:"rules"`
	SourcePriceListID *string                `json:"source_price_list_id,omitempty"`
	ContentSHA256     *string                `json:"content_sha256,omitempty"`
	ActivatedAt       *time.Time             `json:"activated_at,omitempty"`
	ArchivedAt        *time.Time             `json:"archived_at,omitempty"`
	Version           int                    `json:"version"`
	Issues            []pricing.Issue        `json:"issues"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

// Summary is a row of the price list list.
type Summary struct {
	ID                string                 `json:"id"`
	Code              string                 `json:"code"`
	Name              catalog.I18nText       `json:"name"`
	Status            string                 `json:"status"`
	ValidFrom         calendar.Date          `json:"valid_from"`
	ValidTo           calendar.Date          `json:"valid_to"`
	ContractTypes     []pricing.ContractType `json:"contract_types"`
	RuleCount         int                    `json:"rule_count"`
	Version           int                    `json:"version"`
	SourcePriceListID *string                `json:"source_price_list_id,omitempty"`
}

// SaveResult is the answer to a save.
type SaveResult struct {
	Document *Document         `json:"document"`
	IDMap    map[string]string `json:"id_map"`
	Issues   []pricing.Issue   `json:"issues"`
}

func unmarshal[T any](j store.JSON) T {
	var v T
	if len(j) > 0 {
		_ = json.Unmarshal(j, &v)
	}
	return v
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

func contractTypesOf(s store.Strings) []pricing.ContractType {
	out := make([]pricing.ContractType, 0, len(s))
	for _, c := range s {
		out = append(out, pricing.ContractType(c))
	}
	return out
}

func stringsOf(cts []pricing.ContractType) store.Strings {
	out := make(store.Strings, 0, len(cts))
	for _, c := range cts {
		out = append(out, string(c))
	}
	return out
}

// settingsOf decodes stored settings; an empty document takes the defaults.
func settingsOf(j store.JSON) pricing.ListSettings {
	if len(j) == 0 || string(j) == "{}" {
		return pricing.DefaultSettings()
	}
	return unmarshal[pricing.ListSettings](j)
}

// ruleOf rebuilds the engine rule from a row: the body holds the rule, the columns win for indexed fields.
func ruleOf(r *store.PriceRule) pricing.Rule {
	out := unmarshal[pricing.Rule](r.Body)
	out.ID, out.Seq, out.Stage, out.Name, out.ItemID = r.ID, r.Seq, pricing.Stage(r.Stage), r.Name, r.ItemID
	out.Priority, out.Stackable, out.Active = r.Priority, r.Stackable, r.Active
	return out
}

// ruleBody is the stored body of a rule: the engine rule without id and seq.
func ruleBody(r pricing.Rule) store.JSON {
	r.ID, r.Seq = "", 0
	return store.MustJSON(r)
}

// sameBody compares a stored body with a new rule (canonical re-encoding: PostgreSQL jsonb reorders keys).
func sameBody(stored store.JSON, r pricing.Rule) bool {
	old := unmarshal[pricing.Rule](stored)
	old.ID, old.Seq = "", 0
	a, _ := json.Marshal(old)
	return string(a) == string(ruleBody(r))
}

// documentOf builds the document of a stored list.
func documentOf(l *store.PriceList, c *store.PriceListChildren, issues []pricing.Issue) *Document {
	d := &Document{
		ID: l.ID, Code: l.Code, Name: unmarshal[catalog.I18nText](l.Name), Status: l.Status, ValidFrom: l.ValidFrom,
		ValidTo: l.ValidTo, ContractTypes: contractTypesOf(l.ContractTypes), Settings: settingsOf(l.Settings),
		Periods: make([]Period, 0, len(c.Periods)), Bands: make([]Band, 0, len(c.Bands)), Rules: make([]Rule, 0, len(c.Rules)),
		SourcePriceListID: strPtr(l.SourcePriceListID), ContentSHA256: strPtr(l.ContentSHA256),
		ActivatedAt: timePtr(l.ActivatedAt), ArchivedAt: timePtr(l.ArchivedAt), Version: l.Version, Issues: issues,
		UpdatedAt: l.UpdatedAt.UTC(),
	}
	if d.Issues == nil {
		d.Issues = []pricing.Issue{}
	}
	for _, p := range c.Periods {
		d.Periods = append(d.Periods, Period{
			ID: p.ID, Code: p.Code, Name: unmarshal[catalog.I18nText](p.Name), Kind: pricing.PeriodKind(p.Kind), Color: p.Color,
			Ranges: unmarshal[[]pricing.DateRange](p.Ranges),
		})
	}
	for _, b := range c.Bands {
		d.Bands = append(d.Bands, Band{ID: b.ID, Code: b.Code, MaxCm: cmPtr(b.MaxCm), Category: b.CategoryCode})
	}
	for i := range c.Rules {
		r := ruleOf(&c.Rules[i])
		a := r.Active
		d.Rules = append(d.Rules, Rule{Rule: r, Active: &a})
	}
	return d
}

func cmPtr(v int64) *geo.Cm {
	if v == 0 {
		return nil
	}
	c := geo.Cm(v)
	return &c
}

// engineList builds the list part of an engine catalog. Row ids are the document ids, or the keys of unsaved
// rows (so that issues point at them).
func engineCatalog(id, code string, from, to calendar.Date, cts []pricing.ContractType, set pricing.ListSettings, periods []Period, bands []Band, rules []Rule) pricing.Catalog {
	cat := pricing.Catalog{
		PriceList: pricing.PriceList{
			ID: id, Code: code, ValidFrom: from, ValidTo: to, ContractTypes: slices.Clone(cts), Settings: set,
			Periods: make([]pricing.RatePeriod, 0, len(periods)), Bands: make([]pricing.Band, 0, len(bands)),
		},
		Rules: make([]pricing.Rule, 0, len(rules)),
	}
	if cat.PriceList.ContractTypes == nil {
		cat.PriceList.ContractTypes = []pricing.ContractType{}
	}
	for _, p := range periods {
		cat.PriceList.Periods = append(cat.PriceList.Periods, pricing.RatePeriod{Code: p.Code, Kind: p.Kind, Ranges: p.Ranges})
	}
	for _, b := range bands {
		cat.PriceList.Bands = append(cat.PriceList.Bands, pricing.Band{Code: b.Code, MaxCm: b.MaxCm, Category: b.Category})
	}
	for _, r := range rules {
		e := r.Rule
		e.Active = r.active()
		if e.ID == "" {
			e.ID = r.Key
		}
		cat.Rules = append(cat.Rules, e)
	}
	return cat
}

// referencedItemIDs lists the item ids a catalog references: rule items, includes, targets, primary items.
func referencedItemIDs(cat *pricing.Catalog) []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, r := range cat.Rules {
		add(r.ItemID)
		for _, in := range r.Includes {
			add(in.ItemID)
		}
		for _, t := range r.TargetItemIDs {
			add(t)
		}
	}
	keys := make([]string, 0, len(cat.PriceList.Settings.PrimaryItems))
	for ct := range cat.PriceList.Settings.PrimaryItems {
		keys = append(keys, string(ct))
	}
	slices.Sort(keys)
	for _, k := range keys {
		add(cat.PriceList.Settings.PrimaryItems[pricing.ContractType(k)])
	}
	return out
}

// attachItems loads the live items a catalog references plus extra ids (requested services), by code. Drafts
// price with the live items; published lists use their snapshot (attachSnapshot).
func attachItems(ctx context.Context, tx bun.IDB, scope store.MarinaScope, cat *pricing.Catalog, extra ...string) error {
	items, err := liveItems(ctx, tx, scope, append(referencedItemIDs(cat), extra...))
	if err != nil {
		return err
	}
	cat.Items = items
	return nil
}

func liveItems(ctx context.Context, tx bun.IDB, scope store.MarinaScope, ids []string) ([]pricing.Item, error) {
	out := []pricing.Item{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := store.ItemsByIDs(ctx, tx, scope, ids)
	if err != nil {
		return nil, err
	}
	classes, err := catalog.RevenueClassCodes(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, pricing.Item{
			ID: r.ID, Code: r.Code, Type: pricing.ItemType(r.ItemType), Unit: r.Unit, VATRateBP: r.VATRateBP, VATNature: r.VATNature,
			PriceIncludesVAT: r.PriceIncludesVAT, RevenueClass: classes[r.RevenueClassID], ExternalCode: r.ExternalCode, Active: r.Active,
		})
	}
	return out, nil
}

// attachSnapshot loads the item snapshot written at activation (INV-26), so a published list prices and hashes
// the same forever while the live items stay editable. Extra ids outside the snapshot (a requested service the
// list does not reference) are loaded live: the list has no rule for them, so they cannot change its prices.
func attachSnapshot(ctx context.Context, tx bun.IDB, scope store.MarinaScope, listID string, cat *pricing.Catalog, extra ...string) error {
	rows, err := store.PriceListItems(ctx, tx, scope, listID)
	if err != nil {
		return err
	}
	cat.Items = make([]pricing.Item, 0, len(rows)+len(extra))
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		seen[r.ItemID] = true
		cat.Items = append(cat.Items, snapshotItem(r))
	}
	var missing []string
	for _, id := range extra {
		if id != "" && !seen[id] {
			seen[id] = true
			missing = append(missing, id)
		}
	}
	live, err := liveItems(ctx, tx, scope, missing)
	if err != nil {
		return err
	}
	cat.Items = append(cat.Items, live...)
	return nil
}

func snapshotItem(r store.PriceListItem) pricing.Item {
	return pricing.Item{
		ID: r.ItemID, Code: r.Code, Type: pricing.ItemType(r.ItemType), Unit: r.Unit, VATRateBP: r.VATRateBP, VATNature: r.VATNature,
		PriceIncludesVAT: r.PriceIncludesVAT, RevenueClass: r.RevenueClass, ExternalCode: r.ExternalCode, Active: r.Active,
	}
}

// snapshotRows maps the items of a catalog to the snapshot rows written at activation.
func snapshotRows(cat pricing.Catalog, now time.Time) []store.PriceListItem {
	out := make([]store.PriceListItem, 0, len(cat.Items))
	for _, it := range cat.Items {
		out = append(out, store.PriceListItem{
			ID: store.NewID(), ItemID: it.ID, Code: it.Code, ItemType: string(it.Type), Unit: it.Unit, VATRateBP: it.VATRateBP,
			VATNature: it.VATNature, PriceIncludesVAT: it.PriceIncludesVAT, RevenueClass: it.RevenueClass,
			ExternalCode: it.ExternalCode, Active: it.Active, CreatedAt: now,
		})
	}
	return out
}

// LoadCatalog maps a stored list of the marina to an engine catalog with the items it references plus extra
// item ids (m2-plan §5.1): the live items for a draft, the activation snapshot for a published list (INV-26).
func LoadCatalog(ctx context.Context, tx bun.IDB, scope store.MarinaScope, listID string, extraItems ...string) (pricing.Catalog, error) {
	l, err := store.GetPriceList(ctx, tx, scope, listID)
	if err != nil {
		return pricing.Catalog{}, err
	}
	c, err := store.LoadPriceListChildren(ctx, tx, scope, listID)
	if err != nil {
		return pricing.Catalog{}, err
	}
	return catalogOf(ctx, tx, scope, l, c, extraItems...)
}

func catalogOf(ctx context.Context, tx bun.IDB, scope store.MarinaScope, l *store.PriceList, c *store.PriceListChildren, extraItems ...string) (pricing.Catalog, error) {
	d := documentOf(l, c, nil)
	cat := engineCatalog(l.ID, l.Code, l.ValidFrom, l.ValidTo, d.ContractTypes, d.Settings, d.Periods, d.Bands, d.Rules)
	attach := func() error { return attachItems(ctx, tx, scope, &cat, extraItems...) }
	if l.Status != store.PriceListDraft {
		attach = func() error { return attachSnapshot(ctx, tx, scope, l.ID, &cat, extraItems...) }
	}
	if err := attach(); err != nil {
		return pricing.Catalog{}, err
	}
	return cat, nil
}

// catalogHash is pricing.CatalogHash, with a plain sha256 of the JSON as a fallback while the engine returns
// nothing (the column must not be empty for a published list).
func catalogHash(cat pricing.Catalog) string {
	if h := pricing.CatalogHash(cat); h != "" {
		return h
	}
	b, _ := json.Marshal(cat)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// validate runs the engine validation plus the service checks that need the marina (unknown berth categories).
func validate(ctx context.Context, tx bun.IDB, scope store.MarinaScope, cat pricing.Catalog) ([]pricing.Issue, error) {
	issues := pricing.Validate(cat)
	codes, err := catalog.CategoryCodes(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	issues = append(issues, pricing.ValidateCategories(cat, codes)...)
	if issues == nil {
		issues = []pricing.Issue{}
	}
	return issues, nil
}

// hasErrors reports whether issues contain a blocking error.
func hasErrors(issues []pricing.Issue) bool {
	for _, is := range issues {
		if is.Severity == pricing.SeverityError {
			return true
		}
	}
	return false
}

func errorsOnly(issues []pricing.Issue) []pricing.Issue {
	var out []pricing.Issue
	for _, is := range issues {
		if is.Severity == pricing.SeverityError {
			out = append(out, is)
		}
	}
	return out
}
