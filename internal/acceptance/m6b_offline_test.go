//go:build integration

package acceptance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M6b offline replays (m6b-plan §4.4, §5, §10.1): the full HTTP stack (router, Enforce, OfflineGuard,
// Idempotency, handler) with Porto-Offline-Queued-At, on SQLite and PostgreSQL.

const offlineHeader = "Porto-Offline-Queued-At"

// offlineWorld is a desk world at 2027-06-05 10:00 UTC with a confirmed transit arriving today (vessel A, charge
// open) and an open cash session.
type offlineWorld struct {
	*deskWorld
	vesselA, transitA, allocA, berthA, chargeA, customerA string
	residualA                                             int64
}

func newOfflineWorld(t *testing.T, db *store.DB) *offlineWorld {
	t.Helper()
	w := &offlineWorld{deskWorld: newDeskWorld(t, db, 4)}
	o, M := w.owner, w.M
	w.vesselA = w.vessel(t, "Libeccio", 1134, 380)
	rossi := w.customer(t, "Rossi")
	w.customerA = rossi
	w.berthA = w.harbour.Berths[0]
	tr := o.expect(t, 201, "POST", M+"/transits", map[string]any{
		"vessel_id": w.vesselA, "customer_id": rossi, "arrival": "2027-06-05", "departure": "2027-06-08",
		"placements": []any{berthPlacement(w.berthA, "2027-06-05", "2027-06-07")}, "confirm": map[string]any{},
	})
	w.transitA = str(tr, "id")
	w.allocA = str(firstAllocation(t, tr), "id")
	w.setClock(t, time.Date(2027, 6, 5, 10, 0, 0, 0, time.UTC))
	charges := o.expect(t, 200, "GET", M+"/charges?contract_id="+w.transitA, nil)
	items := jsonList(charges, "items")
	if len(items) == 0 {
		t.Fatalf("transit without charges: %v", charges)
	}
	ch := items[0].(map[string]any)
	w.chargeA = str(ch, "id")
	w.residualA = int64(ch["total_cents"].(float64)) - int64(ch["paid_amount_cents"].(float64))
	o.expect(t, 201, "POST", M+"/cash-sessions", map[string]any{"float_cents": 50_00})
	return w
}

// raw sends a request without failing the test (safe from goroutines).
func (c *client) send(method, path string, body []byte, contentType string, headers ...string) (response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.h.srv.URL+path, rd)
	if err != nil {
		return response{}, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if tok := c.csrf(); tok != "" {
		req.Header.Set("X-CSRF-Token", tok)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		return response{}, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return response{status: res.StatusCode, header: res.Header, body: b}, nil
}

// replayUntilDone sends the same request until it is not 409 idempotency_in_progress. The deadline only guards
// against a hang: the first execution finishing is what ends the loop.
func (c *client) replayUntilDone(method, path string, body []byte, contentType string, headers ...string) (response, error) {
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); {
		r, err := c.send(method, path, body, contentType, headers...)
		if err != nil {
			return r, err
		}
		if r.status == http.StatusConflict && r.problemType() == "idempotency_in_progress" {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		return r, nil
	}
	return response{}, fmt.Errorf("still in progress")
}

// hammer runs the same replay from n goroutines and returns the final responses.
func hammer(t *testing.T, n int, send func() (response, error)) []response {
	t.Helper()
	out := make([]response, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { out[i], errs[i] = send() })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	return out
}

func assertSameBodies(t *testing.T, rs []response, want int) {
	t.Helper()
	replayed := 0
	for i, r := range rs {
		if r.status != want {
			t.Fatalf("response %d: %d %s", i, r.status, r.body)
		}
		if !bytes.Equal(r.body, rs[0].body) {
			t.Fatalf("response %d differs:\n%s\n%s", i, r.body, rs[0].body)
		}
		if r.header.Get("Idempotent-Replayed") == "true" {
			replayed++
		}
	}
	if replayed != len(rs)-1 {
		t.Fatalf("replayed %d of %d", replayed, len(rs))
	}
}

func countRows(t *testing.T, db *store.DB, table, where string, args ...any) int {
	t.Helper()
	n, err := db.DB.NewSelect().TableExpr(table).Where(where, args...).Count(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func (w *offlineWorld) arrival(notes string) []byte {
	return jsonBody(map[string]any{
		"vessel_id": w.vesselA, "event": "arrived", "allocation_id": w.allocA, "expected_resource_id": w.berthA,
		"at": "2027-06-05T09:40:00Z", "notes": notes,
	})
}

func queuedAt() string { return "2027-06-05T09:41:00Z" }

func TestConcurrency_OfflineReplay_Presence(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newOfflineWorld(t, db)
		dock := w.clients["dock"]
		body := w.arrival("ormeggio di prua")
		path := w.M + "/presences"
		rs := hammer(t, 100, func() (response, error) {
			return dock.replayUntilDone("POST", path, body, "application/json",
				"Idempotency-Key", "offline-presence-0001", offlineHeader, queuedAt())
		})
		assertSameBodies(t, rs, http.StatusCreated)
		if n := countRows(t, db, "presences", "vessel_id = ?", w.vesselA); n != 1 {
			t.Fatalf("presences %d", n)
		}
		if n := countRows(t, db, "audit_log", "action = ?", "presence.record"); n != 1 {
			t.Fatalf("audit rows %d", n)
		}
		if n := len(eventsOf(t, db, "presence.arrived")); n != 1 {
			t.Fatalf("arrived events %d", n)
		}
		var state string
		if err := db.DB.NewSelect().TableExpr("vessel_presence").Column("state").Where("vessel_id = ?", w.vesselA).Scan(t.Context(), &state); err != nil || state != "in_port" {
			t.Fatalf("state %q %v", state, err)
		}
		var diff string
		if err := db.DB.NewSelect().TableExpr("audit_log").Column("diff").Where("action = ?", "presence.record").Scan(t.Context(), &diff); err != nil ||
			!strings.Contains(strings.ReplaceAll(diff, " ", ""), `"offline_queued_at":"2027-06-05T09:41:00Z"`) || strings.Contains(diff, "prua") {
			t.Fatalf("audit diff %s %v", diff, err)
		}
		assertNoPII(t, w.h.logs.String(), "ormeggio di prua")
		for _, ev := range eventsOf(t, db, "presence.arrived") {
			if strings.Contains(string(ev.Payload), "prua") || strings.Contains(string(ev.Payload), "Libeccio") {
				t.Fatalf("event with personal data: %s", ev.Payload)
			}
		}
	})
}

// TestPresence_DomainKeyAfterLostMiddlewareRow: the idempotency row is gone (expired, takeover): the domain key
// still answers with the same presence, nothing new is written.
func TestPresence_DomainKeyAfterLostMiddlewareRow(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newOfflineWorld(t, db)
		dock := w.clients["dock"]
		body := w.arrival("")
		hdr := []string{"Idempotency-Key", "offline-presence-lost", offlineHeader, queuedAt()}
		first, err := dock.send("POST", w.M+"/presences", body, "application/json", hdr...)
		if err != nil || first.status != 201 {
			t.Fatalf("first %d %s %v", first.status, first.body, err)
		}
		if _, err := db.DB.NewDelete().TableExpr("idempotency_keys").Where("1 = 1").Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		again, err := dock.send("POST", w.M+"/presences", body, "application/json", hdr...)
		if err != nil || again.status != 201 {
			t.Fatalf("again %d %s %v", again.status, again.body, err)
		}
		id := func(r response) string { return str(r.json(t)["presence"].(map[string]any), "id") }
		if id(first) != id(again) || countRows(t, db, "presences", "vessel_id = ?", w.vesselA) != 1 ||
			countRows(t, db, "audit_log", "action = ?", "presence.record") != 1 {
			t.Fatalf("domain dedupe failed: %s vs %s", first.body, again.body)
		}
		// The same key with another body is refused.
		db.DB.NewDelete().TableExpr("idempotency_keys").Where("1 = 1").Exec(t.Context()) //nolint:errcheck // test cleanup
		other, _ := dock.send("POST", w.M+"/presences", w.arrival("altro"), "application/json", hdr...)
		if other.status != 422 || other.problemType() != "idempotency_key_reused" {
			t.Fatalf("reused key %d %s", other.status, other.body)
		}
	})
}

