//go:build integration

package acceptance

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_M11_Offline (spec 29.2, INV-71, INV-83): the check-out checklist and a boat photo of the rental
// phone, queued offline and replayed twice through the full HTTP stack with Porto-Offline-Queued-At: one effect and
// the same answer each; the booking is untouched; every commit refuses the offline header (offline_not_allowed).
func TestAcceptance_M11_Offline(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newHoWorld(t, db, nil)
		M := w.M
		w.at(t, 8, 0)
		b := w.owner.expect(t, 201, "POST", M+"/rental/bookings", map[string]any{
			"unit_id": w.unit, "formula_id": w.morning, "start": bkPoint("2027-06-12", 16), "end": bkPoint("2027-06-12", 26),
			"renter": map[string]string{"name": "Ada Rossi"},
		})
		B := M + "/rental/bookings/" + str(b, "id")
		queued := w.clock.Now().UTC().Format("2006-01-02T15:04:05Z")
		raw, _ := json.Marshal(map[string]any{
			"client_ref": "off-check-0001", "items": w.checklistEntries, "fuel_eighths": 7, "engine_hours_milli": 100, "device_at": queued,
		})
		rs := hammer(t, 2, func() (response, error) {
			return w.owner.replayUntilDone(http.MethodPut, B+"/handovers/checkout/checklist", raw, "application/json",
				"Idempotency-Key", "off-key-checklist-01", offlineHeader, queued)
		})
		// PUT is not replayed by the idempotency middleware: the client_ref dedupe gives one effect and the same answer.
		for i, r := range rs {
			if r.status != http.StatusOK || string(r.body) != string(rs[0].body) {
				t.Fatalf("checklist replay %d: %d %s", i, r.status, r.body)
			}
		}
		photo := hoPNG(t, 5)
		ps := hammer(t, 2, func() (response, error) {
			return w.owner.replayUntilDone(http.MethodPost, B+"/handovers/checkout/photos?kind=boat&client_ref=off-photo-0001", photo, "image/png",
				"Idempotency-Key", "off-key-photo-0001", offlineHeader, queued)
		})
		assertSameBodies(t, ps, http.StatusCreated)
		if n := w.sum(t, "SELECT COUNT(*) FROM rental_handover_files WHERE marina_id = ?", w.marina); n != 1 {
			t.Fatalf("files %d, want 1", n)
		}
		if n := w.sum(t, "SELECT COUNT(*) FROM audit_log WHERE action = 'rental_handover.save' AND marina_id = ?", w.marina); n != 1 {
			t.Fatalf("checklist saves %d, want 1", n)
		}
		cur := w.owner.expect(t, 200, "GET", B, nil)
		if str(cur, "status") != "confirmed" || cur["version"] != b["version"] {
			t.Fatalf("booking changed by the offline drafts: %v", cur)
		}
		// Commits are online only.
		for _, c := range []struct{ method, path string }{
			{"POST", B + "/check-out"}, {"POST", B + "/deposit"}, {"PUT", B + "/identity"}, {"POST", B + "/quick-cash"},
		} {
			r := w.owner.do(t, c.method, c.path, map[string]any{}, "Idempotency-Key", "off-commit-0001", offlineHeader, queued, "If-Match", etagOf(cur["version"]))
			if r.status != 422 || r.problemType() != "offline_not_allowed" {
				t.Fatalf("%s %s offline: %d %s", c.method, c.path, r.status, r.body)
			}
		}
	})
}
