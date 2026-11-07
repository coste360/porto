package pricelist

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/qty"
)

// TestRapid_ProbeRoundTrip: an engine input (a matrix probe) turned into its request form, through JSON (the wire)
// and back gives the same input, so simulating a probe reaches the same Quote call as the matrix (INV-23).
func TestRapid_ProbeRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		arr := calendar.New(2027, 1, 1).AddDays(rapid.IntRange(0, 700).Draw(t, "arr"))
		in := pricing.Input{
			ContractType: rapid.SampledFrom(pricing.ContractTypes()).Draw(t, "ct"),
			Arrival:      arr, Departure: arr.AddDays(rapid.IntRange(1, 400).Draw(t, "n")),
			ItemID:   rapid.SampledFrom([]string{"", "01900000-0000-7000-8000-000000000001"}).Draw(t, "item"),
			Resource: pricing.Resource{Category: rapid.SampledFrom([]string{"", "L12"}).Draw(t, "cat")},
			Channel:  rapid.SampledFrom(pricing.Channels()).Draw(t, "ch"),
			QuotedOn: calendar.New(2026, 12, 1).AddDays(rapid.IntRange(0, 60).Draw(t, "q")),
		}
		if rapid.Bool().Draw(t, "vessel") {
			in.Vessel = &pricing.Vessel{LOA: geo.Cm(rapid.IntRange(1, 20000).Draw(t, "loa")), Beam: geo.Cm(rapid.IntRange(1, 20000).Draw(t, "beam")), HullType: "monohull_motor"}
		}
		if rapid.Bool().Draw(t, "dims") {
			in.Resource.Length, in.Resource.Width = geo.Cm(rapid.IntRange(1, 20000).Draw(t, "l")), geo.Cm(rapid.IntRange(1, 20000).Draw(t, "w"))
		}
		if rapid.Bool().Draw(t, "cust") {
			in.CustomerCategory = "member"
		}
		if rapid.Bool().Draw(t, "svc") {
			in.Services = []pricing.ServiceRequest{{ItemID: "s", Qty: qty.Milli(rapid.IntRange(1, 5000).Draw(t, "qty"))}}
		}
		raw, err := json.Marshal(probeRequest("list", in))
		if err != nil {
			t.Fatal(err)
		}
		var req QuoteRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatal(err)
		}
		if got := inputFromRequest(req); !reflect.DeepEqual(got, in) {
			t.Fatalf("round trip:\n got %+v\nwant %+v", got, in)
		}
	})
}

// TestQuote_UsesInjectedQuoter: the service prices through its quoter, so the engine can be replaced in tests.
func TestQuote_UsesInjectedQuoter(t *testing.T) {
	s := NewService(Deps{})
	called := false
	s.quote = func(pricing.Input, pricing.Catalog) (pricing.Breakdown, error) {
		called = true
		return pricing.Breakdown{Total: 1}, nil
	}
	b, err := s.quote(pricing.Input{}, pricing.Catalog{})
	if err != nil || !called || b.Total != 1 {
		t.Fatal("quoter not used")
	}
}

func TestCheckRequest_Table(t *testing.T) {
	day := calendar.New(2027, 7, 1)
	for _, tc := range []struct {
		name  string
		req   QuoteRequest
		field string
	}{
		{"ok", QuoteRequest{ContractType: pricing.ContractTransit, Arrival: day, Departure: day.AddDays(1)}, ""},
		{"contract type", QuoteRequest{ContractType: "boat", Arrival: day, Departure: day.AddDays(1)}, "contract_type"},
		{"arrival", QuoteRequest{ContractType: pricing.ContractTransit, Departure: day}, "arrival"},
		{"channel", QuoteRequest{ContractType: pricing.ContractTransit, Arrival: day, Departure: day, Channel: "fax"}, "channel"},
		{"customer category", QuoteRequest{ContractType: pricing.ContractTransit, Arrival: day, Departure: day, CustomerCategory: "vip"}, "customer_category"},
		{"vessel", QuoteRequest{ContractType: pricing.ContractTransit, Arrival: day, Departure: day, Vessel: &SimVessel{LOA: 0, Beam: 1}}, "vessel"},
		{"qty", QuoteRequest{ContractType: pricing.ContractTransit, Arrival: day, Departure: day, Services: []pricing.ServiceRequest{{Qty: -1}}}, "services[0].qty_milli"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkRequest(&tc.req)
			if tc.field == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !isFieldErr(err, tc.field) {
				t.Fatalf("want %s, got %v", tc.field, err)
			}
		})
	}
}

func isFieldErr(err error, field string) bool {
	var v *auth.ValidationError
	if !errors.As(err, &v) {
		return false
	}
	for _, f := range v.Fields {
		if f.Field == field {
			return true
		}
	}
	return false
}