// TestPresence_PhotoDomainKeyAfterLostMiddlewareRow: a photo replay whose idempotency row is gone answers with the
// same file: no second stored file, no second presence.photo audit row (INV-83). Another photo replaces it.
func TestPresence_PhotoDomainKeyAfterLostMiddlewareRow(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newOfflineWorld(t, db)
		dock := w.clients["dock"]
		pr, err := dock.send("POST", w.M+"/presences", w.arrival(""), "application/json",
			"Idempotency-Key", "offline-presence-photo-lost", offlineHeader, queuedAt())
		if err != nil || pr.status != 201 {
			t.Fatalf("presence %d %s", pr.status, pr.body)
		}
		id := str(pr.json(t)["presence"].(map[string]any), "id")
		photo := testJPEG(t)
		hdr := []string{"Idempotency-Key", "offline-photo-lost", offlineHeader, queuedAt()}
		before := countRows(t, db, "stored_files", "marina_id = ?", w.marina)
		first, err := dock.send("POST", w.M+"/presences/"+id+"/photo", photo, "image/jpeg", hdr...)
		if err != nil || first.status != 200 {
			t.Fatalf("first %d %s %v", first.status, first.body, err)
		}
		if _, err := db.DB.NewDelete().TableExpr("idempotency_keys").Where("1 = 1").Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		again, err := dock.send("POST", w.M+"/presences/"+id+"/photo", photo, "image/jpeg", hdr...)
		if err != nil || again.status != 200 {
			t.Fatalf("again %d %s %v", again.status, again.body, err)
		}
		if string(first.body) != string(again.body) {
			t.Fatalf("different answers: %s vs %s", first.body, again.body)
		}
		if n := countRows(t, db, "stored_files", "marina_id = ?", w.marina); n != before+1 {
			t.Fatalf("stored files %d, want %d", n, before+1)
		}
		if n := countRows(t, db, "audit_log", "action = ?", "presence.photo"); n != 1 {
			t.Fatalf("photo audit %d", n)
		}
		other := testJPEG(t)
		other = append(other[:len(other)-2:len(other)-2], 0x00, 0xff, 0xd9) // same image, other bytes
		r, err := dock.send("POST", w.M+"/presences/"+id+"/photo", other, "image/jpeg", "Idempotency-Key", "offline-photo-other")
		if err != nil || r.status != 200 || string(r.body) == string(first.body) {
			t.Fatalf("other photo %d %s %v", r.status, r.body, err)
		}
		if n := countRows(t, db, "audit_log", "action = ?", "presence.photo"); n != 2 {
			t.Fatalf("photo audit after replacement %d", n)
		}
	})
}

