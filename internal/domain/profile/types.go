package profile

import (
	"log/slog"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// Deps are the collaborators of the service.
type Deps struct {
	DB     *store.DB
	Clock  clock.Clock
	Audit  *audit.Writer
	Outbox *event.Outbox
	Log    *slog.Logger
	Files  *filestore.Store
	Prices *pricelist.Service
	// BaseURL is PORTO_BASE_URL (canonical and JSON-LD URLs).
	BaseURL string
	// LocalMode is true on the desktop edition (the page is reachable only on the local network).
	LocalMode bool
}

// Text is an I18n text {it, en}.
type Text map[string]string

// Hours is one opening interval (weekday 1 = Monday).
type Hours struct {
	Weekday int    `json:"weekday"`
	From    string `json:"from"`
	To      string `json:"to"`
}

// Photo is a photo referenced by the profile.
type Photo struct {
	FileID string `json:"file_id"`
	Alt    Text   `json:"alt"`
	Sort   int    `json:"sort"`
}

// POI is a point of interest on the simplified map.
type POI struct {
	Kind string `json:"kind"` // entrance, fuel, office, pump_out
	XCm  int64  `json:"x_cm"`
	YCm  int64  `json:"y_cm"`
}

// Address is the postal address of the profile (JSON column address).
type Address struct {
	Street     string `json:"street,omitempty"`
	PostalCode string `json:"postal_code,omitempty"`
	City       string `json:"city,omitempty"`
	Province   string `json:"province,omitempty"`
	Country    string `json:"country,omitempty"`
}

// Fuel is the fuel station of the services.
type Fuel struct {
	Petrol bool `json:"petrol,omitempty"`
	Diesel bool `json:"diesel,omitempty"`
	Hours  Text `json:"hours,omitempty"`
}

// Services are the typed services of the profile (JSON column services, m12-plan §12.1); booleans default false.
type Services struct {
	Water        bool   `json:"water,omitempty"`
	PowerAmps    []int  `json:"power_amps,omitempty"`
	Fuel         *Fuel  `json:"fuel,omitempty"`
	WiFi         bool   `json:"wifi,omitempty"`
	Toilets      bool   `json:"toilets,omitempty"`
	Showers      bool   `json:"showers,omitempty"`
	Laundry      bool   `json:"laundry,omitempty"`
	PumpOut      bool   `json:"pump_out,omitempty"`
	Waste        bool   `json:"waste,omitempty"`
	TravelLiftKg *int64 `json:"travel_lift_kg,omitempty"`
	CraneKg      *int64 `json:"crane_kg,omitempty"`
	Slipway      bool   `json:"slipway,omitempty"`
	Yard         bool   `json:"yard,omitempty"`
	Storage      bool   `json:"storage,omitempty"`
	Rental       bool   `json:"rental,omitempty"`
	Parking      bool   `json:"parking,omitempty"`
	Restaurant   bool   `json:"restaurant,omitempty"`
	Shops        bool   `json:"shops,omitempty"`
	Transport    bool   `json:"transport,omitempty"`
}

// Input saves the profile (putMarinaProfile); it replaces every field.
type Input struct {
	Locales                            []string
	EntranceLatE7, EntranceLonE7       *int64
	Address                            Address
	Phone, Email, Website              string
	VHFChannels                        []int
	OfficeHours, DockHours             []Hours
	HoursNote                          Text
	BerthsTotalOverride                *int
	TransitBerthsOverride              *int
	MaxLoaCm                           *int64
	DepthEntranceCm, DepthQuayCm       *int64
	DepthMinCm, DepthMaxCm             *int64
	Seabed                             string
	Shelter                            []string
	AccessNotes, Lights, Description   Text
	AccessImages, Photos               []Photo
	Services                           Services
	ShowMap, ShowPrices, ShowOccupancy bool
	MapID                              string
	OccupancyCategories                []string
	POI                                []POI
}

// View is the panel view of the profile: the row (defaults prefilled from the marina, Version 0, when never saved)
// and the computed figures.
type View struct {
	store.MarinaProfile
	BerthsTotal, TransitBerths int
	PricesAvailable            bool
	Missing                    []string
	PublicURL                  string
	// LocalMode is true on the desktop edition (the panel says the page is reachable on the local network only).
	LocalMode bool
}

// PhotoFile is an uploaded, re-encoded photo.
type PhotoFile struct {
	FileID        string
	ContentType   string
	Width, Height int
	SizeBytes     int64
}

// Image is the bytes of a photo to serve.
type Image struct {
	ContentType string
	Body        []byte
	ETag        string
}

// Public is the lang-resolved public JSON of a published profile (getPublicProfile).
type Public struct {
	Slug, Name, Lang string
	Locales          []string
	Version          int
	ETag             string
	Data             *PublicData // api.PublicProfile-shaped (same JSON names)
}

// Head is the rendered head of /p/{slug} (title, meta, canonical, hreflang, Open Graph, JSON-LD) and its cache keys.
type Head struct {
	HTML    string
	ETag    string
	Version int
	Expires time.Time
}

// PublicPhoto is a photo of the public JSON.
type PublicPhoto struct {
	URL string `json:"url"`
	Alt string `json:"alt"`
}

// PublicSections are the section flags of the public JSON.
type PublicSections struct {
	Map       bool `json:"map"`
	Prices    bool `json:"prices"`
	Occupancy bool `json:"occupancy"`
}

// PublicData is the public JSON of a profile (api.PublicProfile: the same JSON names). It holds the marina's
// business contacts and the harbour description only: no personal data, no per-berth data (INV-103).
type PublicData struct {
	Slug            string         `json:"slug"`
	Name            string         `json:"name"`
	Lang            string         `json:"lang"`
	Locales         []string       `json:"locales"`
	Description     string         `json:"description,omitempty"`
	EntranceLatE7   *int64         `json:"entrance_lat_e7,omitempty"`
	EntranceLonE7   *int64         `json:"entrance_lon_e7,omitempty"`
	Latitude        string         `json:"latitude,omitempty"`
	Longitude       string         `json:"longitude,omitempty"`
	Address         *Address       `json:"address,omitempty"`
	Phone           string         `json:"phone,omitempty"`
	Email           string         `json:"email,omitempty"`
	Website         string         `json:"website,omitempty"`
	VHFChannels     []int          `json:"vhf_channels,omitempty"`
	OfficeHours     []Hours        `json:"office_hours,omitempty"`
	DockHours       []Hours        `json:"dock_hours,omitempty"`
	HoursNote       string         `json:"hours_note,omitempty"`
	BerthsTotal     *int           `json:"berths_total,omitempty"`
	TransitBerths   *int           `json:"transit_berths,omitempty"`
	MaxLoaCm        *int64         `json:"max_loa_cm,omitempty"`
	DepthEntranceCm *int64         `json:"depth_entrance_cm,omitempty"`
	DepthQuayCm     *int64         `json:"depth_quay_cm,omitempty"`
	DepthMinCm      *int64         `json:"depth_min_cm,omitempty"`
	DepthMaxCm      *int64         `json:"depth_max_cm,omitempty"`
	Seabed          string         `json:"seabed,omitempty"`
	Shelter         []string       `json:"shelter,omitempty"`
	AccessNotes     string         `json:"access_notes,omitempty"`
	AccessImages    []PublicPhoto  `json:"access_images,omitempty"`
	Lights          string         `json:"lights,omitempty"`
	Services        *Services      `json:"services,omitempty"`
	FuelHours       string         `json:"fuel_hours,omitempty"`
	Photos          []PublicPhoto  `json:"photos,omitempty"`
	Sections        PublicSections `json:"sections"`
	BookingURL      string         `json:"booking_url,omitempty"`
	PageURL         string         `json:"page_url"`
	Version         int            `json:"version"`
}

// PublicShape is an element of the public map (api.SimpleShape).
type PublicShape struct {
	Points   [][2]int64 `json:"points"`
	Closed   bool       `json:"closed,omitempty"`
	WidthCm  *int64     `json:"width_cm,omitempty"`
	Category string     `json:"category,omitempty"`
}

// PublicMapData is the public simplified map (api.PublicProfileMap).
type PublicMapData struct {
	MinXCm     int64         `json:"min_x_cm"`
	MinYCm     int64         `json:"min_y_cm"`
	MaxXCm     int64         `json:"max_x_cm"`
	MaxYCm     int64         `json:"max_y_cm"`
	Structures []PublicShape `json:"structures"`
	Berths     []PublicShape `json:"berths"`
	Areas      []PublicShape `json:"areas"`
	Land       []PublicShape `json:"land"`
	POI        []POI         `json:"poi"`
	Version    int           `json:"version"`
}

// PublicPriceRow is a row of the public price matrix (api.PublicPriceRow).
type PublicPriceRow struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	MaxLoaCm  *int64 `json:"max_loa_cm,omitempty"`
	MaxBeamCm *int64 `json:"max_beam_cm,omitempty"`
}

