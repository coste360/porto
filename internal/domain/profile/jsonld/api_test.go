package jsonld

import (
	"encoding/json"
	"os" //nolint:depguard // the test reads the i18n catalogues of the web workspace (not the builder)
	"path/filepath"
	"strings"
	"testing"
)

func ptr(v int64) *int64 { return &v }

func TestJSONLD_FormatE7(t *testing.T) {
	cases := map[int64]string{
		437101234: "43.7101234", 104012345: "10.4012345", 0: "0.0000000", 1: "0.0000001",
		-5000000: "-0.5000000", -437101234: "-43.7101234", -1: "-0.0000001", 1800000000: "180.0000000",
		-1800000000: "-180.0000000", -9223372036854775808: "-922337203685.4775808",
	}
	for in, want := range cases {
		if got := FormatE7(in); got != want {
			t.Errorf("FormatE7(%d) = %q, want %q", in, got, want)
		}
	}
}

func goldenProfile() Profile {
	return Profile{
		Slug: "porto-demo", Name: "Porto Demo", Description: "Un porto", Phone: "+39 050 000000", Email: "info@example.org",
		Website: "https://example.org", Street: "Via del Porto 1", PostalCode: "56100", City: "Pisa", Province: "PI",
		Country: "IT", LatE7: ptr(437101234), LonE7: ptr(-104012345),
		OfficeHours: []Hours{{Weekday: 1, From: "08:00", To: "19:00"}, {Weekday: 9, From: "x", To: "y"}},
		Amenities:   map[string]bool{"wifi": true, "water": true, "crane": false, "unknown": true},
		PhotoIDs:    []string{"f1"},
	}
}

func TestJSONLD_Golden(t *testing.T) {
	cases := []struct {
		name, lang string
		booking    bool
		want       string
	}{
		{"it without booking", "it", false, `{"@context":"https://schema.org","@id":"https://porto.example/p/porto-demo#marina",` +
			`"@type":["Marina","Place"],"address":{"@type":"PostalAddress","addressCountry":"IT","addressLocality":"Pisa",` +
			`"addressRegion":"PI","postalCode":"56100","streetAddress":"Via del Porto 1"},` +
			`"amenityFeature":[{"@type":"LocationFeatureSpecification","name":"Acqua","value":true},` +
			`{"@type":"LocationFeatureSpecification","name":"Wi-Fi","value":true}],"description":"Un porto",` +
			`"email":"info@example.org","geo":{"@type":"GeoCoordinates","latitude":"43.7101234","longitude":"-10.4012345"},` +
			`"image":["https://porto.example/api/public/v1/porto-demo/profile/photos/f1"],"inLanguage":"it","name":"Porto Demo",` +
			`"openingHoursSpecification":[{"@type":"OpeningHoursSpecification","closes":"19:00",` +
			`"dayOfWeek":"https://schema.org/Monday","opens":"08:00"}],"sameAs":["https://example.org"],` +
			`"telephone":"+39 050 000000","url":"https://porto.example/p/porto-demo"}`},
		{"en with booking", "en", true, `{"@context":"https://schema.org","@id":"https://porto.example/p/porto-demo#marina",` +
			`"@type":["Marina","Place"],"address":{"@type":"PostalAddress","addressCountry":"IT","addressLocality":"Pisa",` +
			`"addressRegion":"PI","postalCode":"56100","streetAddress":"Via del Porto 1"},` +
			`"amenityFeature":[{"@type":"LocationFeatureSpecification","name":"Water","value":true},` +
			`{"@type":"LocationFeatureSpecification","name":"Wi-Fi","value":true}],"description":"Un porto",` +
			`"email":"info@example.org","geo":{"@type":"GeoCoordinates","latitude":"43.7101234","longitude":"-10.4012345"},` +
			`"image":["https://porto.example/api/public/v1/porto-demo/profile/photos/f1"],"inLanguage":"en","name":"Porto Demo",` +
			`"openingHoursSpecification":[{"@type":"OpeningHoursSpecification","closes":"19:00",` +
			`"dayOfWeek":"https://schema.org/Monday","opens":"08:00"}],` +
			`"potentialAction":{"@type":"ReserveAction","target":"https://porto.example/b/porto-demo"},` +
			`"sameAs":["https://example.org"],"telephone":"+39 050 000000","url":"https://porto.example/p/porto-demo"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := goldenProfile()
			p.BookingEnabled = tc.booking
			got, err := Marshal(Build(p, tc.lang, "https://porto.example/"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestJSONLD_Minimal(t *testing.T) {
	got, err := Marshal(Build(Profile{Slug: "x-y", Name: "N"}, "fr", "http://h"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"@context":"https://schema.org","@id":"http://h/p/x-y#marina","@type":["Marina","Place"],"name":"N","url":"http://h/p/x-y"}`
	if string(got) != want {
		t.Fatalf("got %s", got)
	}
}

func TestJSONLD_EscapesScript(t *testing.T) {
	p := Profile{Slug: "porto-demo", Name: "</script><script>alert(1)</script>", Description: "a & b   <!--"}
	got, err := Marshal(Build(p, "it", "https://h"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(got))
	for _, bad := range []string{"</script", "<script", "<!--", "&", " "} {
		if strings.Contains(s, bad) {
			t.Fatalf("output contains %q: %s", bad, got)
		}
	}
	var back map[string]any
	if err := json.Unmarshal(got, &back); err != nil || back["name"] != p.Name {
		t.Fatalf("round trip failed: %v %v", err, back["name"])
	}
}

func TestJSONLD_LabelsMatchI18n(t *testing.T) {
	for _, lang := range []string{"it", "en"} {
		path := filepath.Join("..", "..", "..", "..", "web", "shared", "src", "i18n", "locales", lang, "publicProfile.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cat struct {
			Services map[string]string `json:"services"`
		}
		if err := json.Unmarshal(raw, &cat); err != nil {
			t.Fatal(err)
		}
		for _, key := range AmenityKeys() {
			if got, want := cat.Services[key], AmenityLabel(key, lang); got != want {
				t.Errorf("%s services.%s = %q, Go table %q", lang, key, got, want)
			}
		}
	}
	if AmenityLabel("nope", "it") != "" {
		t.Error("unknown key must have no label")
	}
}
