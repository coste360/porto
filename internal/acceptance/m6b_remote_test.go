//go:build integration

package acceptance

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/desktop"
	"github.com/coste360/porto/internal/desktop/remote"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/fiscal"
	"github.com/coste360/porto/internal/peripheral"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M6b remote desktop mode and fiscal bridge (m6b-plan §7, §8.4, §10.3).

// bridgeWorld is a desk world with a front-desk (office) user, a desktop device paired with a token and the
// billing settings on the fiscal driver "bridge" for that device.
type bridgeWorld struct {
	*deskWorld
	officeID string
	water    string
	session  string
}

func newBridgeWorld(t *testing.T, db *store.DB) *bridgeWorld {
	t.Helper()
	w := &bridgeWorld{deskWorld: newDeskWorld(t, db, 4)}
	o, M := w.owner, w.M
	mem := o.expect(t, 201, "POST", M+"/members", map[string]any{"email": "banco@porto.test", "name": "Banco", "role": "office"})
	w.officeID = str(mem, "user_id")
	w.water, _ = w.saleItems(t)
	w.session = str(o.expect(t, 201, "POST", M+"/cash-sessions", map[string]any{"float_cents": 100_00}), "id")
	return w
}

// pairDesktop creates a pairing code for the office user and pairs a desktop through the device API.
func (w *bridgeWorld) pairDesktop(t *testing.T, name string) (deviceID, token string) {
	t.Helper()
	pc := w.owner.expect(t, 201, "POST", w.M+"/devices/pairing-codes", map[string]any{"user_id": w.officeID, "kind": "desktop"})
	res := w.h.bearerClient("").expect(t, 201, "POST", "/api/device/v1/pair", map[string]any{
		"code": str(pc, "code"), "name": name, "platform": "macos", "kind": "desktop",
	})
	return str(res["device"].(map[string]any), "id"), str(res, "token")
}

func (w *bridgeWorld) useBridge(t *testing.T, deviceID string) {
	t.Helper()
	bs := w.owner.expect(t, 200, "GET", w.M+"/billing-settings", nil)
	body := map[string]any{
		"default_due_days": bs["default_due_days"], "auto_payment_request": bs["auto_payment_request"],
		"reminder_policy": bs["reminder_policy"], "reminder_hour": bs["reminder_hour"], "arrears_policy": bs["arrears_policy"],
		"export_reminder_days": bs["export_reminder_days"], "fiscal_driver": "bridge", "receipt_footer": "Grazie",
		"sepa_lead_days": bs["sepa_lead_days"], "origin_items": bs["origin_items"], "fiscal_bridge_device_id": deviceID,
	}
	ver := `"0"`
	if v, ok := bs["version"].(float64); ok && v > 0 {
		ver = etagOf(v)
	}
	if r := w.owner.do(t, "PUT", w.M+"/billing-settings", map[string]any{
		"default_due_days": bs["default_due_days"], "auto_payment_request": bs["auto_payment_request"],
		"reminder_policy": bs["reminder_policy"], "reminder_hour": bs["reminder_hour"], "arrears_policy": bs["arrears_policy"],
		"export_reminder_days": bs["export_reminder_days"], "fiscal_driver": "bridge", "receipt_footer": "",
		"sepa_lead_days": bs["sepa_lead_days"], "origin_items": bs["origin_items"],
	}, "If-Match", ver); r.status != 400 && r.status != 422 {
		t.Fatalf("bridge without a device: %d %s", r.status, r.body)
	}
	saved := w.owner.expect(t, 200, "PUT", w.M+"/billing-settings", body, "If-Match", ver)
	if str(saved, "fiscal_bridge_device_id") != deviceID || str(saved, "fiscal_driver") != "bridge" {
		t.Fatalf("billing settings %v", saved)
	}
}

// sale records a receipt-mode counter sale with a commercial document; it returns the payment and document ids.
func (w *bridgeWorld) sale(t *testing.T, i int) (paymentID, documentID string) {
	t.Helper()
	res := w.owner.expect(t, 201, "POST", w.M+"/counter-sales", map[string]any{
		"lines":   []map[string]any{{"item_id": w.water, "qty_milli": 1000, "unit_cents": 2_00 + i}},
		"payment": map[string]any{"method": "cash"}, "fiscal_document": true, "receipt": "none",
	}, "Idempotency-Key", fmt.Sprintf("bridge-sale-%04d", i))
	fd, _ := res["fiscal_document"].(map[string]any)
	if fd == nil || str(fd, "status") != "pending" || str(fd, "driver") != "bridge" {
		t.Fatalf("sale %d fiscal document %v", i, res["fiscal_document"])
	}
	return str(res["payment"].(map[string]any), "id"), str(fd, "id")
}

