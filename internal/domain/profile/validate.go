package profile

import (
	"context"
	"errors"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// Limits of m12-plan §12.1 and the OpenAPI schema.
const (
	maxVHF          = 4
	maxHours        = 21
	maxPhotos       = 12
	maxAccessImages = 4
	maxPOI          = 16
	maxCategories   = 20
	maxAltRunes     = 200
	maxHoursNote    = 300
	maxAccessNotes  = 2000
	maxLights       = 1000
	maxDescription  = 1000
)

// Winds in canonical order (shelter).
var winds = []string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}

var (
	seabeds   = []string{store.ProfileSeabedSand, store.ProfileSeabedMud, store.ProfileSeabedRock, store.ProfileSeabedWeed, store.ProfileSeabedMixed, store.ProfileSeabedOther}
	poiKinds  = []string{"entrance", "fuel", "office", "pump_out"}
	powerAmps = []int{16, 32, 63, 125}
	hhmm      = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
)

func trimText(t Text) Text {
	out := Text{}
	for k, v := range t {
		if v = strings.TrimSpace(v); v != "" {
			out[k] = v
		}
	}
	return out
}

func trimPhotos(ps []Photo) []Photo {
	out := make([]Photo, 0, len(ps))
	for i, p := range ps {
		out = append(out, Photo{FileID: strings.TrimSpace(p.FileID), Alt: trimText(p.Alt), Sort: i})
	}
	return out
}

// normalize trims texts, orders lists canonically and replaces nil slices: photos keep the order given (sort is
// rewritten as the index), shelter follows the compass order, VHF channels are sorted, locales always hold it.
func normalize(in Input) Input {
	in.Phone, in.Email, in.Website = strings.TrimSpace(in.Phone), strings.TrimSpace(in.Email), strings.TrimSpace(in.Website)
	in.Seabed, in.MapID = strings.TrimSpace(in.Seabed), strings.TrimSpace(in.MapID)
	a := &in.Address
	a.Street, a.PostalCode, a.City = strings.TrimSpace(a.Street), strings.TrimSpace(a.PostalCode), strings.TrimSpace(a.City)
	a.Province, a.Country = strings.ToUpper(strings.TrimSpace(a.Province)), strings.ToUpper(strings.TrimSpace(a.Country))
	in.HoursNote, in.AccessNotes, in.Lights, in.Description = trimText(in.HoursNote), trimText(in.AccessNotes), trimText(in.Lights), trimText(in.Description)
	in.Photos, in.AccessImages = trimPhotos(in.Photos), trimPhotos(in.AccessImages)
	locales := []string{"it"}
	for _, l := range in.Locales {
		if !slices.Contains(locales, l) {
			locales = append(locales, l)
		}
	}
	in.Locales = locales
	in.VHFChannels = slices.Clone(in.VHFChannels)
	if in.VHFChannels == nil {
		in.VHFChannels = []int{}
	}
	slices.Sort(in.VHFChannels)
	in.VHFChannels = slices.Compact(in.VHFChannels)
	shelter := []string{}
	for _, w := range winds {
		if slices.Contains(in.Shelter, w) {
			shelter = append(shelter, w)
		}
	}
	for _, w := range in.Shelter {
		if !slices.Contains(winds, w) {
			shelter = append(shelter, w) // invalid, reported by validate
		}
	}
	in.Shelter = shelter
	for _, l := range []*[]Hours{&in.OfficeHours, &in.DockHours} {
		*l = slices.Clone(*l)
		if *l == nil {
			*l = []Hours{}
		}
		slices.SortStableFunc(*l, func(x, y Hours) int {
			if x.Weekday != y.Weekday {
				return x.Weekday - y.Weekday
			}
			return strings.Compare(x.From, y.From)
		})
	}
	cats := []string{}
	for _, c := range in.OccupancyCategories {
		if c = strings.TrimSpace(c); c != "" && !slices.Contains(cats, c) {
			cats = append(cats, c)
		}
	}
	in.OccupancyCategories = cats
	in.POI = slices.Clone(in.POI)
	if in.POI == nil {
		in.POI = []POI{}
	}
	s := &in.Services
	s.PowerAmps = slices.Clone(s.PowerAmps)
	slices.Sort(s.PowerAmps)
	s.PowerAmps = slices.Compact(s.PowerAmps)
	if s.Fuel != nil {
		f := *s.Fuel
		f.Hours = trimText(f.Hours)
		s.Fuel = &f
		if !f.Petrol && !f.Diesel && len(f.Hours) == 0 {
			s.Fuel = nil
		}
	}
	return in
}

func checkText(v *auth.ValidationError, field string, t Text, limit int) {
	for k, s := range t {
		if k != "it" && k != "en" {
			v.Add(field, "invalid")
			return
		}
		if utf8.RuneCountInString(s) > limit {
			v.Add(field+"."+k, "too_long")
		}
	}
}

