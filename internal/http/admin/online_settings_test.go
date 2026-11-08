//go:build integration

package admin_test

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/store" //nolint:depguard // test setup: fixtures and assertions read the database directly
)

func onlineSettingsBody() map[string]any {
	return map[string]any{
		"rules": map[string]any{
			"min_nights": 2, "max_nights": 14, "min_advance_hours": 4, "max_advance_days": 200,
			"arrival_from": "13:00", "arrival_to": "19:30", "closed_weekdays": []int{1},
		},
		"payment_mode": "deposit", "deposit_bp": 2500,
		"cancellation_tiers": []map[string]int{{"min_days_before": 0, "refund_bp": 0}, {"min_days_before": 10, "refund_bp": 10000}},
		"refund_as":          "refund",
		"approval":           map[string]any{"loa_over_cm": 2000, "group_ids": []string{}, "always": false, "hold_hours": 24},
		"required_fields":    map[string]any{"documents": []string{"insurance_policy"}, "people_on_board": true, "phone": false, "documents_later": false},
		"reveal_berth":       "never", "late_payment_recovery": true, "allow_without_provider": false, "max_live_holds": 20,
		"arrival_info": map[string]any{"vhf_channel": "9", "rules_url": "https://marina.example/regole", "text": map[string]string{"it": "Chiamare al canale 9"}},
		"theme_preset": "default", "hide_header_in_embed": true, "show_feet": false,
	}
}