func (w *bridgeWorld) docStatus(t *testing.T, id string) (status, ref string) {
	t.Helper()
	page := w.owner.expect(t, 200, "GET", w.M+"/fiscal/documents?limit=200", nil)
	for _, it := range jsonList(page, "items") {
		d := it.(map[string]any)
		if str(d, "id") == id {
			return str(d, "status"), str(d, "external_ref")
		}
	}
	t.Fatalf("document %s not listed", id)
	return "", ""
}

func (w *bridgeWorld) devicePrincipal(t *testing.T, token string) *auth.Principal {
	t.Helper()
	p, err := w.h.app.Auth.AuthenticateDevice(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// --- certificates of the online instance (a self-signed LAN-like target) ---------------------------------------

func instanceCert(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Porto test CA"}, IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
	}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	ca, _ := x509.ParseCertificate(caDER)
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "porto"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, leafTmpl, ca, &k.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der, caDER}, PrivateKey: k}, remote.FingerprintB64(der)
}

type memDeviceToken struct {
	mu sync.Mutex
	v  string
}

func (m *memDeviceToken) Get() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.v == "" {
		return "", desktop.ErrSecretNotFound
	}
	return m.v, nil
}
func (m *memDeviceToken) Set(v string) error { m.mu.Lock(); m.v = v; m.mu.Unlock(); return nil }
func (m *memDeviceToken) Delete() error      { m.mu.Lock(); m.v = ""; m.mu.Unlock(); return nil }

type proxyClient struct {
	base string
	http *http.Client
}