func checkHours(v *auth.ValidationError, field string, hs []Hours) {
	if len(hs) > maxHours {
		v.Add(field, "too_many")
		return
	}
	for i, h := range hs {
		f := field + "[" + strconv.Itoa(i) + "]"
		if h.Weekday < 1 || h.Weekday > 7 {
			v.Add(f+".weekday", "invalid")
		}
		if !hhmm.MatchString(h.From) {
			v.Add(f+".from", "invalid")
		}
		if (!hhmm.MatchString(h.To) && h.To != "24:00") || h.To <= h.From {
			v.Add(f+".to", "invalid")
		}
	}
}

func checkLen(v *auth.ValidationError, field string, p *int64, positive bool) {
	if p == nil {
		return
	}
	if *p < 0 || (positive && *p == 0) || *p > 10_000_000 {
		v.Add(field, "range")
	}
}

// validate checks the normalized input against the marina: locales, files of profile uploads, map and berth
// categories of the marina.
func (s *Service) validate(ctx context.Context, db bun.IDB, scope store.MarinaScope, m *store.Marina, in Input) error {
	v := &auth.ValidationError{}
	allowed := marinaLocales(m)
	for _, l := range in.Locales {
		if !slices.Contains(allowed, l) {
			v.Add("locales", "invalid")
			break
		}
	}
	if (in.EntranceLatE7 == nil) != (in.EntranceLonE7 == nil) {
		v.Add("entrance_lat_e7", "required")
	}
	if in.EntranceLatE7 != nil && (*in.EntranceLatE7 < -900_000_000 || *in.EntranceLatE7 > 900_000_000) {
		v.Add("entrance_lat_e7", "range")
	}
	if in.EntranceLonE7 != nil && (*in.EntranceLonE7 < -1_800_000_000 || *in.EntranceLonE7 > 1_800_000_000) {
		v.Add("entrance_lon_e7", "range")
	}
	a := in.Address
	for field, lim := range map[string][2]int{"address.street": {len(a.Street), 200}, "address.postal_code": {len(a.PostalCode), 16}, "address.city": {len(a.City), 100}, "address.province": {len(a.Province), 8}} {
		if lim[0] > lim[1] {
			v.Add(field, "too_long")
		}
	}
	if a.Country != "" && (len(a.Country) != 2 || strings.ToUpper(a.Country) != a.Country || strings.ContainsAny(a.Country, "0123456789")) {
		v.Add("address.country", "invalid")
	}
	if len(in.Phone) > 40 {
		v.Add("phone", "too_long")
	}
	if in.Email != "" {
		if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email || len(in.Email) > 254 {
			v.Add("email", "invalid")
		}
	}
	if in.Website != "" {
		u, err := url.Parse(in.Website)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(in.Website) > 300 {
			v.Add("website", "invalid")
		}
	}
	if len(in.VHFChannels) > maxVHF {
		v.Add("vhf_channels", "too_many")
	}
	for _, c := range in.VHFChannels {
		if c < 1 || c > 88 {
			v.Add("vhf_channels", "range")
			break
		}
	}
	checkHours(v, "office_hours", in.OfficeHours)
	checkHours(v, "dock_hours", in.DockHours)
	checkText(v, "hours_note", in.HoursNote, maxHoursNote)
	checkText(v, "access_notes", in.AccessNotes, maxAccessNotes)
	checkText(v, "lights", in.Lights, maxLights)
	checkText(v, "description", in.Description, maxDescription)
	for field, p := range map[string]*int{"berths_total_override": in.BerthsTotalOverride, "transit_berths_override": in.TransitBerthsOverride} {
		if p != nil && (*p < 0 || *p > 100_000) {
			v.Add(field, "range")
		}
	}
	checkLen(v, "max_loa_cm", in.MaxLoaCm, true)
	checkLen(v, "depth_entrance_cm", in.DepthEntranceCm, false)
	checkLen(v, "depth_quay_cm", in.DepthQuayCm, false)
	checkLen(v, "depth_min_cm", in.DepthMinCm, false)
	checkLen(v, "depth_max_cm", in.DepthMaxCm, false)
	if in.DepthMinCm != nil && in.DepthMaxCm != nil && *in.DepthMinCm > *in.DepthMaxCm {
		v.Add("depth_min_cm", "greater_than_max")
	}
	if in.Seabed != "" && !slices.Contains(seabeds, in.Seabed) {
		v.Add("seabed", "invalid")
	}
	for _, w := range in.Shelter {
		if !slices.Contains(winds, w) {
			v.Add("shelter", "invalid")
			break
		}
	}
	for _, a := range in.Services.PowerAmps {
		if !slices.Contains(powerAmps, a) {
			v.Add("services.power_amps", "invalid")
			break
		}
	}
	for field, p := range map[string]*int64{"services.travel_lift_kg": in.Services.TravelLiftKg, "services.crane_kg": in.Services.CraneKg} {
		if p != nil && (*p < 0 || *p > 10_000_000) {
			v.Add(field, "range")
		}
	}
	if in.Services.Fuel != nil {
		checkText(v, "services.fuel.hours", in.Services.Fuel.Hours, maxHoursNote)
	}
	if len(in.Photos) > maxPhotos {
		v.Add("photos", "too_many")
	}
	if len(in.AccessImages) > maxAccessImages {
		v.Add("access_images", "too_many")
	}
	if len(in.POI) > maxPOI {
		v.Add("poi", "too_many")
	}
	for i, p := range in.POI {
		if !slices.Contains(poiKinds, p.Kind) {
			v.Add("poi["+strconv.Itoa(i)+"].kind", "invalid")
		}
		if p.XCm < -10_000_000 || p.XCm > 10_000_000 || p.YCm < -10_000_000 || p.YCm > 10_000_000 {
			v.Add("poi["+strconv.Itoa(i)+"]", "range")
		}
	}
	if len(in.OccupancyCategories) > maxCategories {
		v.Add("occupancy_categories", "too_many")
	}
	if in.ShowOccupancy && len(in.OccupancyCategories) == 0 {
		v.Add("occupancy_categories", "required")
	}
	if err := v.Err(); err != nil {
		return err
	}
	if err := s.checkPhotos(ctx, db, scope, v, "photos", in.Photos); err != nil {
		return err
	}
	if err := s.checkPhotos(ctx, db, scope, v, "access_images", in.AccessImages); err != nil {
		return err
	}
	if in.MapID != "" {
		if _, err := store.GetMap(ctx, db, scope, in.MapID); err != nil {
			if !isNotFound(err) {
				return err
			}
			v.Add("map_id", "unknown")
		}
	}
	if len(in.OccupancyCategories) > 0 {
		cats, err := store.ListBerthCategories(ctx, db, scope, false)
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for _, c := range cats {
			known[c.Code] = true
		}
		for _, c := range in.OccupancyCategories {
			if !known[c] {
				v.Add("occupancy_categories", "unknown")
				break
			}
		}
	}
	return v.Err()
}

