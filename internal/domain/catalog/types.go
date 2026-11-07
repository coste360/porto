package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/store"
)

// The output types carry the JSON names of the contract (PricingSettings, RevenueClass, Item, BerthCategory),
// so internal/http/view converts them with a strict JSON round trip.

// I18nText is a text in Italian (required) and English.
type I18nText struct {
	It string  `json:"it"`
	En *string `json:"en,omitempty"`
}

// I18nTextOptional is a translated text where both languages may be empty.
type I18nTextOptional struct {
	It string `json:"it,omitempty"`
	En string `json:"en,omitempty"`
}

// Settings are the pricing defaults of a marina.
type Settings struct {
	DefaultVATRateBP        int64 `json:"default_vat_rate_bp"`
	DefaultPriceIncludesVAT bool  `json:"default_price_includes_vat"`
	Version                 int   `json:"version"`
}

// RevenueClass is a revenue category, or a subcategory when ParentID is set.
type RevenueClass struct {
	ID        string   `json:"id"`
	Code      string   `json:"code"`
	Name      I18nText `json:"name"`
	ParentID  *string  `json:"parent_id,omitempty"`
	SortOrder int      `json:"sort_order"`
	Archived  bool     `json:"archived"`
	ItemCount int      `json:"item_count"`
	Version   int      `json:"version"`
}

// ItemUsage counts the price lists that reference an item.
type ItemUsage struct {
	PriceLists int `json:"price_lists"`
}

// Item is an item or service.
type Item struct {
	ID               string           `json:"id"`
	Code             string           `json:"code"`
	Name             I18nText         `json:"name"`
	Description      I18nTextOptional `json:"description"`
	ItemType         string           `json:"item_type"`
	Unit             string           `json:"unit"`
	VATRateBP        int64            `json:"vat_rate_bp"`
	VATNature        *string          `json:"vat_nature,omitempty"`
	PriceIncludesVAT bool             `json:"price_includes_vat"`
	RevenueClassID   string           `json:"revenue_class_id"`
	ExternalCode     string           `json:"external_code"`
	Active           bool             `json:"active"`
	OnlineVisible    bool             `json:"online_visible"`
	SortOrder        int              `json:"sort_order"`
	Usage            *ItemUsage       `json:"usage,omitempty"`
	Version          int              `json:"version"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

// CategoryUsage counts the elements and rules that use a berth category.
type CategoryUsage struct {
	Berths        int `json:"berths"`
	DynamicAreas  int `json:"dynamic_areas"`
	LandResources int `json:"land_resources"`
	Rules         int `json:"rules"`
}

// BerthCategory is a registered price_class code.
type BerthCategory struct {
	ID        string        `json:"id"`
	Code      string        `json:"code"`
	Name      I18nText      `json:"name"`
	AppliesTo []string      `json:"applies_to"`
	SortOrder int           `json:"sort_order"`
	Archived  bool          `json:"archived"`
	Usage     CategoryUsage `json:"usage"`
	Version   int           `json:"version"`
}

// CodeCount is a price_class code with the number of elements that use it.
type CodeCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// CategoryList is the list of berth categories with the codes in use that have no category.
type CategoryList struct {
	Items        []BerthCategory `json:"items"`
	UnknownInUse []CodeCount     `json:"unknown_in_use"`
}

// Enumerations of the catalogue.
var (
	itemUnits      = []string{"night", "month", "season", "year", "m", "m2", "kwh", "m3", "hour", "piece", "lump"}
	categoryTarget = []string{"berth", "dynamic_area", "land_resource"}
	// VATNatures are the FatturaPA natures accepted with a 0 rate (m2-plan §12 D9).
	VATNatures = []string{
		"N1", "N2.1", "N2.2", "N3.1", "N3.2", "N3.3", "N3.4", "N3.5", "N3.6", "N4", "N5",
		"N6.1", "N6.2", "N6.3", "N6.4", "N6.5", "N6.6", "N6.7", "N6.8", "N6.9", "N7",
	}
	itemCodePattern  = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{0,31}$`)
	classCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,31}$`)
	catCodePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,31}$`)
)

func itemTypes() []string {
	out := []string{}
	for _, t := range pricing.ItemTypes() {
		out = append(out, string(t))
	}
	return out
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

// checker collects field errors.
type checker struct{ v *auth.ValidationError }

func newChecker() checker { return checker{&auth.ValidationError{}} }

func (c checker) add(field, code string) { c.v.Add(field, code) }

func (c checker) enum(field, val string, vals []string) {
	if !slices.Contains(vals, val) {
		c.add(field, "invalid")
	}
}

func (c checker) rangeI(field string, val, lo, hi int64) {
	if val < lo || val > hi {
		c.add(field, "out_of_range")
	}
}

func (c checker) pattern(field, val string, re *regexp.Regexp) {
	if !re.MatchString(val) {
		c.add(field, "invalid")
	}
}

func (c checker) text(field, val string, maxLen int, required bool) {
	if required && strings.TrimSpace(val) == "" {
		c.add(field, "required")
		return
	}
	if utf8.RuneCountInString(val) > maxLen || !utf8.ValidString(val) || strings.ContainsFunc(val, func(r rune) bool { return r < 0x20 && r != '\n' }) {
		c.add(field, "invalid")
	}
}

func (c checker) i18n(field string, t I18nText) {
	c.text(field+".it", t.It, 200, true)
	if t.En != nil {
		c.text(field+".en", *t.En, 200, false)
	}
}

func (c checker) err() error { return c.v.Err() }

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

// write records an audit row and the config.pricing_changed event in tx.
func (s *Service) write(ctx context.Context, tx bun.IDB, p *auth.Principal, scope store.MarinaScope, action, entityType, id string, diff any, data event.PricingChangedData) error {
	if err := s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: action, EntityType: entityType, EntityID: id, Diff: diff,
	}); err != nil {
		return err
	}
	if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.ConfigPricingChanged, data); err != nil {
		return fmt.Errorf("pricing event: %w", err)
	}
	return nil
}