func (c *proxyClient) do(t *testing.T, method, path, body string, hdr ...string) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, c.base+path, rd)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// TestAcceptance_M6bRemote: a real server over TLS (self-signed, pinned), a desktop paired in remote mode through
// the pinned client, the panel API through the proxy with the device token, a presence recorded through the
// proxy, a receipt-mode sale on the bridge driver issued by the desktop's fiscal agent on its local fake device,
// the courtesy receipt printed on the local fake printer, and the revocation resetting the app.
func TestAcceptance_M6bRemote(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newBridgeWorld(t, db)
		ctx := context.Background()
		cert, fp := instanceCert(t)
		online := httptest.NewUnstartedServer(w.h.app.Router)
		online.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		online.StartTLS()
		defer online.Close()

		// 1. Pairing with the pinned client (fingerprint of the payload), then the CA is pinned too.
		pc := w.owner.expect(t, 201, "POST", w.M+"/devices/pairing-codes", map[string]any{"user_id": w.officeID, "kind": "desktop"})
		paired, err := remote.Pair(ctx, remote.PairInput{URL: online.URL, Code: str(pc, "code"), Fingerprint: fp, Name: "Banco", Platform: "darwin", AppVersion: "1.0.0"})
		if err != nil {
			t.Fatal(err)
		}
		if paired.Pins.LeafFP == "" || paired.Pins.CAFP == "" || paired.MarinaID != w.marina {
			t.Fatalf("paired %+v", paired.Pins)
		}

		// 2. The remote runtime and its proxy, as the launcher builds them.
		dir := t.TempDir()
		paths := desktop.Paths{Root: dir}
		settings := desktop.DefaultSettings()
		settings.Mode = "remote"
		settings.AppPinHash, _ = (&auth.Hasher{Params: auth.TestArgon2Params}).Hash("482910")
		settings.Remote = &desktop.RemoteFile{
			URL: paired.Origin.String(), DeviceID: paired.DeviceID, MarinaID: paired.MarinaID, MarinaName: paired.MarinaName,
			LeafFP: paired.Pins.LeafFP, CAFP: paired.Pins.CAFP, PairedAt: time.Now().UTC(),
		}
		settings.Peripherals.Receipt.Driver, settings.Peripherals.Fiscal.Driver = "fake", "fake"
		if err := desktop.SaveSettings(paths.Settings(), settings); err != nil {
			t.Fatal(err)
		}
		tokens := &memDeviceToken{v: paired.Token}
		rr := desktop.NewRemoteRuntime(desktop.RemoteRuntimeConfig{
			Version: "1.0.0", Paths: paths, Settings: settings, Argon2: auth.TestArgon2Params, Tokens: tokens, SecretKind: "keyring",
		})
		var lc net.ListenConfig
		ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		local := remote.NewLocalAPI(rr, paired.MarinaID, false)
		proxy := remote.New(remote.Options{
			Origin: paired.Origin, Pins: remote.Pins{LeafFP: paired.Pins.LeafFP, CAFP: paired.Pins.CAFP}, Token: rr.Token,
			Port: port, CacheDir: paths.RemoteCache(), Local: local,
			OnRevoked: func() { _ = rr.Reset(context.Background(), true) },
		})
		local.Proxy = proxy
		resets := make(chan bool, 1)
		rr.Attach(proxy, nil, func(revoked bool) { resets <- revoked })
		srv := &http.Server{Handler: proxy, ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = srv.Serve(ln) }()
		defer srv.Close()
		jar, _ := cookiejar.New(nil)
		pcl := &proxyClient{base: "http://127.0.0.1:" + strconv.Itoa(port), http: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
		enter, _ := proxy.EnterURL("")
		k := enter[strings.Index(enter, "#k=")+3:]
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, pcl.base+remote.EnterPath, strings.NewReader("k="+url.QueryEscape(k)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", pcl.base)
		if res, err := pcl.http.Do(req); err != nil || res.StatusCode != http.StatusNoContent {
			t.Fatalf("enter: %v %v", res, err)
		} else {
			res.Body.Close()
		}

		// 3. The panel API through the proxy: the device token authenticates, the page never sees it.
		code, body := pcl.do(t, "GET", "/api/admin/v1/organizations", "")
		if code != 200 || !strings.Contains(body, "Porto Banchina SRL") || strings.Contains(body, paired.Token) {
			t.Fatalf("organizations through the proxy %d %s", code, body)
		}
		code, body = pcl.do(t, "GET", "/api/desktop/v1/status", "")
		if code != 200 || !strings.Contains(body, `"reachable":true`) || !strings.Contains(body, `"mode":"remote"`) {
			t.Fatalf("status %d %s", code, body)
		}

		// 4. A presence through the proxy: a confirmed transit arrives.
		vessel := w.vessel(t, "Maestrale", 1100, 380)
		cust := w.customer(t, "Remoto")
		sr := w.owner.expect(t, 200, "POST", w.M+"/occupancies/search", map[string]any{
			"purpose": "transit", "vessel_id": vessel, "arrival": "2027-06-01", "departure": "2027-06-03", "customer_id": cust,
			"resource_types": []string{"berth"},
		})
		seg := jsonList(jsonList(sr, "solutions")[0].(map[string]any), "segments")[0].(map[string]any)
		tr := w.owner.expect(t, 201, "POST", w.M+"/transits", map[string]any{
			"vessel_id": vessel, "customer_id": cust, "arrival": "2027-06-01", "departure": "2027-06-03",
			"placements": []any{map[string]any{"resource_type": seg["resource_type"], "resource_id": seg["resource_id"], "first_night": seg["first_night"], "last_night": seg["last_night"]}},
		}, "Idempotency-Key", "remote-transit-0001")
		w.owner.expect(t, 200, "POST", w.M+"/transits/"+str(tr, "id")+"/confirm", map[string]any{"reference": "POS"}, "If-Match", etagOf(tr["version"]))
		code, body = pcl.do(t, "POST", w.M+"/presences", `{"vessel_id":"`+vessel+`","event":"arrived"}`, "Idempotency-Key", "remote-arrival-0001")
		if code != 201 {
			t.Fatalf("presence through the proxy %d %s", code, body)
		}

		// 5. Receipt-mode sale on the bridge driver: pending; the job leaves it alone; the agent issues it locally.
		w.useBridge(t, paired.DeviceID)
		payID, docID := w.sale(t, 1)
		w.h.runJobs("fiscal.issue")
		if st, _ := w.docStatus(t, docID); st != "pending" {
			t.Fatalf("job touched a bridge document: %s", st)
		}
		agent := &remote.FiscalAgent{Upstream: proxy.Upstream(), Reachable: proxy.Reachable, Driver: func() remote.FiscalDriver { return rr.Peripherals().Fiscal }}
		if n, err := agent.RunOnce(ctx); err != nil || n != 1 {
			t.Fatalf("agent %d %v", n, err)
		}
		st, ref := w.docStatus(t, docID)
		if st != "issued" || !strings.HasPrefix(ref, "FAKE-") {
			t.Fatalf("after the agent: %s %q", st, ref)
		}
		if lines, _ := peripheral.ReadFake(paths.Peripherals(), "fiscal"); len(lines) != 1 {
			t.Fatalf("local fiscal device calls %d", len(lines))
		}

		// 6. The courtesy receipt on the local printer, data from the instance (format=json, no customer).
		code, body = pcl.do(t, "POST", "/api/bridge/v1/receipts", `{"payment_id":"`+payID+`"}`)
		if code != 202 {
			t.Fatalf("print %d %s", code, body)
		}
		lines, _ := peripheral.ReadFake(paths.Peripherals(), "receipt")
		if len(lines) != 1 {
			t.Fatalf("receipts printed %d", len(lines))
		}
		raw := w.owner.do(t, "GET", w.M+"/payments/"+payID+"/receipt?format=json", nil)
		var rd map[string]any
		_ = json.Unmarshal(raw.body, &rd)
		if raw.status != 200 || rd["not_fiscal"] != true || rd["customer"] != nil || str(rd, "fiscal_number") == "" {
			t.Fatalf("receipt json %d %s", raw.status, raw.body)
		}

		// 7. Revoke: the next request finds the device revoked and the app resets to the first run.
		w.owner.expect(t, 204, "DELETE", w.M+"/devices/"+paired.DeviceID, nil)
		_, _ = pcl.do(t, "GET", "/api/admin/v1/organizations", "")
		select {
		case revoked := <-resets:
			if !revoked {
				t.Fatal("reset not marked revoked")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("revocation not detected")
		}
		if tokens.v != "" {
			t.Fatal("token kept after revocation")
		}
		if s, err := desktop.LoadSettings(paths.Settings()); err != nil || s.Mode != "local" || s.Remote != nil {
			t.Fatalf("settings after revocation %+v %v", s.Remote, err)
		}
	})
}

// TestConcurrency_FiscalBridgeClaim (INV-86): 100 goroutines claim the same pending bridge documents; every
// document is claimed by exactly one of them and issued once.
func TestConcurrency_FiscalBridgeClaim(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newBridgeWorld(t, db)
		dev, token := w.pairDesktop(t, "Banco")
		w.useBridge(t, dev)
		const docs = 25
		want := map[string]bool{}
		for i := range docs {
			_, id := w.sale(t, i)
			want[id] = true
		}
		p := w.devicePrincipal(t, token)
		ctx := context.Background()
		var (
			mu      sync.Mutex
			claimed = map[string][]string{}
			wg      sync.WaitGroup
			errs    []error
		)
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cs, err := w.h.app.Fiscal.ClaimBridge(ctx, p, fiscal.MaxBridgeClaim)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs = append(errs, err)
					return
				}
				for _, c := range cs {
					claimed[c.Document.ID] = append(claimed[c.Document.ID], c.ClaimID)
				}
			}()
		}
		wg.Wait()
		if len(errs) > 0 {
			t.Fatalf("claim errors: %v", errs[0])
		}
		if len(claimed) != docs {
			t.Fatalf("claimed %d documents, want %d", len(claimed), docs)
		}
		var outcomes sync.WaitGroup
		for id, ids := range claimed {
			if !want[id] || len(ids) != 1 {
				t.Fatalf("document %s claimed %d times", id, len(ids))
			}
			for range 4 { // the same outcome reported concurrently
				outcomes.Add(1)
				go func() {
					defer outcomes.Done()
					_ = w.h.app.Fiscal.BridgeOutcome(ctx, p, id, fiscal.BridgeOutcomeInput{ClaimID: ids[0], Status: fiscal.BridgeIssued, ExternalRef: "RT-" + id[:8]})
				}()
			}
		}
		outcomes.Wait()
		var issued, audits int
		if err := db.NewSelect().TableExpr("fiscal_documents").ColumnExpr("COUNT(*)").Where("marina_id = ?", w.marina).
			Where("status = 'issued'").Where("driver = 'bridge'").Scan(ctx, &issued); err != nil {
			t.Fatal(err)
		}
		if err := db.NewSelect().TableExpr("audit_log").ColumnExpr("COUNT(*)").Where("action = 'fiscal.bridge_issue'").Scan(ctx, &audits); err != nil {
			t.Fatal(err)
		}
		if issued != docs || audits != docs {
			t.Fatalf("issued %d, audit rows %d, want %d", issued, audits, docs)
		}
		// Nothing is left to claim.
		if cs, err := w.h.app.Fiscal.ClaimBridge(ctx, p, 10); err != nil || len(cs) != 0 {
			t.Fatalf("claim after issue: %d %v", len(cs), err)
		}
	})
}

