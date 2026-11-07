package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M2 pricing tables (internal/migrations/*/00004_pricing.sql). Conventions as in
// harbor_models.go. Money is integer cents, percentages basis points, lengths centimetres. Rows of a price list
// that is not a draft are immutable (triggers, INV-22): writes on them fail with an error that MapImmutable turns
// into ErrImmutable.

// PricingSettings is a row of pricing_settings (one per marina, created lazily with defaults).
type PricingSettings struct {
	bun.BaseModel `bun:"table:pricing_settings"`

	ID                      string    `bun:"id,pk"`
	MarinaID                string    `bun:"marina_id"`
	DefaultVATRateBP        int64     `bun:"default_vat_rate_bp"`
	DefaultPriceIncludesVAT bool      `bun:"default_price_includes_vat"`
	ActivationSeq           int64     `bun:"activation_seq"` // bumped by every activation (INV-24 serialisation)
	Version                 int       `bun:"version"`
	CreatedAt               time.Time `bun:"created_at"`
	UpdatedAt               time.Time `bun:"updated_at"`
}

// RevenueClass is a row of revenue_classes: a category, or a subcategory when ParentID is set (one level).
type RevenueClass struct {
	bun.BaseModel `bun:"table:revenue_classes"`

	ID         string    `bun:"id,pk"`
	MarinaID   string    `bun:"marina_id"`
	Code       string    `bun:"code"`
	Name       JSON      `bun:"name"` // I18nText
	ParentID   string    `bun:"parent_id,nullzero"`
	SortOrder  int       `bun:"sort_order"`
	ArchivedAt time.Time `bun:"archived_at,nullzero"`
	Version    int       `bun:"version"`
	CreatedAt  time.Time `bun:"created_at"`
	UpdatedAt  time.Time `bun:"updated_at"`
}

// Item is a row of items (spec 12.1).
type Item struct {
	bun.BaseModel `bun:"table:items"`

	ID               string    `bun:"id,pk"`
	MarinaID         string    `bun:"marina_id"`
	Code             string    `bun:"code"` // upper case
	Name             JSON      `bun:"name"` // I18nText
	Description      JSON      `bun:"description"`
	ItemType         string    `bun:"item_type"` // pricing.ItemType
	Unit             string    `bun:"unit"`      // night, month, season, year, m, m2, kwh, m3, hour, piece, lump
	VATRateBP        int64     `bun:"vat_rate_bp"`
	VATNature        string    `bun:"vat_nature,nullzero"` // set exactly when VATRateBP == 0 (CHECK)
	PriceIncludesVAT bool      `bun:"price_includes_vat"`
	RevenueClassID   string    `bun:"revenue_class_id"`
	ExternalCode     string    `bun:"external_code"`
	Active           bool      `bun:"active"`
	OnlineVisible    bool      `bun:"online_visible"`
	SortOrder        int       `bun:"sort_order"`
	Version          int       `bun:"version"`
	CreatedAt        time.Time `bun:"created_at"`
	UpdatedAt        time.Time `bun:"updated_at"`
}

// BerthCategory is a row of berth_categories: a registered price_class code (spec 07, 12.2).
type BerthCategory struct {
	bun.BaseModel `bun:"table:berth_categories"`

	ID         string    `bun:"id,pk"`
	MarinaID   string    `bun:"marina_id"`
	Code       string    `bun:"code"`
	Name       JSON      `bun:"name"`       // I18nText
	AppliesTo  Strings   `bun:"applies_to"` // berth, dynamic_area, land_resource
	SortOrder  int       `bun:"sort_order"`
	ArchivedAt time.Time `bun:"archived_at,nullzero"`
	Version    int       `bun:"version"`
	CreatedAt  time.Time `bun:"created_at"`
	UpdatedAt  time.Time `bun:"updated_at"`
}

// Price list statuses.
const (
	PriceListDraft    = "draft"
	PriceListActive   = "active"
	PriceListArchived = "archived"
)

// PriceList is a row of price_lists (spec 12.2).
type PriceList struct {
	bun.BaseModel `bun:"table:price_lists"`

	ID                string        `bun:"id,pk"`
	MarinaID          string        `bun:"marina_id"`
	Code              string        `bun:"code"`
	Name              JSON          `bun:"name"` // I18nText
	Status            string        `bun:"status"`
	ValidFrom         calendar.Date `bun:"valid_from"`
	ValidTo           calendar.Date `bun:"valid_to"`
	ContractTypes     Strings       `bun:"contract_types"` // pricing.ContractType values
	Settings          JSON          `bun:"settings"`       // pricing.ListSettings
	SourcePriceListID string        `bun:"source_price_list_id,nullzero"`
	ContentSHA256     string        `bun:"content_sha256,nullzero"` // pricing.CatalogHash at activation
	ActivatedAt       time.Time     `bun:"activated_at,nullzero"`
	ActivatedByUserID string        `bun:"activated_by_user_id,nullzero"`
	ArchivedAt        time.Time     `bun:"archived_at,nullzero"`
	NextRuleSeq       int64         `bun:"next_rule_seq"`
	Version           int           `bun:"version"`
	CreatedAt         time.Time     `bun:"created_at"`
	UpdatedAt         time.Time     `bun:"updated_at"`
}

// RatePeriod is a row of rate_periods: a season or an event of a price list.
type RatePeriod struct {
	bun.BaseModel `bun:"table:rate_periods"`

	ID          string    `bun:"id,pk"`
	MarinaID    string    `bun:"marina_id"`
	PriceListID string    `bun:"price_list_id"`
	Code        string    `bun:"code"`
	Name        JSON      `bun:"name"` // I18nText
	Kind        string    `bun:"kind"` // season, event
	Color       string    `bun:"color"`
	Ranges      JSON      `bun:"ranges"` // []pricing.DateRange
	SortOrder   int       `bun:"sort_order"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

// PriceListBand is a row of price_list_bands: a length band, inclusive up to MaxCm (0 = NULL = open-ended).
type PriceListBand struct {
	bun.BaseModel `bun:"table:price_list_bands"`

	ID           string    `bun:"id,pk"`
	MarinaID     string    `bun:"marina_id"`
	PriceListID  string    `bun:"price_list_id"`
	Code         string    `bun:"code"`
	MaxCm        int64     `bun:"max_cm,nullzero"`
	CategoryCode string    `bun:"category_code,nullzero"`
	SortOrder    int       `bun:"sort_order"`
	CreatedAt    time.Time `bun:"created_at"`
	UpdatedAt    time.Time `bun:"updated_at"`
}

// PriceRule is a row of price_rules. The indexed columns are copies of the rule; Body holds the rest of
// pricing.Rule (basis, unit, amounts, includes, targets, conditions, effect, discount kind, promo code, scope).
type PriceRule struct {
	bun.BaseModel `bun:"table:price_rules"`

	ID          string    `bun:"id,pk"`
	MarinaID    string    `bun:"marina_id"`
	PriceListID string    `bun:"price_list_id"`
	Seq         int64     `bun:"seq"`
	Stage       string    `bun:"stage"` // base, modifier, discount
	Name        string    `bun:"name"`
	ItemID      string    `bun:"item_id,nullzero"` // required for base rules (CHECK)
	Priority    int64     `bun:"priority"`
	Stackable   bool      `bun:"stackable"`
	Active      bool      `bun:"active"`
	Body        JSON      `bun:"body"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
}

// PriceListItem is a row of price_list_items: the engine fields of an item referenced by a list, copied when the
// list is activated (INV-26). Lists that are not drafts price with these rows, never with the live item.
type PriceListItem struct {
	bun.BaseModel `bun:"table:price_list_items"`

	ID               string    `bun:"id,pk"`
	MarinaID         string    `bun:"marina_id"`
	PriceListID      string    `bun:"price_list_id"`
	ItemID           string    `bun:"item_id"`
	Code             string    `bun:"code"`
	ItemType         string    `bun:"item_type"`
	Unit             string    `bun:"unit"`
	VATRateBP        int64     `bun:"vat_rate_bp"`
	VATNature        string    `bun:"vat_nature,nullzero"`
	PriceIncludesVAT bool      `bun:"price_includes_vat"`
	RevenueClass     string    `bun:"revenue_class"` // class code at activation
	ExternalCode     string    `bun:"external_code"`
	Active           bool      `bun:"active"`
	CreatedAt        time.Time `bun:"created_at"`
}
