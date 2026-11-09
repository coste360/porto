// Package jsonld builds the schema.org JSON-LD of a published marina profile (m12-plan §12.6): @type
// ["Marina", "Place"], name, url, description, contacts, PostalAddress, GeoCoordinates formatted as exact
// decimals from the e7 integers (no float), opening hours, amenities with labels from a Go table checked against
// the i18n keys, photos, website as sameAs, ReserveAction to /b/{slug} only when booking is enabled. Pure: no store,
// no clock. Marshal with HTML escaping so "</script>" cannot break out of the page.
package jsonld

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Hours is one opening interval (weekday 1 = Monday, "HH:MM").
type Hours struct {
	Weekday  int
	From, To string
}

// Profile is what the JSON-LD needs of a profile, already resolved in one language.
type Profile struct {
	Slug, Name, Description  string
	Phone, Email, Website    string
	Street, PostalCode, City string
	Province, Country        string
	LatE7, LonE7             *int64
	OfficeHours              []Hours
	Amenities                map[string]bool // amenity keys (AmenityKeys) of the profile
	PhotoIDs                 []string
	BookingEnabled           bool
}

const e7 = 10_000_000

// amenity is one row of the label table: the key is the i18n key services.<key> of the publicProfile namespace.
type amenity struct{ key, it, en string }

// amenities is the label table, in output order. TestJSONLD_LabelsMatchI18n keeps it equal to the publicProfile
// i18n values (web/shared/src/i18n/locales/{it,en}/publicProfile.json, key services.<key>).
var amenities = []amenity{
	{"water", "Acqua", "Water"},
	{"power", "Elettricità", "Electricity"},
	{"fuel", "Carburante", "Fuel"},
	{"wifi", "Wi-Fi", "Wi-Fi"},
	{"toilets", "Servizi igienici", "Toilets"},
	{"showers", "Docce", "Showers"},
	{"laundry", "Lavanderia", "Laundry"},
	{"pump_out", "Svuotamento acque nere", "Pump-out"},
	{"waste", "Raccolta rifiuti", "Waste disposal"},
	{"travel_lift", "Travel lift", "Travel lift"},
	{"crane", "Gru", "Crane"},
	{"slipway", "Scivolo", "Slipway"},
	{"yard", "Cantiere", "Boatyard"},
	{"storage", "Rimessaggio", "Dry storage"},
	{"rental", "Noleggio", "Rental"},
	{"parking", "Parcheggio", "Parking"},
	{"restaurant", "Ristorante", "Restaurant"},
	{"shops", "Negozi", "Shops"},
	{"transport", "Trasporti", "Transport"},
}

var weekdays = [...]string{"", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}

// AmenityKeys returns the amenity keys in output order.
func AmenityKeys() []string {
	out := make([]string, len(amenities))
	for i, a := range amenities {
		out[i] = a.key
	}
	return out
}

// AmenityLabel returns the label of an amenity key in lang (it, en; anything else is it); "" when unknown.
func AmenityLabel(key, lang string) string {
	for _, a := range amenities {
		if a.key == key {
			if lang == "en" {
				return a.en
			}
			return a.it
		}
	}
	return ""
}

// FormatE7 formats a WGS84 e7 integer as an exact decimal string with 7 decimals (437101234 -> "43.7101234",
// -5000000 -> "-0.5000000").
func FormatE7(v int64) string {
	neg := v < 0
	u := uint64(v)
	if neg {
		u = uint64(-(v + 1)) + 1 // safe for math.MinInt64
	}
	frac := strconv.FormatUint(u%e7, 10)
	s := strconv.FormatUint(u/e7, 10) + "." + strings.Repeat("0", 7-len(frac)) + frac
	if neg {
		return "-" + s
	}
	return s
}

// Build returns the JSON-LD object for lang (it, en) under baseURL (PORTO_BASE_URL, no trailing slash needed).
// Empty fields are omitted.
func Build(p Profile, lang, baseURL string) map[string]any {
	base := strings.TrimRight(baseURL, "/")
	page := base + "/p/" + p.Slug
	out := map[string]any{
		"@context": "https://schema.org",
		"@type":    []string{"Marina", "Place"},
		"@id":      page + "#marina",
		"name":     p.Name,
		"url":      page,
	}
	if lang == "it" || lang == "en" {
		out["inLanguage"] = lang
	}
	set := func(k, v string) {
		if v = strings.TrimSpace(v); v != "" {
			out[k] = v
		}
	}
	set("description", p.Description)
	set("telephone", p.Phone)
	set("email", p.Email)
	addr := map[string]any{}
	for k, v := range map[string]string{
		"streetAddress": p.Street, "postalCode": p.PostalCode, "addressLocality": p.City,
		"addressRegion": p.Province, "addressCountry": p.Country,
	} {
		if v = strings.TrimSpace(v); v != "" {
			addr[k] = v
		}
	}
	if len(addr) > 0 {
		addr["@type"] = "PostalAddress"
		out["address"] = addr
	}
	if p.LatE7 != nil && p.LonE7 != nil {
		out["geo"] = map[string]any{"@type": "GeoCoordinates", "latitude": FormatE7(*p.LatE7), "longitude": FormatE7(*p.LonE7)}
	}
	if hours := openingHours(p.OfficeHours); len(hours) > 0 {
		out["openingHoursSpecification"] = hours
	}
	feats := []any{}
	for _, a := range amenities {
		if p.Amenities[a.key] {
			feats = append(feats, map[string]any{"@type": "LocationFeatureSpecification", "name": AmenityLabel(a.key, lang), "value": true})
		}
	}
	if len(feats) > 0 {
		out["amenityFeature"] = feats
	}
	if len(p.PhotoIDs) > 0 {
		imgs := make([]string, 0, len(p.PhotoIDs))
		for _, id := range p.PhotoIDs {
			imgs = append(imgs, base+"/api/public/v1/"+p.Slug+"/profile/photos/"+id)
		}
		out["image"] = imgs
	}
	if w := strings.TrimSpace(p.Website); w != "" {
		out["sameAs"] = []string{w}
	}
	if p.BookingEnabled {
		out["potentialAction"] = map[string]any{"@type": "ReserveAction", "target": base + "/b/" + p.Slug}
	}
	return out
}

func openingHours(hs []Hours) []any {
	out := []any{}
	for _, h := range hs {
		if h.Weekday < 1 || h.Weekday > 7 || h.From == "" || h.To == "" {
			continue
		}
		out = append(out, map[string]any{
			"@type": "OpeningHoursSpecification", "dayOfWeek": "https://schema.org/" + weekdays[h.Weekday],
			"opens": h.From, "closes": h.To,
		})
	}
	return out
}

// Marshal encodes the object for a <script type="application/ld+json"> element: keys sorted, HTML characters
// (<, >, &) and U+2028/U+2029 escaped, so no value can close the script element.
func Marshal(obj map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(obj); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