// TestFiscalBridge_OutcomeIdempotent: the outcome is replayed by Idempotency-Key; a stale claim is 409
// claim_expired; a deferred outcome keeps the document pending and claimable again; the lease expires.
func TestFiscalBridge_OutcomeIdempotent(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newBridgeWorld(t, db)
		dev, token := w.pairDesktop(t, "Banco")
		w.useBridge(t, dev)
		_, docID := w.sale(t, 1)
		d := w.h.bearerClient(token)
		claim := d.expect(t, 200, "POST", "/api/device/v1/fiscal/claim", map[string]any{"max": 10})
		items := jsonList(claim, "items")
		if len(items) != 1 || str(items[0].(map[string]any), "document_id") != docID {
			t.Fatalf("claim %v", claim)
		}
		item := items[0].(map[string]any)
		claimID := str(item, "claim_id")
		if len(jsonList(item, "lines")) == 0 || str(item, "method") != "cash" {
			t.Fatalf("claimed document without lines or method: %v", item)
		}
		// A second claim within the lease gets nothing.
		if again := d.expect(t, 200, "POST", "/api/device/v1/fiscal/claim", map[string]any{"max": 10}); len(jsonList(again, "items")) != 0 {
			t.Fatalf("claimed twice: %v", again)
		}
		out := "/api/device/v1/fiscal/documents/" + docID + "/outcome"
		if r := d.do(t, "POST", out, map[string]any{"claim_id": "0190aaaa-0000-7000-8000-000000000000", "status": "issued"}, "Idempotency-Key", docID+":stale"); r.status != 409 || r.problemType() != "claim_expired" {
			t.Fatalf("stale claim %d %s", r.status, r.body)
		}
		// Deferred: still pending, claimable again with a new claim.
		d.expect(t, 204, "POST", out, map[string]any{"claim_id": claimID, "status": "deferred"}, "Idempotency-Key", docID+":"+claimID)
		if st, _ := w.docStatus(t, docID); st != "pending" {
			t.Fatalf("deferred document %s", st)
		}
		claim = d.expect(t, 200, "POST", "/api/device/v1/fiscal/claim", map[string]any{"max": 10})
		claimID = str(jsonList(claim, "items")[0].(map[string]any), "claim_id")
		key := docID + ":" + claimID
		body := map[string]any{"claim_id": claimID, "status": "issued", "external_ref": "RT-0001"}
		first := d.do(t, "POST", out, body, "Idempotency-Key", key)
		second := d.do(t, "POST", out, body, "Idempotency-Key", key)
		if first.status != 204 || second.status != 204 {
			t.Fatalf("outcome %d then %d", first.status, second.status)
		}
		if st, ref := w.docStatus(t, docID); st != "issued" || ref != "RT-0001" {
			t.Fatalf("issued %s %s", st, ref)
		}
		var audits int
		_ = db.NewSelect().TableExpr("audit_log").ColumnExpr("COUNT(*)").Where("action = 'fiscal.bridge_issue'").Where("entity_id = ?", docID).Scan(context.Background(), &audits)
		if audits != 2 { // deferred + issued
			t.Fatalf("audit rows %d", audits)
		}
		// An issued document cannot be failed afterwards.
		if r := d.do(t, "POST", out, map[string]any{"claim_id": claimID, "status": "failed"}, "Idempotency-Key", key+":x"); r.status != 409 {
			t.Fatalf("fail after issue %d %s", r.status, r.body)
		}

		// Lease expiry: a claim older than 60 s is taken over by the next claim.
		_, doc2 := w.sale(t, 2)
		c1 := d.expect(t, 200, "POST", "/api/device/v1/fiscal/claim", map[string]any{"max": 1})
		old := str(jsonList(c1, "items")[0].(map[string]any), "claim_id")
		w.clock.Advance(61 * time.Second)
		w.owner.login(t, "owner@porto.test")
		c2 := d.expect(t, 200, "POST", "/api/device/v1/fiscal/claim", map[string]any{"max": 1})
		if items := jsonList(c2, "items"); len(items) != 1 || str(items[0].(map[string]any), "document_id") != doc2 {
			t.Fatalf("expired lease not reclaimed: %v", c2)
		}
		o2 := "/api/device/v1/fiscal/documents/" + doc2 + "/outcome"
		if r := d.do(t, "POST", o2, map[string]any{"claim_id": old, "status": "issued"}, "Idempotency-Key", doc2+":"+old); r.status != 409 || r.problemType() != "claim_expired" {
			t.Fatalf("old claim after takeover %d %s", r.status, r.body)
		}
		if r := d.do(t, "POST", "/api/device/v1/fiscal/claim", map[string]any{"max": 11}); r.status != 400 {
			t.Fatalf("max 11: %d", r.status)
		}
	})
}