// TestPresence_ExpectedResourceConflict: the allocation moved after the operation was queued → 409
// allocation_changed with the current berth, nothing written.
func TestPresence_ExpectedResourceConflict(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newOfflineWorld(t, db)
		dock := w.clients["dock"]
		target := w.harbour.Berths[1]
		alloc := w.owner.expect(t, 200, "GET", w.M+"/contracts/"+w.transitA, nil)
		a := firstAllocation(t, alloc)
		w.owner.expect(t, 200, "POST", w.M+"/occupancies/move", map[string]any{
			"allocation_id": str(a, "id"), "version": a["version"], "from_night": "2027-06-05",
			"target": map[string]any{"resource_type": "berth", "resource_id": target},
		})
		r, err := dock.send("POST", w.M+"/presences", w.arrival(""), "application/json",
			"Idempotency-Key", "offline-presence-moved", offlineHeader, queuedAt())
		if err != nil || r.status != 409 || r.problemType() != "allocation_changed" {
			t.Fatalf("moved %d %s %v", r.status, r.body, err)
		}
		det, _ := r.json(t)["details"].(map[string]any)
		if str(det, "current_resource_id") != target || str(det, "current_resource_code") == "" {
			t.Fatalf("details %s", r.body)
		}
		if n := countRows(t, db, "presences", "vessel_id = ?", w.vesselA); n != 0 {
			t.Fatalf("presences written %d", n)
		}
	})
}