// PublicPriceColumn is a column of the public price matrix (api.PublicPriceColumn).
type PublicPriceColumn struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Arrival   string `json:"arrival"`
	Departure string `json:"departure"`
}

// PublicPriceCell is a cell of the public price matrix (api.PublicPriceCell).
type PublicPriceCell struct {
	Row        int    `json:"row"`
	Col        int    `json:"col"`
	TotalCents *int64 `json:"total_cents,omitempty"`
	Unit       string `json:"unit,omitempty"`
}

// PublicPrices is the public transit price matrix (api.PublicPriceMatrix).
type PublicPrices struct {
	QuotedOn         string              `json:"quoted_on"`
	Currency         string              `json:"currency"`
	PriceIncludesVAT bool                `json:"price_includes_vat"`
	Rows             []PublicPriceRow    `json:"rows"`
	Columns          []PublicPriceColumn `json:"columns"`
	Cells            []PublicPriceCell   `json:"cells"`
}

// CategoryName is the I18n name of a berth category (api.I18nText).
type CategoryName struct {
	It string  `json:"it"`
	En *string `json:"en,omitempty"`
}

// PublicOccupancyCategory is one category of the aggregate occupancy (api.PublicOccupancyCategory).
type PublicOccupancyCategory struct {
	Code        string       `json:"code"`
	Name        CategoryName `json:"name"`
	FreeBerths  int          `json:"free_berths"`
	TotalBerths int          `json:"total_berths"`
}

// PublicOccupancyData is the aggregate occupancy (api.PublicOccupancy): counts only, never ids or positions.
type PublicOccupancyData struct {
	AsOf       time.Time                 `json:"as_of"`
	Night      string                    `json:"night"`
	Categories []PublicOccupancyCategory `json:"categories"`
}