// TestFiscalBridge_WrongDevice403: only the device of the billing settings may claim or report.
func TestFiscalBridge_WrongDevice403(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newBridgeWorld(t, db)
		dev, _ := w.pairDesktop(t, "Banco")
		_, other := w.pairDesktop(t, "Ufficio")
		w.useBridge(t, dev)
		_, docID := w.sale(t, 1)
		o := w.h.bearerClient(other)
		if r := o.do(t, "POST", "/api/device/v1/fiscal/claim", map[string]any{"max": 10}); r.status != 403 {
			t.Fatalf("other device claim %d %s", r.status, r.body)
		}
		if r := o.do(t, "POST", "/api/device/v1/fiscal/documents/"+docID+"/outcome", map[string]any{"claim_id": "x", "status": "issued"}); r.status != 403 {
			t.Fatalf("other device outcome %d %s", r.status, r.body)
		}
		// A session (not a device) cannot claim either.
		if r := w.owner.do(t, "POST", "/api/device/v1/fiscal/claim", map[string]any{"max": 10}); r.status != 401 && r.status != 403 {
			t.Fatalf("session claim %d", r.status)
		}
		// A mobile device is not a valid bridge device.
		pc := w.owner.expect(t, 201, "POST", w.M+"/devices/pairing-codes", map[string]any{"user_id": w.officeID, "kind": "mobile"})
		mob := w.h.bearerClient("").expect(t, 201, "POST", "/api/device/v1/pair", map[string]any{"code": str(pc, "code"), "name": "Tel", "platform": "android", "kind": "mobile"})
		bs := w.owner.expect(t, 200, "GET", w.M+"/billing-settings", nil)
		bs["fiscal_bridge_device_id"] = str(mob["device"].(map[string]any), "id")
		delete(bs, "fiscal_drivers")
		delete(bs, "version")
		if r := w.owner.do(t, "PUT", w.M+"/billing-settings", bs, "If-Match", etagOf(w.owner.expect(t, 200, "GET", w.M+"/billing-settings", nil)["version"])); r.status != 400 && r.status != 422 {
			t.Fatalf("mobile bridge device accepted %d %s", r.status, r.body)
		}
	})
}

// TestFiscalBridge_JobSkipsBridgeDocs: fiscal.issue never issues or fails bridge documents, and is not starved
// by them.
func TestFiscalBridge_JobSkipsBridgeDocs(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newBridgeWorld(t, db)
		dev, _ := w.pairDesktop(t, "Banco")
		w.useBridge(t, dev)
		var ids []string
		for i := range 3 {
			_, id := w.sale(t, i)
			ids = append(ids, id)
		}
		for range 2 {
			w.h.runJobs("fiscal.issue")
		}
		for _, id := range ids {
			if st, _ := w.docStatus(t, id); st != "pending" {
				t.Fatalf("bridge document %s is %s after the job", id, st)
			}
		}
		pending, err := store.PendingFiscalDocumentsExcept(context.Background(), db.DB, fiscal.DriverBridge, 50)
		if err != nil || len(pending) != 0 {
			t.Fatalf("job candidates %d %v", len(pending), err)
		}
	})
}
