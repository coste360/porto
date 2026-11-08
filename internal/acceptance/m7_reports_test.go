//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/csv"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/internal/xlsx"
	"github.com/coste360/porto/pkg/ext"
)

// m7Now is the fixed instant of the M7 acceptance (late summer: occupancy, transits, billing history).
var m7Now = time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC)

const m7Password = "demo-password-123"

// m7Harness is the API harness on a fake clock with the demo marina seeded.
type m7Harness struct {
	*harness
	marina string
	today  calendar.Date
}

func newM7Harness(t *testing.T, db *store.DB) *m7Harness {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	vars := map[string]string{
		"PORTO_SECRET_KEY": base64.StdEncoding.EncodeToString(key), "PORTO_BASE_URL": "http://localhost:8080",
		"PORTO_DATA_DIR": t.TempDir(), "PORTO_DEV": "true",
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	a, err := app.NewWithOptions(context.Background(), cfg, "test", ext.Extensions{}, slog.New(slog.NewJSONHandler(logs, nil)),
		app.Options{DB: db, Argon2: auth.TestArgon2Params, Transport: &fixture.Mailbox{}, Clock: clock.NewFake(m7Now)})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	res, err := a.SeedDemo(fixture.Ctx(), m7Password)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Router)
	t.Cleanup(srv.Close)
	loc, _ := calendar.LoadLocation("Europe/Rome")
	return &m7Harness{harness: &harness{t: t, app: a, srv: srv, logs: logs}, marina: res.MarinaID, today: calendar.In(m7Now, loc)}
}

// login returns a cookie client logged in as the demo user of role.
func (h *m7Harness) login(t *testing.T, role auth.Role) *client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &client{h: h.harness, http: &http.Client{Jar: clockJar{jar}, Timeout: 60 * time.Second}}
	c.expect(t, http.StatusOK, "POST", A+"/auth/login", map[string]string{"email": string(role) + "@" + app.DemoEmailDomain, "password": m7Password})
	return c
}

// clockJar keeps the session cookies whatever their expiry: the server runs on the fake clock (m7Now), the
// cookie jar on the wall clock.
type clockJar struct{ http.CookieJar }

func (j clockJar) SetCookies(u *url.URL, cs []*http.Cookie) {
	for _, c := range cs {
		c.Expires, c.MaxAge = time.Time{}, 0
	}
	j.CookieJar.SetCookies(u, cs)
}

func m7num(m map[string]any, k string) int64 {
	v, _ := m[k].(float64)
	return int64(v)
}

func m7Periods(today calendar.Date) map[string][2]calendar.Date {
	return map[string][2]calendar.Date{
		"mtd":      {calendar.New(today.Year(), today.Month(), 1), today},
		"ytd":      {calendar.New(today.Year(), time.January, 1), today},
		"last_12m": {today.AddDays(-364), today},
	}
}

// drillURL is the report URL of a tile drill with a period.
func (h *m7Harness) drillURL(drill map[string]any, from, to calendar.Date, extra url.Values) string {
	v := url.Values{}
	params, _ := drill["params"].(map[string]any)
	for k, x := range params {
		v.Set(k, x.(string))
	}
	// A tile with its own period (M9 storage_exits_blocked) carries from and to in its drill.
	if v.Get("from") == "" || v.Get("to") == "" {
		v.Set("from", from.String())
		v.Set("to", to.String())
	}
	for k, xs := range extra {
		v[k] = xs
	}
	return A + "/marinas/" + h.marina + "/reports/" + drill["report"].(string) + "?" + v.Encode()
}