func TestOnlineSettingsHandlers_IfMatchAndValidation(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	var s api.OnlineBookingSettings
	r := c.expect(t, 200, "", "GET", M+"/online-booking/settings", nil, &s)
	if s.Saved || s.Version != 0 || s.Rules.MinNights != 1 || s.PaymentMode != "full" || len(s.CancellationTiers) != 3 ||
		r.header.Get("ETag") != etag(0) || s.EmbedOrigins == nil {
		t.Fatalf("defaults %+v %q", s, r.header.Get("ETag"))
	}
	body := onlineSettingsBody()
	c.expect(t, 428, "precondition_required", "PUT", M+"/online-booking/settings", body, nil)
	c.expect(t, 412, "version_conflict", "PUT", M+"/online-booking/settings", body, nil, "If-Match", etag(3))
	ps.dock.expect(t, 403, "forbidden", "PUT", M+"/online-booking/settings", body, nil, "If-Match", etag(0))
	ps.other.expect(t, 404, "not_found", "GET", M+"/online-booking/settings", nil, nil)
	ps.other.expect(t, 404, "not_found", "PUT", M+"/online-booking/settings", body, nil, "If-Match", etag(0))

	for name, mutate := range map[string]func(b map[string]any){
		"min above max": func(b map[string]any) { b["rules"].(map[string]any)["min_nights"] = 20 },
		"bad time":      func(b map[string]any) { b["rules"].(map[string]any)["arrival_from"] = "25:00" },
		"hold too long": func(b map[string]any) { b["approval"].(map[string]any)["hold_hours"] = 200 },
		"unknown group": func(b map[string]any) { b["approval"].(map[string]any)["group_ids"] = []string{store.NewID()} },
		"bad document":  func(b map[string]any) { b["required_fields"].(map[string]any)["documents"] = []string{"passport"} },
		"no tiers":      func(b map[string]any) { b["cancellation_tiers"] = []any{} },
		"tiers grow": func(b map[string]any) {
			b["cancellation_tiers"] = []map[string]int{{"min_days_before": 10, "refund_bp": 1000}, {"min_days_before": 2, "refund_bp": 5000}}
		},
		"bad url": func(b map[string]any) { b["arrival_info"].(map[string]any)["rules_url"] = "javascript:alert(1)" },
	} {
		b := onlineSettingsBody()
		mutate(b)
		r := c.do(t, "PUT", M+"/online-booking/settings", b, "If-Match", etag(0))
		if r.status != 400 || r.problem() != "validation_failed" {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
	}

	r = c.expect(t, 200, "", "PUT", M+"/online-booking/settings", body, &s, "If-Match", etag(0))
	if !s.Saved || s.Version != 1 || r.header.Get("ETag") != etag(1) || s.Rules.MinNights != 2 || s.DepositBp != 2500 ||
		s.CancellationTiers[0].MinDaysBefore != 10 || s.Approval.LoaOverCm == nil || *s.Approval.LoaOverCm != 2000 ||
		s.ArrivalInfo == nil || s.ArrivalInfo.Text == nil || s.ArrivalInfo.Text.It != "Chiamare al canale 9" || s.ShowFeet ||
		len(s.Rules.ClosedWeekdays) != 1 || s.Rules.ClosedWeekdays[0] != 1 {
		t.Fatalf("saved %+v", s)
	}
	// Creating again with If-Match 0 is a conflict; the stored version moves on.
	c.expect(t, 412, "version_conflict", "PUT", M+"/online-booking/settings", body, nil, "If-Match", etag(0))
	body["payment_mode"] = "full"
	c.expect(t, 200, "", "PUT", M+"/online-booking/settings", body, &s, "If-Match", etag(1))
	if s.Version != 2 || s.PaymentMode != "full" {
		t.Fatalf("second save %+v", s)
	}
	c.expect(t, 200, "", "GET", M+"/online-booking/settings", nil, &s)
	if s.Version != 2 || s.Rules.MaxNights != 14 {
		t.Fatalf("reload %+v", s)
	}
}

func TestBookingTextsHandlers_Immutable(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	var list api.BookingTextList
	c.expect(t, 200, "", "GET", M+"/online-booking/texts", nil, &list)
	if len(list.Items) != 0 {
		t.Fatalf("texts %+v", list)
	}
	var t1, t2, p1 api.BookingText
	c.expect(t, 201, "", "POST", M+"/online-booking/texts", map[string]any{"kind": "terms", "texts": map[string]string{"it": "Condizioni v1"}}, &t1)
	c.expect(t, 201, "", "POST", M+"/online-booking/texts", map[string]any{"kind": "terms", "texts": map[string]string{"it": "Condizioni v2", "en": "Terms v2"}}, &t2)
	c.expect(t, 201, "", "POST", M+"/online-booking/texts", map[string]any{"kind": "privacy", "texts": map[string]string{"it": "Privacy"}}, &p1)
	if t1.Version != 1 || t2.Version != 2 || p1.Version != 1 || len(t1.Sha256) != 64 || t1.Sha256 == t2.Sha256 ||
		t2.PublishedBy == nil || t2.PublishedBy.Name != "Owner" || t2.Texts.En == nil || *t2.Texts.En != "Terms v2" {
		t.Fatalf("published %+v %+v %+v", t1, t2, p1)
	}
	c.expect(t, 400, "validation_failed", "POST", M+"/online-booking/texts", map[string]any{"kind": "terms", "texts": map[string]string{"it": "  "}}, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/online-booking/texts", map[string]any{"kind": "cookies", "texts": map[string]string{"it": "x"}}, nil)
	ps.dock.expect(t, 403, "forbidden", "POST", M+"/online-booking/texts", map[string]any{"kind": "terms", "texts": map[string]string{"it": "x"}}, nil)
	ps.other.expect(t, 404, "not_found", "GET", M+"/online-booking/texts", nil, nil)
	ps.other.expect(t, 404, "not_found", "POST", M+"/online-booking/texts", map[string]any{"kind": "terms", "texts": map[string]string{"it": "x"}}, nil)

	c.expect(t, 200, "", "GET", M+"/online-booking/texts?kind=terms", nil, &list)
	if len(list.Items) != 2 || list.Items[0].Version != 2 {
		t.Fatalf("terms %+v", list)
	}
	// The trigger refuses any change of a published text.
	_, err := ps.s.app.DB.NewRaw("UPDATE booking_texts SET texts = ? WHERE id = ?", `{"it":"changed"}`, t1.Id).Exec(context.Background())
	if err == nil {
		t.Fatal("published text changed")
	}
}

func TestOnlineQuotasHandlers(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	ctx := context.Background()
	now := store.UTC(time.Now())
	groups := []*store.BerthGroup{
		{ID: store.NewID(), MarinaID: ps.marinaID, Code: "T", Name: store.JSON(`{"it":"Transiti"}`), Kind: "custom", Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: store.NewID(), MarinaID: ps.marinaID, Code: "A", Name: store.JSON(`{"it":"Area"}`), Kind: "custom", Version: 1, CreatedAt: now, UpdatedAt: now},
	}
	for _, g := range groups {
		if _, err := ps.s.app.DB.NewInsert().Model(g).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var mapID string
	if err := ps.s.app.DB.NewRaw("SELECT id FROM maps WHERE marina_id = ? LIMIT 1", ps.marinaID).Scan(ctx, &mapID); err != nil {
		// A marina always has a map: without one, membership rows cannot be added.
		t.Skipf("no map: %v", err)
	}
	if _, err := ps.s.app.DB.NewRaw(`INSERT INTO berth_group_members (id, marina_id, group_id, map_id, resource_type, resource_id, created_at)
		VALUES (?, ?, ?, ?, 'dynamic_area', ?, ?)`, store.NewID(), ps.marinaID, groups[1].ID, mapID, store.NewID(), now).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	var list api.OnlineQuotaList
	c.expect(t, 200, "", "GET", M+"/online-booking/quotas", nil, &list)
	if len(list.Items) != 2 || list.Items[0].Kind != "none" {
		t.Fatalf("quotas %+v", list)
	}
	set := func(items ...map[string]any) map[string]any { return map[string]any{"items": items} }
	c.expect(t, 400, "validation_failed", "PUT", M+"/online-booking/quotas", set(map[string]any{"group_id": groups[1].ID, "kind": "percent", "value": 50}), nil)
	c.expect(t, 400, "validation_failed", "PUT", M+"/online-booking/quotas", set(map[string]any{"group_id": groups[0].ID, "kind": "percent", "value": 150}), nil)
	c.expect(t, 400, "validation_failed", "PUT", M+"/online-booking/quotas", set(map[string]any{"group_id": store.NewID(), "kind": "count", "value": 1}), nil)
	ps.dock.expect(t, 403, "forbidden", "PUT", M+"/online-booking/quotas", set(), nil)
	ps.other.expect(t, 404, "not_found", "GET", M+"/online-booking/quotas", nil, nil)
	ps.other.expect(t, 404, "not_found", "PUT", M+"/online-booking/quotas", set(), nil)

	// A sold night keeps its cap at least at what is used.
	night := time.Now().UTC().AddDate(0, 0, 10).Format("2006-01-02")
	if _, err := ps.s.app.DB.NewRaw("INSERT INTO online_quota_usage (marina_id, group_id, night, cap, used, updated_at) VALUES (?, ?, ?, 5, 3, ?)",
		ps.marinaID, groups[0].ID, night, now).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	c.expect(t, 200, "", "PUT", M+"/online-booking/quotas", set(
		map[string]any{"group_id": groups[0].ID, "kind": "count", "value": 1},
		map[string]any{"group_id": groups[1].ID, "kind": "count", "value": 2},
	), &list)
	byID := map[string]api.OnlineQuota{}
	for _, q := range list.Items {
		byID[q.GroupId] = q
	}
	if byID[groups[0].ID].Kind != "count" || byID[groups[0].ID].Value != 1 || !byID[groups[1].ID].HasAreas {
		t.Fatalf("updated %+v", list)
	}

	ps.other.expect(t, 404, "not_found", "GET", M+"/online-booking/quotas/usage", nil, nil)
	c.expect(t, 400, "validation_failed", "GET", M+"/online-booking/quotas/usage", nil, nil)
	c.expect(t, 400, "validation_failed", "GET", M+"/online-booking/quotas/usage?from=2027-01-01&to=2027-06-30", nil, nil)
	c.expect(t, 400, "validation_failed", "GET", M+"/online-booking/quotas/usage?from=bad&to=2027-01-02", nil, nil)
	var usage api.OnlineQuotaUsage
	c.expect(t, 200, "", "GET", M+"/online-booking/quotas/usage?from="+night+"&to="+night, nil, &usage)
	if len(usage.Groups) != 2 {
		t.Fatalf("usage %+v", usage)
	}
	for _, g := range usage.Groups {
		if g.GroupId == groups[0].ID && (len(g.Nights) != 1 || g.Nights[0].Cap != 3 || g.Nights[0].Used != 3) {
			t.Fatalf("cap below used: %+v", g)
		}
	}
}

func TestDocumentsHandlers_Verify(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	var cu struct {
		Customer api.Customer `json:"customer"`
	}
	c.expect(t, 201, "", "POST", M+"/customers", map[string]any{"kind": "person", "first_name": "Mario", "last_name": "Rossi"}, &cu)
	var doc struct {
		ID string `json:"id"`
	}
	c.expect(t, 201, "", "POST", M+"/documents", map[string]any{"owner_type": "customer", "customer_id": cu.Customer.Id, "type": "insurance_policy"}, &doc)
	var v api.DocumentVerification
	c.expect(t, 200, "", "POST", M+"/documents/"+doc.ID+"/verify", map[string]any{"verified": true}, &v)
	if !v.Verified || v.VerifiedAt == nil || v.VerifiedBy == nil || v.VerifiedBy.Name != "Owner" {
		t.Fatalf("verified %+v", v)
	}
	v = api.DocumentVerification{}
	c.expect(t, 200, "", "POST", M+"/documents/"+doc.ID+"/verify", map[string]any{"verified": false}, &v)
	if v.Verified || v.VerifiedAt != nil || v.VerifiedBy != nil {
		t.Fatalf("unverified %+v", v)
	}
	c.expect(t, 404, "not_found", "POST", M+"/documents/"+store.NewID()+"/verify", map[string]any{"verified": true}, nil)
	c.expect(t, 404, "not_found", "POST", M+"/documents/not-an-id/verify", map[string]any{"verified": true}, nil)
	ps.other.expect(t, 404, "not_found", "POST", M+"/documents/"+doc.ID+"/verify", map[string]any{"verified": true}, nil)
	ps.other.expect(t, 404, "not_found", "POST", ps.otherM+"/documents/"+doc.ID+"/verify", map[string]any{"verified": true}, nil)
	ps.dock.expect(t, 403, "forbidden", "POST", M+"/documents/"+doc.ID+"/verify", map[string]any{"verified": true}, nil)
}

// Desk list, detail and channels: another marina is 404 before anything else.
func TestOnlineDeskHandlers_CrossMarina(t *testing.T) {
	ps := newPricingSetup(t)
	M, id := ps.M, store.NewID()
	o := ps.other
	o.expect(t, 404, "not_found", "GET", M+"/online-bookings", nil, nil)
	o.expect(t, 404, "not_found", "GET", M+"/online-bookings/"+id, nil, nil)
	o.expect(t, 404, "not_found", "POST", M+"/online-bookings/"+id+"/approve", nil, nil, "If-Match", etag(1))
	o.expect(t, 404, "not_found", "POST", M+"/online-bookings/"+id+"/reject", map[string]any{"reason": "x", "notify": false}, nil, "If-Match", etag(1))
	o.expect(t, 404, "not_found", "POST", M+"/online-bookings/"+id+"/payment-link", nil, nil, "If-Match", etag(1))
	o.expect(t, 404, "not_found", "GET", M+"/booking-channels", nil, nil)
	o.expect(t, 404, "not_found", "PUT", M+"/booking-channels/fake", map[string]any{"enabled": true, "push_enabled": false, "category_map": map[string]string{}}, nil, "If-Match", etag(0))
	o.expect(t, 404, "not_found", "POST", M+"/booking-channels/fake/test", nil, nil)
	o.expect(t, 404, "not_found", "POST", M+"/booking-channels/fake/sync", nil, nil)
	o.expect(t, 404, "not_found", "GET", M+"/channel-reservations", nil, nil)
	// Invalid query dates: another marina still 404, the own marina 400.
	o.expect(t, 404, "not_found", "GET", M+"/online-bookings?from=bad", nil, nil)
	ps.owner.expect(t, 400, "validation_failed", "GET", M+"/online-bookings?from=bad", nil, nil)
	// Approval without If-Match.
	ps.owner.expect(t, 428, "precondition_required", "POST", M+"/online-bookings/"+id+"/approve", nil, nil)
}