// TestPresence_OfflineRequiresAllocation: an offline replay without allocation (or a haul-out) is not an
// offline operation.
func TestPresence_OfflineRequiresAllocation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newOfflineWorld(t, db)
		dock := w.clients["dock"]
		for i, body := range []map[string]any{
			{"vessel_id": w.vesselA, "event": "arrived"},
			{"vessel_id": w.vesselA, "event": "hauled_out", "allocation_id": w.allocA},
			{"vessel_id": w.vesselA, "event": "hauled_out", "allocation_id": w.allocA, "expected_resource_id": w.berthA},
			// Without the expected resource a moved allocation could not surface as allocation_changed.
			{"vessel_id": w.vesselA, "event": "arrived", "allocation_id": w.allocA},
		} {
			r, _ := dock.send("POST", w.M+"/presences", jsonBody(body), "application/json",
				"Idempotency-Key", fmt.Sprintf("offline-no-alloc-%d", i), offlineHeader, queuedAt())
			if r.status != 422 || r.problemType() != "offline_not_allowed" {
				t.Fatalf("%v: %d %s", body, r.status, r.body)
			}
		}
		// The same arrival online (no header) is still accepted.
		dock.expect(t, 201, "POST", w.M+"/presences", map[string]any{"vessel_id": w.vesselA, "event": "arrived"})
	})
}

func (w *offlineWorld) cash(amount int64) []byte {
	return jsonBody(map[string]any{
		"customer_id": w.customerA, "kind": "payment", "method": "cash", "amount_cents": amount, "received_on": "2027-06-05",
		"matches": []map[string]any{{"charge_id": w.chargeA, "amount_cents": amount}},
	})
}

func TestConcurrency_OfflineReplay_CashPayment(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newOfflineWorld(t, db)
		dock := w.clients["dock"]
		amount := w.residualA
		body := w.cash(amount)
		rs := hammer(t, 100, func() (response, error) {
			return dock.replayUntilDone("POST", w.M+"/payments", body, "application/json",
				"Idempotency-Key", "offline-cash-0001", offlineHeader, queuedAt())
		})
		assertSameBodies(t, rs, http.StatusCreated)
		if n := countRows(t, db, "payments", "marina_id = ?", w.marina); n != 1 {
			t.Fatalf("payments %d", n)
		}
		if n := countRows(t, db, "payment_matches", "charge_id = ?", w.chargeA); n != 1 {
			t.Fatalf("matches %d", n)
		}
		ch := w.owner.expect(t, 200, "GET", w.M+"/charges/"+w.chargeA, nil)
		if str(ch, "payment_status") != "paid" {
			t.Fatalf("charge %v", ch)
		}
		cur := w.owner.expect(t, 200, "GET", w.M+"/cash-sessions/current", nil)
		sum, _ := cur["summary"].(map[string]any)
		cashExpected := int64(-1)
		for _, e := range jsonList(sum, "expected") {
			if em := e.(map[string]any); str(em, "method") == "cash" {
				cashExpected = int64(em["amount_cents"].(float64))
			}
		}
		if cashExpected != 50_00+amount {
			t.Fatalf("expected cash %d, want %d (%v)", cashExpected, 50_00+amount, sum)
		}
		var diff string
		if err := db.DB.NewSelect().TableExpr("audit_log").Column("diff").Where("action = ?", "payment.record").Scan(t.Context(), &diff); err != nil ||
			!strings.Contains(diff, "offline_queued_at") {
			t.Fatalf("audit diff %s %v", diff, err)
		}
		scope, _ := store.SystemMarinaScope(t.Context(), db.DB, w.marina)
		if vs, err := store.CheckBillingInvariants(t.Context(), db.DB, scope); err != nil || len(vs) != 0 {
			t.Fatalf("invariants %+v %v", vs, err)
		}
	})
}

// TestPayment_OfflineReceiptModeRefused: a receipt-mode charge numbers a commercial document; it is never
// collected from the offline queue (D6). Not cash, or not matched, is refused too.
func TestPayment_OfflineReceiptModeRefused(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newOfflineWorld(t, db)
		dock := w.clients["dock"]
		if _, err := db.DB.NewUpdate().TableExpr("charges").Set("fiscal_mode = 'receipt'").Where("id = ?", w.chargeA).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		r, _ := dock.send("POST", w.M+"/payments", w.cash(10_00), "application/json",
			"Idempotency-Key", "offline-cash-receipt", offlineHeader, queuedAt())
		if r.status != 422 || r.problemType() != "offline_not_allowed" {
			t.Fatalf("receipt mode %d %s", r.status, r.body)
		}
		unmatched := jsonBody(map[string]any{"kind": "payment", "method": "cash", "amount_cents": 10_00, "auto_match": "oldest_due_first"})
		r, _ = w.owner.send("POST", w.M+"/payments", unmatched, "application/json",
			"Idempotency-Key", "offline-cash-automatch", offlineHeader, queuedAt())
		if r.status != 422 || r.problemType() != "offline_not_allowed" {
			t.Fatalf("auto match %d %s", r.status, r.body)
		}
		if n := countRows(t, db, "payments", "marina_id = ?", w.marina); n != 0 {
			t.Fatalf("payments %d", n)
		}
	})
}