// recordSum reads every page of the drill report and recomputes the tile from the records: Σ value, or Σ
// numerator and Σ denominator.
func (h *m7Harness) recordSum(t *testing.T, c *client, key string, drill map[string]any, from, to calendar.Date) (value, numer, denom, total int64) {
	t.Helper()
	for page := 1; ; page++ {
		body := c.expect(t, http.StatusOK, "GET", h.drillURL(drill, from, to, url.Values{"page": {strconv.Itoa(page)}, "page_size": {"1000"}}), nil)
		recs, _ := body["records"].([]any)
		for _, x := range recs {
			r := x.(map[string]any)
			switch key {
			case "production":
				value += m7num(r, "competence_cents")
			case "transit_avg_night":
				if r["kind"] == "transit" {
					numer += m7num(r, "competence_cents")
					denom += m7num(r, "nights_in_period")
				}
			case "charged":
				value += m7num(r, "gross_cents")
			case "collected":
				value += m7num(r, "amount_cents")
			case "receivables_open":
				value += m7num(r, "residual_cents")
			case "receivables_overdue":
				if r["overdue"] == true {
					value += m7num(r, "residual_cents")
				}
			case "occupancy":
				numer += m7num(r, "occupied_units")
				denom += m7num(r, "available_units")
			case "efficiency":
				numer += m7num(r, "vessel_units")
				denom += m7num(r, "resource_units")
			case "berth_revenue_per_m":
				numer += m7num(r, "revenue_cents")
				denom += m7num(r, "length_cm")
			case "new_customers":
				value += m7num(r, "new")
			case "renewal_rate":
				numer += m7num(r, "renewed")
				denom++
			case "vessels_hosted":
				value++
			case "vessels_avg_loa":
				numer += m7num(r, "loa_cm")
				denom++
			default:
				m9RecordSum(key, r, &value, &numer)
				m10RecordSum(key, r, &value)
			}
		}
		m9TotalsDenom(key, body, &denom)
		pg, _ := body["page"].(map[string]any)
		total = m7num(pg, "total_records")
		if int64(page)*1000 >= total {
			return value, numer, denom, total
		}
	}
}

// m7ratio is num × scale / den half-even (the tests recompute the bp of the records with the same rule).
func m7ratio(n, d, scale int64) (int64, bool) {
	if d == 0 {
		return 0, false
	}
	x := n * scale
	q, r := x/d, x%d
	if r < 0 {
		r = -r
	}
	if 2*r > m7abs(d) || (2*r == m7abs(d) && q%2 != 0) {
		if (x < 0) != (d < 0) {
			q--
		} else {
			q++
		}
	}
	return q, true
}

func m7abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

var tileScale = map[string]int64{"occupancy": 10000, "efficiency": 10000, "renewal_rate": 10000, "berth_revenue_per_m": 100, "transit_avg_night": 1, "vessels_avg_loa": 1}

// checkTile asserts that a tile value (current or previous) equals the records of its drill report.
func (h *m7Harness) checkTile(t *testing.T, c *client, label, key string, val map[string]any, drill map[string]any, from, to calendar.Date) {
	t.Helper()
	value, numer, denom, total := h.recordSum(t, c, key, drill, from, to)
	if scale, frac := tileScale[key]; frac {
		if m7num(val, "numerator") != numer || m7num(val, "denominator") != denom {
			t.Errorf("%s %s: records %d/%d != tile %v/%v", label, key, numer, denom, val["numerator"], val["denominator"])
		}
		want, ok := m7ratio(numer, denom, scale)
		if got, has := val["value"].(float64); ok != has || (ok && int64(got) != want) {
			t.Errorf("%s %s: value %v != recomputed %d (%v)", label, key, val["value"], want, ok)
		}
	} else if m7num(val, "value") != value {
		t.Errorf("%s %s: Σ records %d != tile %v", label, key, value, val["value"])
	}
	// The exports carry the same records: one CSV row per record plus the header and the Totale row.
	r := c.do(t, "GET", h.drillURL(drill, from, to, url.Values{"format": {"csv"}}), nil)
	if r.status != http.StatusOK {
		t.Errorf("%s %s csv: status %d", label, key, r.status)
		return
	}
	rd := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(r.body, []byte("\xEF\xBB\xBF"))))
	rd.Comma, rd.FieldsPerRecord = ';', -1
	rows, err := rd.ReadAll()
	if err != nil {
		t.Errorf("%s %s csv: %v", label, key, err)
		return
	}
	if int64(len(rows)) != total+2 || !strings.HasPrefix(rows[len(rows)-1][0], "Total") {
		t.Errorf("%s %s csv: %d rows for %d records", label, key, len(rows), total)
		return
	}
	col, summed := m7ExportColumn[key]
	if summed {
		if got, ok := m7ColumnSum(rows, col, ','); !ok || got != value {
			t.Errorf("%s %s csv: Σ %q = %d (%v), tile %d", label, key, col, got, ok, value)
		}
	}
	x := c.do(t, "GET", h.drillURL(drill, from, to, url.Values{"format": {"xlsx"}}), nil)
	if x.status != http.StatusOK || !bytes.HasPrefix(x.body, []byte("PK")) {
		t.Errorf("%s %s xlsx: status %d", label, key, x.status)
		return
	}
	sheet, err := xlsx.ReadFirstSheet(bytes.NewReader(x.body), int64(len(x.body)), xlsx.Limits{MaxUncompressed: 512 << 20, MaxCells: 10_000_000, MaxRatio: 1000, MaxParts: 100})
	if err != nil || int64(len(sheet)) != total+2 {
		t.Errorf("%s %s xlsx: %d rows for %d records (%v)", label, key, len(sheet), total, err)
		return
	}
	if summed {
		if got, ok := m7ColumnSum(sheet, col, '.'); !ok || got != value {
			t.Errorf("%s %s xlsx: Σ %q = %d (%v), tile %d", label, key, col, got, ok, value)
		}
	}
}

