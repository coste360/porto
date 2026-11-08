//go:build integration

package desktop_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/peripheral"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store" //nolint:depguard // test setup: testdb hands the database to the test
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestBridgeAPI_LoopbackOnly (INV-85): the bridge answers on the desktop loopback listener only; on the local
// network listener and in the server edition it is 404; without a session 401.
func TestBridgeAPI_LoopbackOnly(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		fixture.New(t, db)
		h := newHarness(t, db, true)
		anon := h.client(t)
		if st, _ := anon.do(http.MethodGet, "/api/bridge/v1/status", nil); st != http.StatusUnauthorized {
			t.Fatalf("anonymous status %d", st)
		}
		c := h.client(t)
		c.login("owner@porto.test")
		st, body := c.do(http.MethodGet, "/api/bridge/v1/status", nil)
		if st != http.StatusOK || body["reader"].(map[string]any)["driver"] != "keyboard" {
			t.Fatalf("loopback status %d %v", st, body)
		}

		lanSrv := httptest.NewUnstartedServer(nil)
		lanSrv.Config = h.app.NewHTTPServer(reqctx.ListenerLAN, nil)
		lanSrv.Start()
		defer lanSrv.Close()
		lc := &client{t: t, base: lanSrv.URL, http: c.http}
		lc.login("owner@porto.test")
		for _, p := range []string{"/api/bridge/v1/status", "/api/bridge/v1/scanner/next?timeout=1"} {
			if st, body := lc.do(http.MethodGet, p, nil); st != http.StatusNotFound || problemType(body) != "not_found" {
				t.Fatalf("LAN %s: %d %v", p, st, body)
			}
		}
		if st, _ := lc.do(http.MethodPost, "/api/bridge/v1/receipts", map[string]string{"payment_id": "x"}); st != http.StatusNotFound {
			t.Fatalf("LAN print %d", st)
		}

		server := newHarness(t, db, false)
		sc := server.client(t)
		sc.login("owner@porto.test")
		if st, body := sc.do(http.MethodGet, "/api/bridge/v1/status", nil); st != http.StatusNotFound || problemType(body) != "not_found" {
			t.Fatalf("server edition %d %v", st, body)
		}
		// The proxy-only desktop operations are always 404 on a Porto server.
		if st, _ := c.do(http.MethodDelete, "/api/desktop/v1/remote", nil); st != http.StatusNotFound {
			t.Fatalf("unpair on a Porto server %d", st)
		}
	})
}

