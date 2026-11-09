package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"slices"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/profile"
	"github.com/coste360/porto/internal/store"
)

// seedProfile adds the M12 demo of PUBLIC (m12-plan §17.2): a published profile of the demo marina with 3
// generated PNG photos, the simplified map with points of interest, the transit prices section and the aggregate
// occupancy on the two berth categories with the most berths. Everything goes through the profile service as the
// demo owner; a no-op when the marina already has a saved profile. The prices section shows the matrix of the
// active transit list only when that list publishes it (settings.publish_matrix).
func (a *App) seedProfile(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.Profiles == nil || res.MarinaID == "" {
		return nil
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if _, err := store.GetMarinaProfile(ctx, a.DB.DB, scope); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	photos := make([]profile.Photo, 0, 3)
	alts := []profile.Text{
		{"it": "L'imboccatura del porto al tramonto", "en": "The harbour entrance at sunset"},
		{"it": "Il pontile dei transiti", "en": "The transit pier"},
		{"it": "La banchina di riva", "en": "The main quay"},
	}
	for i, alt := range alts {
		var buf bytes.Buffer
		if err := png.Encode(&buf, seedProfileImage(i)); err != nil {
			return err
		}
		f, err := a.Profiles.UploadPhoto(ctx, p, res.MarinaID, "image/png", &buf)
		if err != nil {
			return fmt.Errorf("photo %d: %w", i, err)
		}
		photos = append(photos, profile.Photo{FileID: f.FileID, Alt: alt})
	}
	berths, err := store.PublicActiveBerths(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, b := range berths {
		if b.PriceClass != "" {
			counts[b.PriceClass]++
		}
	}
	known, err := store.ListBerthCategories(ctx, a.DB.DB, scope, false)
	if err != nil {
		return err
	}
	cats := []string{}
	for _, c := range known {
		if counts[c.Code] > 0 {
			cats = append(cats, c.Code)
		}
	}
	slices.SortStableFunc(cats, func(x, y string) int { return counts[y] - counts[x] })
	if len(cats) > 2 {
		cats = cats[:2]
	}
	m, err := store.GetMarina(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	locales := []string{"it"}
	if slices.Contains([]string(m.Locales), "en") {
		locales = append(locales, "en")
	}
	lat, lon := int64(409_065_068), int64(95_437_130) // the entrance on the demo georeference
	in := profile.Input{
		Locales: locales, EntranceLatE7: &lat, EntranceLonE7: &lon,
		Address: profile.Address{Street: "Lungomare del Porto 1", PostalCode: "04024", City: "Porto Demo", Province: "LT", Country: "IT"},
		Phone:   "+39 0771 000000", Email: "ormeggi@" + DemoEmailDomain, Website: "https://" + DemoEmailDomain,
		VHFChannels: []int{9, 16},
		OfficeHours: []profile.Hours{
			{Weekday: 1, From: "08:00", To: "19:00"},
			{Weekday: 2, From: "08:00", To: "19:00"},
			{Weekday: 3, From: "08:00", To: "19:00"},
			{Weekday: 4, From: "08:00", To: "19:00"},
			{Weekday: 5, From: "08:00", To: "19:00"},
			{Weekday: 6, From: "08:00", To: "13:00"},
		},
		DockHours: []profile.Hours{{Weekday: 1, From: "07:00", To: "21:00"}, {Weekday: 7, From: "07:00", To: "21:00"}},
		HoursNote: profile.Text{"it": "In luglio e agosto gli ormeggiatori sono in servizio fino alle 22.", "en": "In July and August the dock staff is on duty until 10 pm."},
		MaxLoaCm:  seedProfileCm(3000), DepthEntranceCm: seedProfileCm(600), DepthQuayCm: seedProfileCm(400), DepthMinCm: seedProfileCm(250), DepthMaxCm: seedProfileCm(600),
		Seabed: store.ProfileSeabedSand, Shelter: []string{"N", "NE", "E", "NW"},
		AccessNotes: profile.Text{"it": "Avvicinarsi da sud lasciando la diga foranea a sinistra; chiamare il canale 9 prima dell'ingresso.", "en": "Approach from the south keeping the breakwater to port; call channel 9 before entering."},
		Lights:      profile.Text{"it": "Testata della diga: luce verde a lampi ogni 5 s.", "en": "Breakwater head: green flashing light every 5 s."},
		Description: profile.Text{"it": "Porto turistico dimostrativo con posti per il transito, cantiere e noleggio.", "en": "Demo marina with transit berths, boatyard and rentals."},
		Services: profile.Services{
			Water: true, PowerAmps: []int{16, 32, 63}, Fuel: &profile.Fuel{Petrol: true, Diesel: true, Hours: profile.Text{"it": "08:00–19:00", "en": "8 am – 7 pm"}},
			WiFi: true, Toilets: true, Showers: true, Laundry: true, PumpOut: true, Waste: true, TravelLiftKg: seedProfileCm(50_000),
			Slipway: true, Yard: true, Storage: true, Rental: true, Parking: true, Restaurant: true,
		},
		Photos: photos, ShowMap: true, ShowPrices: true, ShowOccupancy: len(cats) > 0, OccupancyCategories: cats,
		POI: []profile.POI{{Kind: "entrance", XCm: 30000, YCm: 39000}, {Kind: "fuel", XCm: 2000, YCm: 3500}, {Kind: "office", XCm: 4000, YCm: 1500}},
	}
	v, err := a.Profiles.Put(ctx, p, res.MarinaID, 0, in)
	if err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	if _, err := a.Profiles.Publish(ctx, p, res.MarinaID, v.Version); err != nil {
		return fmt.Errorf("profile publish: %w", err)
	}
	return nil
}

func seedProfileCm(v int64) *int64 { return &v }

// seedProfileImage draws a deterministic 960×640 picture: sky, sea and a pier in three palettes.
func seedProfileImage(n int) image.Image {
	const w, h = 960, 640
	palettes := [][3]color.NRGBA{
		{{250, 196, 120, 255}, {20, 70, 120, 255}, {90, 60, 40, 255}},
		{{170, 210, 240, 255}, {30, 110, 160, 255}, {200, 200, 190, 255}},
		{{200, 225, 245, 255}, {15, 90, 130, 255}, {150, 140, 120, 255}},
	}
	pal := palettes[n%len(palettes)]
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	horizon := h * (2 + n%2) / 5
	for y := range h {
		for x := range w {
			c := pal[0]
			if y >= horizon {
				c = pal[1]
				// Gentle bands for the waves, integer arithmetic only.
				if (y-horizon)%24 < 2 && (x/40+y/24)%3 == 0 {
					c = color.NRGBA{min(255, c.R+60), min(255, c.G+60), min(255, c.B+60), 255}
				}
			} else {
				shade := uint8(y * 40 / max(1, horizon))
				c = color.NRGBA{c.R - min(c.R, shade), c.G - min(c.G, shade/2), c.B, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	// The pier: a band from the bottom towards the horizon.
	for y := horizon + 20; y < h; y++ {
		half := 20 + (y-horizon)*(80+n*20)/(h-horizon)
		cx := w/3 + n*w/6
		for x := max(0, cx-half); x < min(w, cx+half); x++ {
			img.SetNRGBA(x, y, pal[2])
		}
	}
	return img
}