// m7ExportColumn is the Italian header of the export column whose records sum to a money tile.
var m7ExportColumn = map[string]string{
	"production": "Competenza", "charged": "Lordo", "collected": "Importo", "receivables_open": "Residuo",
}

// m7ColumnSum sums the cents of a column over the record rows (header first, Totale last) of an export; sep is
// the decimal separator ("1234,56" in the CSV, "1234.56" in the workbook).
func m7ColumnSum(rows [][]string, header string, sep byte) (int64, bool) {
	idx := slices.Index(rows[0], header)
	if idx < 0 {
		return 0, false
	}
	var sum int64
	for _, row := range rows[1 : len(rows)-1] {
		if idx >= len(row) || row[idx] == "" {
			continue
		}
		whole, frac, _ := strings.Cut(row[idx], string(sep))
		frac = (frac + "00")[:2]
		neg := strings.HasPrefix(whole, "-")
		n, err := strconv.ParseInt(strings.TrimPrefix(whole, "-")+frac, 10, 64)
		if err != nil {
			return 0, false
		}
		if neg {
			n = -n
		}
		sum += n
	}
	return sum, true
}

// TestAcceptance_M7_DashboardDrillDown (spec 32 M7, INV-62): on the seeded demo, for month to date, year to date and
// the last twelve months, every available tile of the dashboard and its previous value equal the records of its
// drill report read page by page through the API, and the CSV and Excel exports carry the same records.
func TestAcceptance_M7_DashboardDrillDown(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newM7Harness(t, db)
		owner := h.login(t, auth.RoleOwner)
		for name, pr := range m7Periods(h.today) {
			v := url.Values{"from": {pr[0].String()}, "to": {pr[1].String()}}
			dash := owner.expect(t, http.StatusOK, "GET", A+"/marinas/"+h.marina+"/dashboard?"+v.Encode(), nil)
			prevPeriod, _ := dash["previous_period"].(map[string]any)
			pf, _ := calendar.Parse(str(prevPeriod, "from"))
			pt, _ := calendar.Parse(str(prevPeriod, "to"))
			tiles, _ := dash["tiles"].([]any)
			if len(tiles) == 0 {
				t.Fatalf("%s: no tiles", name)
			}
			for _, x := range tiles {
				tile := x.(map[string]any)
				key := str(tile, "key")
				if tile["available"] != true {
					if str(tile, "reason") == "" {
						t.Errorf("%s %s: unavailable without reason", name, key)
					}
					continue
				}
				drill, _ := tile["drill"].(map[string]any)
				h.checkTile(t, owner, name, key, tile, drill, pr[0], pr[1])
				if prev, ok := tile["previous"].(map[string]any); ok {
					h.checkTile(t, owner, name+" previous", key, prev, drill, pf, pt)
				}
			}
		}
	})
}

