package app

import (
	"context"
	"fmt"
	"slices"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/domain/onlineconf"
	"github.com/coste360/porto/internal/store"
)

// Demo online booking (m8-plan §3, D7): the public page /b/demo works end to end on the demo. The transit berths
// (group TRANSITI), the berths of pier C (25% online quota) and the alongside side of the dynamic pier T are sold
// online; the settings take bookings without a checkout provider (allow_without_provider: the booking goes to the
// desk for approval and is paid by bank transfer with the manual driver), the terms and privacy texts are
// published and booking is enabled on the marina. The step is a no-op when the marina already has booking enabled
// or saved settings.

// seedOnlineBooking enables the demo online booking (see above) and counts the sellable resources in res.
func (a *App) seedOnlineBooking(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	m, err := a.Marinas.GetMarina(ctx, p, res.MarinaID)
	if err != nil {
		return err
	}
	set, err := a.OnlineConf.Get(ctx, p, res.MarinaID)
	if err != nil {
		return err
	}
	if m.BookingEnabled || set.Saved() {
		return nil
	}
	groups, err := a.Harbor.ListGroups(ctx, p, res.MarinaID)
	if err != nil {
		return err
	}
	byCode := map[string]string{}
	for _, g := range groups {
		byCode[g.Code] = g.ID
	}
	if byCode["TRANSITI"] == "" {
		return nil // not the demo harbour: leave the marina alone
	}
	sellable, err := a.markDemoSellable(ctx, p, res.MarinaID, byCode)
	if err != nil {
		return fmt.Errorf("sellable: %w", err)
	}
	res.OnlineSellable = sellable
	if g := byCode["PONT-C"]; g != "" {
		if _, err := a.OnlineConf.UpdateQuotas(ctx, p, res.MarinaID, []onlineconf.QuotaSet{{GroupID: g, Kind: store.QuotaPercent, Value: 25}}); err != nil {
			return fmt.Errorf("quotas: %w", err)
		}
	}
	s := onlineconf.Defaults()
	s.AllowWithoutProvider = true
	s.PaymentMode, s.DepositBP = onlineconf.PaymentDeposit, 3000
	s.RequiredFields = onlineconf.RequiredFields{Documents: []string{}, Phone: true, DocumentsLater: true}
	s.ArrivalInfo = onlineconf.ArrivalInfo{
		VHFChannel: "9", Phone: "+39 0771 000000",
		Text: onlineconf.LongText{
			It: "Chiamate la torre sul canale 9 VHF prima dell'imboccatura: l'ormeggiatore vi accompagna al posto.",
			En: "Call the tower on VHF channel 9 before the harbour mouth: the dock staff will guide you to the berth.",
		},
	}
	if _, err := a.OnlineConf.Update(ctx, p, res.MarinaID, 0, s); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	for _, t := range []struct{ kind, it, en string }{
		{
			"terms",
			"Condizioni generali di ormeggio in transito di Marina Demo (dati dimostrativi): l'ormeggio è assegnato dal porto in base alle dimensioni dichiarate; l'arrivo è tra le 12:00 e le 20:00; la cancellazione segue le fasce indicate nella prenotazione.",
			"General terms for transit moorings at Marina Demo (demo data): the berth is assigned by the marina from the declared dimensions; arrival is between 12:00 and 20:00; cancellations follow the tiers shown in the booking.",
		},
		{
			"privacy",
			"Informativa privacy (dati dimostrativi): Marina Demo tratta i dati della prenotazione solo per gestire l'ormeggio e gli adempimenti di legge.",
			"Privacy notice (demo data): Marina Demo processes booking data only to manage the mooring and its legal obligations.",
		},
	} {
		if _, err := a.OnlineConf.PublishText(ctx, p, res.MarinaID, t.kind, onlineconf.LongText{It: t.it, En: t.en}); err != nil {
			return fmt.Errorf("text %s: %w", t.kind, err)
		}
	}
	on := true
	if _, err := a.Marinas.UpdateMarina(ctx, p, res.MarinaID, m.Version, marina.MarinaPatch{BookingEnabled: &on}); err != nil {
		return fmt.Errorf("enable: %w", err)
	}
	return nil
}

// markDemoSellable flags the online resources on the water map (one save of the harbour document).
func (a *App) markDemoSellable(ctx context.Context, p *auth.Principal, marinaID string, groups map[string]string) (int, error) {
	maps, err := a.Harbor.ListMaps(ctx, p, marinaID)
	if err != nil {
		return 0, err
	}
	for _, sm := range maps {
		if sm.Kind != "water" {
			continue
		}
		d, err := a.Harbor.GetMap(ctx, p, marinaID, sm.ID)
		if err != nil {
			return 0, err
		}
		req := d.Request()
		n := 0
		for i := range req.Berths {
			b := &req.Berths[i]
			if b.MooringType == "buoy" || !b.Active {
				continue
			}
			if slices.Contains(b.GroupIDs, groups["TRANSITI"]) || slices.Contains(b.GroupIDs, groups["PONT-C"]) {
				b.OnlineSellable = true
				n++
			}
		}
		for i := range req.DynamicAreas {
			if da := &req.DynamicAreas[i]; da.Code == "T-FIANCO" && da.Active {
				da.OnlineSellable = true
				n++
			}
		}
		if n == 0 {
			continue
		}
		if _, err := a.Harbor.SaveMap(ctx, p, marinaID, sm.ID, d.Version, req); err != nil {
			return 0, err
		}
		return n, nil
	}
	return 0, nil
}