// checkPhotos accepts only live images uploaded through the profile (an audit row of the upload exists), each once.
func (s *Service) checkPhotos(ctx context.Context, db bun.IDB, scope store.MarinaScope, v *auth.ValidationError, field string, ps []Photo) error {
	seen := map[string]bool{}
	for i, p := range ps {
		f := field + "[" + strconv.Itoa(i) + "]"
		checkText(v, f+".alt", p.Alt, maxAltRunes)
		if seen[p.FileID] {
			v.Add(f+".file_id", "duplicate")
			continue
		}
		seen[p.FileID] = true
		ok, err := s.isProfilePhoto(ctx, db, scope, p.FileID)
		if err != nil {
			return err
		}
		if !ok {
			v.Add(f+".file_id", "unknown")
		}
	}
	return nil
}

// isProfilePhoto reports whether fileID is a live JPEG/PNG of the marina uploaded through UploadPhoto.
func (s *Service) isProfilePhoto(ctx context.Context, db bun.IDB, scope store.MarinaScope, fileID string) (bool, error) {
	f, err := store.GetStoredFile(ctx, db, scope, fileID)
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if f.ContentType != filestore.TypeJPEG && f.ContentType != filestore.TypePNG {
		return false, nil
	}
	return store.ProfilePhotoUploaded(ctx, db, scope, fileID)
}

func isNotFound(err error) bool {
	return errors.Is(err, store.ErrNotFound) || errors.Is(err, auth.ErrNotFound)
}

// apply writes the input over the row (publication and keys untouched).
func apply(row store.MarinaProfile, in Input) store.MarinaProfile {
	row.Locales = store.MustJSON(in.Locales)
	row.EntranceLatE7, row.EntranceLonE7 = in.EntranceLatE7, in.EntranceLonE7
	row.Address = store.MustJSON(in.Address)
	row.Phone, row.Email, row.Website = in.Phone, in.Email, in.Website
	row.VHFChannels = store.MustJSON(in.VHFChannels)
	row.OfficeHours, row.DockHours = store.MustJSON(in.OfficeHours), store.MustJSON(in.DockHours)
	row.HoursNote = store.MustJSON(in.HoursNote)
	row.BerthsTotalOverride, row.TransitBerthsOverride = in.BerthsTotalOverride, in.TransitBerthsOverride
	row.MaxLoaCm = in.MaxLoaCm
	row.DepthEntranceCm, row.DepthQuayCm, row.DepthMinCm, row.DepthMaxCm = in.DepthEntranceCm, in.DepthQuayCm, in.DepthMinCm, in.DepthMaxCm
	row.Seabed = in.Seabed
	row.Shelter = store.MustJSON(in.Shelter)
	row.AccessNotes, row.Lights, row.Description = store.MustJSON(in.AccessNotes), store.MustJSON(in.Lights), store.MustJSON(in.Description)
	row.AccessImages, row.Photos = store.MustJSON(in.AccessImages), store.MustJSON(in.Photos)
	row.Services = store.MustJSON(in.Services)
	row.ShowMap, row.ShowPrices, row.ShowOccupancy = in.ShowMap, in.ShowPrices, in.ShowOccupancy
	row.MapID = in.MapID
	row.OccupancyCategories = store.MustJSON(in.OccupancyCategories)
	row.POI = store.MustJSON(in.POI)
	return row
}