// TestPayment_OfflineAmountEqualsMatches: an offline replay is collection on existing documents only, so the whole
// amount is matched (INV-87): an unmatched remainder (cash credit) or a zero match is refused, online it is not.
func TestPayment_OfflineAmountEqualsMatches(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newOfflineWorld(t, db)
		dock := w.clients["dock"]
		part := w.residualA / 2
		credit := jsonBody(map[string]any{
			"customer_id": w.customerA, "kind": "payment", "method": "cash", "amount_cents": part + 5_00,
			"received_on": "2027-06-05", "matches": []map[string]any{{"charge_id": w.chargeA, "amount_cents": part}},
		})
		r, _ := dock.send("POST", w.M+"/payments", credit, "application/json",
			"Idempotency-Key", "offline-cash-credit", offlineHeader, queuedAt())
		if r.status != 422 || r.problemType() != "offline_not_allowed" {
			t.Fatalf("unmatched remainder %d %s", r.status, r.body)
		}
		zero := jsonBody(map[string]any{
			"customer_id": w.customerA, "kind": "payment", "method": "cash", "amount_cents": part,
			"received_on": "2027-06-05", "matches": []map[string]any{
				{"charge_id": w.chargeA, "amount_cents": part}, {"charge_id": w.chargeA, "amount_cents": 0},
			},
		})
		r, _ = dock.send("POST", w.M+"/payments", zero, "application/json",
			"Idempotency-Key", "offline-cash-zero", offlineHeader, queuedAt())
		if r.status != 422 || (r.problemType() != "offline_not_allowed" && r.problemType() != "validation_failed") {
			t.Fatalf("zero match %d %s", r.status, r.body)
		}
		if n := countRows(t, db, "payments", "marina_id = ?", w.marina); n != 0 {
			t.Fatalf("payments %d", n)
		}
		// The same body sent online is a legitimate payment with credit (M5).
		r, _ = dock.send("POST", w.M+"/payments", credit, "application/json", "Idempotency-Key", "online-cash-credit")
		if r.status != 201 {
			t.Fatalf("online %d %s", r.status, r.body)
		}
		// A full match offline is accepted.
		r, _ = dock.send("POST", w.M+"/payments", w.cash(w.residualA-part), "application/json",
			"Idempotency-Key", "offline-cash-full", offlineHeader, queuedAt())
		if r.status != 201 {
			t.Fatalf("full match %d %s", r.status, r.body)
		}
	})
}