// TestAcceptance_M7_Roles (spec 26.1): the office sees only the operational tiles, accounting every tile, the
// dock no dashboard.
func TestAcceptance_M7_Roles(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newM7Harness(t, db)
		money := map[string]bool{
			"production": true, "charged": true, "collected": true, "receivables_open": true,
			"receivables_overdue": true, "berth_revenue_per_m": true, "transit_avg_night": true, "new_customers": true, "renewal_rate": true,
		}
		keys := func(c *client) map[string]bool {
			d := c.expect(t, http.StatusOK, "GET", A+"/marinas/"+h.marina+"/dashboard", nil)
			out := map[string]bool{}
			for _, x := range d["tiles"].([]any) {
				tile := x.(map[string]any)
				if tile["available"] == true {
					out[str(tile, "key")] = true
				}
			}
			return out
		}
		office := keys(h.login(t, auth.RoleOffice))
		for k := range office {
			if money[k] {
				t.Errorf("office sees %s", k)
			}
		}
		if !office["occupancy"] || !office["vessels_hosted"] {
			t.Errorf("office misses operational tiles: %v", office)
		}
		acc := keys(h.login(t, auth.RoleAccounting))
		for k := range money {
			if !acc[k] {
				t.Errorf("accounting misses %s", k)
			}
		}
		dock := h.login(t, auth.RoleDock)
		if r := dock.do(t, "GET", A+"/marinas/"+h.marina+"/dashboard", nil); r.status != http.StatusForbidden {
			t.Errorf("dock dashboard: %d", r.status)
		}
		if r := dock.do(t, "GET", A+"/marinas/"+h.marina+"/reports/occupancy", nil); r.status != http.StatusForbidden {
			t.Errorf("dock report: %d", r.status)
		}
	})
}

// TestAcceptance_M7_Comparison: the series of a comparison sum to its current and previous values with both
// alignments.
func TestAcceptance_M7_Comparison(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newM7Harness(t, db)
		owner := h.login(t, auth.RoleOwner)
		from := calendar.New(h.today.Year(), time.January, 1)
		for _, ind := range []string{"production", "charged", "collected", "arrivals", "occupancy"} {
			for _, al := range []string{"date", "weekday"} {
				v := url.Values{"indicator": {ind}, "align": {al}, "from": {from.String()}, "to": {h.today.String()}}
				c := owner.expect(t, http.StatusOK, "GET", A+"/marinas/"+h.marina+"/reports/comparison?"+v.Encode(), nil)
				cur, prev := c["current"].(map[string]any), c["previous"].(map[string]any)
				var cn, cd, pn, pd, cv, pv int64
				for _, x := range c["series"].([]any) {
					p := x.(map[string]any)
					cn, cd = cn+m7num(p, "current_numerator"), cd+m7num(p, "current_denominator")
					pn, pd = pn+m7num(p, "previous_numerator"), pd+m7num(p, "previous_denominator")
					cv, pv = cv+m7num(p, "current"), pv+m7num(p, "previous")
				}
				if _, frac := tileScale[ind]; frac {
					if cn != m7num(cur, "numerator") || cd != m7num(cur, "denominator") || pn != m7num(prev, "numerator") || pd != m7num(prev, "denominator") {
						t.Errorf("%s %s: series %d/%d %d/%d != %v %v", ind, al, cn, cd, pn, pd, cur, prev)
					}
				} else if cv != m7num(cur, "value") || pv != m7num(prev, "value") {
					t.Errorf("%s %s: Σ series %d/%d != %v/%v", ind, al, cv, pv, cur["value"], prev["value"])
				}
			}
		}
		r := owner.do(t, "GET", A+"/marinas/"+h.marina+"/reports/comparison?indicator=receivables_open", nil)
		if r.status != http.StatusUnprocessableEntity || r.problemType() != "unknown_indicator" {
			t.Errorf("receivables comparison: %d %s", r.status, r.problemType())
		}
	})
}