// TestBridgeAPI_PrintReceiptFake: the courtesy receipt of a payment (in-process receipt data) on the fake
// receipt printer; the peripheral settings and the test need the owner; a user who cannot collect gets 404.
func TestBridgeAPI_PrintReceiptFake(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		e.AddUser(t, "viewer@porto.test", auth.RoleViewer, e.OrgID, e.MarinaID)
		h := newHarness(t, db, true)
		c := h.client(t)
		c.login("owner@porto.test")
		M := "/api/admin/v1/marinas/" + e.MarinaID

		if st, body := c.do(http.MethodPost, "/api/bridge/v1/receipts", map[string]string{"payment_id": "0190aaaa-0000-7000-8000-000000000000"}); st != http.StatusServiceUnavailable || problemType(body) != "peripheral_unavailable" {
			t.Fatalf("no printer %d %v", st, body)
		}
		st, body := c.do(http.MethodPut, "/api/desktop/v1/peripherals", map[string]any{
			"receipt": map[string]any{"driver": "fake", "width": 58}, "label": map[string]any{"driver": "none"},
			"reader": map[string]any{"driver": "fake"}, "fiscal": map[string]any{"driver": "fake", "fake_mode": "offline"},
		})
		if st != http.StatusOK {
			t.Fatalf("peripherals %d %v", st, body)
		}

		_, classes := c.do(http.MethodGet, M+"/revenue-classes", nil)
		class := classes["items"].([]any)[0].(map[string]any)["id"].(string)
		st, item := c.do(http.MethodPost, M+"/items", map[string]any{
			"code": "ACQUA", "name": map[string]string{"it": "Acqua"}, "item_type": "product", "unit": "piece",
			"revenue_class_id": class, "price_includes_vat": true, "vat_rate_bp": 1000,
		})
		if st != http.StatusCreated {
			t.Fatalf("item %d %v", st, item)
		}
		if st, b := c.do(http.MethodPost, M+"/cash-sessions", map[string]any{"float_cents": 0}); st != http.StatusCreated {
			t.Fatalf("session %d %v", st, b)
		}
		st, sale := c.do(http.MethodPost, M+"/counter-sales", map[string]any{
			"lines":   []map[string]any{{"item_id": item["id"], "qty_milli": 1000, "unit_cents": 350}},
			"payment": map[string]any{"method": "cash"}, "fiscal_document": true, "receipt": "none",
		}, "Idempotency-Key", "bridge-print-sale-1")
		if st != http.StatusCreated {
			t.Fatalf("sale %d %v", st, sale)
		}
		payID := sale["payment"].(map[string]any)["id"].(string)

		if st, body := c.do(http.MethodPost, "/api/bridge/v1/receipts", map[string]any{"payment_id": payID}); st != http.StatusAccepted {
			t.Fatalf("print %d %v", st, body)
		}
		lines, _ := peripheral.ReadFake(launcherPeripherals(h), "receipt")
		if len(lines) != 1 {
			t.Fatalf("printed %d receipts", len(lines))
		}
		v := h.client(t)
		v.login("viewer@porto.test")
		if st, _ := v.do(http.MethodPost, "/api/bridge/v1/receipts", map[string]any{"payment_id": payID}); st != http.StatusNotFound {
			t.Fatalf("viewer print %d", st)
		}
		if st, _ := v.do(http.MethodPost, "/api/bridge/v1/test", map[string]any{"device": "receipt"}); st != http.StatusForbidden {
			t.Fatalf("viewer test %d", st)
		}
		if st, _ := c.do(http.MethodPost, "/api/bridge/v1/test", map[string]any{"device": "receipt"}); st != http.StatusNoContent {
			t.Fatalf("owner test %d", st)
		}
		if st, body := c.do(http.MethodPost, "/api/bridge/v1/test", map[string]any{"device": "fiscal"}); st != http.StatusServiceUnavailable {
			t.Fatalf("offline fiscal test %d %v", st, body)
		}
		st, status := c.do(http.MethodGet, "/api/bridge/v1/status", nil)
		if st != http.StatusOK || status["fiscal"].(map[string]any)["message"] != "unreachable" {
			t.Fatalf("status %v", status)
		}
		h.rt.FakeReader().Push("PT1:0190aaaa-0000-7000-8000-000000000001")
		if st, body := c.do(http.MethodGet, "/api/bridge/v1/scanner/next?timeout=2", nil); st != http.StatusOK || body["text"] == "" {
			t.Fatalf("scan %d %v", st, body)
		}
		if st, _ := c.do(http.MethodGet, "/api/bridge/v1/scanner/next?timeout=99", nil); st != http.StatusBadRequest {
			t.Fatalf("timeout range %d", st)
		}
		// Kiosk in local mode: the setting needs the owner, the exit an owner's credentials.
		if st, _ := v.do(http.MethodPut, "/api/desktop/v1/kiosk", map[string]any{"enabled": true}); st != http.StatusForbidden {
			t.Fatalf("viewer kiosk %d", st)
		}
		if st, _ := c.do(http.MethodPut, "/api/desktop/v1/kiosk", map[string]any{"enabled": true}); st != http.StatusOK {
			t.Fatalf("owner kiosk %d", st)
		}
		if st, _ := v.do(http.MethodPost, "/api/desktop/v1/kiosk/exit", map[string]any{"email": "viewer@porto.test", "password": fixture.Password}); st != http.StatusBadRequest {
			t.Fatalf("kiosk exit with a non-owner %d", st)
		}
		if st, body := v.do(http.MethodPost, "/api/desktop/v1/kiosk/exit", map[string]any{"email": "owner@porto.test", "password": fixture.Password}); st != http.StatusNoContent {
			t.Fatalf("kiosk exit with the owner %d %v", st, body)
		}
		if e.AuditCount(t, "desktop.peripherals_update") != 1 || e.AuditCount(t, "desktop.kiosk_exit") != 1 {
			t.Fatal("audit rows")
		}
	})
}

func launcherPeripherals(h *harness) string { return h.dir + "/peripherals" }