// TestPayment_OfflineClosedSessionConflict: the session open when the operation was queued is closed at replay:
// a conflict, never a silent move to another session.
func TestPayment_OfflineClosedSessionConflict(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newOfflineWorld(t, db)
		o := w.owner
		cur := o.expect(t, 200, "GET", w.M+"/cash-sessions/current", nil)
		sum := cur["summary"].(map[string]any)
		var counted []map[string]any
		for _, e := range jsonList(sum, "expected") {
			em := e.(map[string]any)
			counted = append(counted, map[string]any{"method": em["method"], "amount_cents": em["amount_cents"]})
		}
		o.expect(t, 200, "POST", w.M+"/cash-sessions/"+str(cur, "id")+"/close", map[string]any{"counted": counted}, "If-Match", etagOf(cur["version"]))
		r, _ := w.clients["dock"].send("POST", w.M+"/payments", w.cash(10_00), "application/json",
			"Idempotency-Key", "offline-cash-closed", offlineHeader, queuedAt())
		if r.status != 409 || r.problemType() != "cash_session_required" {
			t.Fatalf("closed session %d %s", r.status, r.body)
		}
	})
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for x := range 32 {
		img.Set(x, x%24, color.RGBA{R: 200, A: 255})
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestConcurrency_OfflineReplay_Photo(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newOfflineWorld(t, db)
		dock := w.clients["dock"]
		pr, err := dock.send("POST", w.M+"/presences", w.arrival(""), "application/json",
			"Idempotency-Key", "offline-presence-photo", offlineHeader, queuedAt())
		if err != nil || pr.status != 201 {
			t.Fatalf("presence %d %s", pr.status, pr.body)
		}
		id := str(pr.json(t)["presence"].(map[string]any), "id")
		photo := testJPEG(t)
		before := countRows(t, db, "stored_files", "marina_id = ?", w.marina)
		rs := hammer(t, 100, func() (response, error) {
			return dock.replayUntilDone("POST", w.M+"/presences/"+id+"/photo", photo, "image/jpeg",
				"Idempotency-Key", "offline-photo-0001", offlineHeader, queuedAt())
		})
		assertSameBodies(t, rs, http.StatusOK)
		if n := countRows(t, db, "stored_files", "marina_id = ?", w.marina); n != before+1 {
			t.Fatalf("stored files %d, want %d", n, before+1)
		}
		if n := countRows(t, db, "audit_log", "action = ?", "presence.photo"); n != 1 {
			t.Fatalf("photo audit %d", n)
		}
	})
}

// TestCollectibles_ScopeAndPermissions: the dock (payments.collect_cash) reads the open charges of today's
// contracts, customer names because it holds customers.read (every collecting role does today; the service blanks
// them otherwise); another date is empty; another marina is 404; a role without collect permissions is 403.
func TestCollectibles_ScopeAndPermissions(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newOfflineWorld(t, db)
		dock := w.clients["dock"]
		page := dock.expect(t, 200, "GET", w.M+"/collectibles", nil)
		items := jsonList(page, "items")
		if len(items) != 1 {
			t.Fatalf("collectibles %v", page)
		}
		it := items[0].(map[string]any)
		if str(it, "charge_id") != w.chargeA || str(it, "contract_id") != w.transitA || int64(it["residual_cents"].(float64)) != w.residualA ||
			str(it["vessel"].(map[string]any), "name") != "Libeccio" || str(it, "contract_code") == "" {
			t.Fatalf("item %v", it)
		}
		if str(it, "customer_display_name") != "Rossi Mario" || str(it, "fiscal_mode") != "invoice" {
			t.Fatalf("dock item %v", it)
		}
		// Another date: nothing in port on 2027-07-01.
		if items := jsonList(dock.expect(t, 200, "GET", w.M+"/collectibles?date=2027-07-01", nil), "items"); len(items) != 0 {
			t.Fatalf("july %v", items)
		}
		if r := w.clients["viewer"].do(t, "GET", w.M+"/collectibles", nil); r.status != 403 {
			t.Fatalf("viewer %d", r.status)
		}
		if r := dock.do(t, "GET", A+"/marinas/"+store.NewID()+"/collectibles", nil); r.status != 404 {
			t.Fatalf("other marina %d", r.status)
		}
	})
}

// TestConcurrency_IdempotencyTakeOver: the conditional update lets exactly one stale claimer win, on both
// dialects.
func TestConcurrency_IdempotencyTakeOver(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		now := time.Date(2027, 6, 1, 7, 0, 0, 123456789, time.UTC)
		row, created, err := store.ClaimIdempotencyKey(t.Context(), db.DB, "u|POST /x", "takeover-key-1", "h", now, now.Add(time.Hour))
		if err != nil || !created {
			t.Fatal(err)
		}
		got, _, err := store.ClaimIdempotencyKey(t.Context(), db.DB, "u|POST /x", "takeover-key-1", "h", now, now.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		later := now.Add(3 * time.Minute)
		wins := 0
		var mu sync.Mutex
		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				ok, err := store.TakeOverIdempotencyKey(t.Context(), db.DB, row.ID, got.UpdatedAt, later)
				if err != nil {
					t.Error(err)
				}
				if ok {
					mu.Lock()
					wins++
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		if wins != 1 {
			t.Fatalf("takeover winners %d", wins)
		}
	})
}

func jsonBody(v any) []byte { b, _ := json.Marshal(v); return b }
