//go:build integration

package acceptance

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_CrossMarinaResourceIdsM12 (INV-73, INV-102, M12 contract) extends the resource-id sweep to the
// sensor and profile routes: marina B calls every route under its own {marinaId} with the ids of real rows of
// marina A (device, gateway, source, alert, profile photo), inserted directly because the CONTRACT stubs create
// nothing. Each call is 404 not_found, also while the handlers are still stubs.
func TestAcceptance_CrossMarinaResourceIdsM12(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		w := newDeskWorld(t, db, 2)
		now := store.UTC(w.clock.Now())
		gw := &store.SensorGateway{
			ID: store.NewID(), MarinaID: w.marina, Code: "GW1", Name: "Gateway", Kind: store.SensorGatewayLoRaWAN, Active: true,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		src := &store.SensorSource{
			ID: store.NewID(), MarinaID: w.marina, Code: "fake", Name: "Fake", PollSeconds: 60, Enabled: true, Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		dev := &store.SensorDevice{
			ID: store.NewID(), MarinaID: w.marina, ExternalID: "A81758FFFE0312AB", Code: "S1", Kind: store.SensorKindUltrasonic,
			ResourceType: store.SensorResourceBerth, ResourceID: w.harbour.Berths[0], MapID: w.harbour.MapID, GatewayID: gw.ID,
			Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		alert := &store.SensorAlert{
			ID: store.NewID(), MarinaID: w.marina, Kind: store.SensorAlertBatteryLow, DeviceID: dev.ID,
			ResourceType: store.SensorResourceBerth, ResourceID: dev.ResourceID, MapID: dev.MapID, ConditionKey: "battery",
			Refs: store.JSON(`{}`), Status: store.SensorAlertOpen, OpenedAt: now, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		fileID := store.NewID()
		photo := &store.StoredFile{
			ID: fileID, MarinaID: w.marina, Purpose: "document", ContentType: "image/png", SizeBytes: 10, SHA256: strings.Repeat("0", 64),
			StorageKey: w.marina + "/" + fileID + ".bin", CreatedAt: now, UpdatedAt: now,
		}
		for _, row := range []any{gw, src, dev, alert, photo} {
			if _, err := db.NewInsert().Model(row).Exec(ctx); err != nil {
				t.Fatalf("insert %T: %v", row, err)
			}
		}

		ids := map[string]string{
			"{sensorDeviceId}": dev.ID, "{sensorGatewayId}": gw.ID, "{sensorSourceId}": src.ID, "{sensorAlertId}": alert.ID,
			"{fileId}": fileID,
		}
		b, err := w.h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "b@porto.test", Name: "Owner B", Password: password, Organization: "Org B", MarinaName: "Marina B", MarinaSlug: "marina-b",
		})
		if err != nil {
			t.Fatal(err)
		}
		bc := w.h.cookieClient()
		bc.login(t, "b@porto.test")
		router, ok := w.h.app.Router.(chi.Routes)
		if !ok {
			t.Fatal("router is not a chi router")
		}
		placeholder := regexp.MustCompile(`\{[A-Za-z]+\}`)
		checked := map[string]bool{}
		routes := 0
		err = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			if !strings.Contains(route, "{marinaId}/sensors/") && !strings.Contains(route, "{marinaId}/profile/") {
				return nil
			}
			path, uses := strings.ReplaceAll(route, "{marinaId}", b.MarinaID), false
			for p, id := range ids {
				if strings.Contains(path, p) {
					path, uses = strings.ReplaceAll(path, p, id), true
					checked[p] = true
				}
			}
			if !uses || placeholder.MatchString(path) {
				return nil
			}
			var body any
			if method != "GET" && method != "DELETE" {
				body = map[string]any{}
			}
			routes++
			r := bc.do(t, method, path, body, "If-Match", `"1"`)
			if r.status != http.StatusNotFound || r.problemType() != "not_found" {
				t.Errorf("%s %s with a resource of marina A: status %d %s, want 404 not_found", method, route, r.status, r.body)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for p := range ids {
			if !checked[p] {
				t.Errorf("no route checked with %s", p)
			}
		}
		if routes < 14 {
			t.Errorf("only %d sensor and profile routes with ids checked", routes)
		}
		// The same ids under marina A's own path reach the handlers (501 while they are stubs, never 404).
		if r := w.owner.do(t, "GET", w.M+"/sensors/devices/"+dev.ID, nil); r.status == http.StatusNotFound {
			t.Errorf("own device: %d %s", r.status, r.body)
		}
	})
}
