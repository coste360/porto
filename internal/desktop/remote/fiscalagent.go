package remote

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/pkg/ext"
)

// FiscalAgent issues the documents of the server driver "bridge" on the local fiscal device (m6b-plan §8.4,
// D16): while the instance is reachable and a local device is configured, it claims ONE pending document at a time
// (60 s lease, so a slow print never outlives its lease), issues it on the device and reports the outcome with
// Idempotency-Key <documentId>:<claim_id>. Nothing is issued while the instance is unreachable (29.2: no
// commercial documents offline): the documents wait pending on the server.
//
// A document printed is never printed again (INV-86): its "issued" outcome is written to the local journal
// before it is sent and removed only once the instance acknowledged it. A lost answer or an expired lease leaves
// the document pending on the server; the agent resends the journalled outcome first, and when the document is
// claimed again it reports the journalled outcome with the new claim id instead of calling the device.
type FiscalAgent struct {
	Upstream  *Upstream
	Driver    func() FiscalDriver
	Reachable func() bool
	Every     time.Duration
	Log       *slog.Logger
	// Journal is the file of the issued outcomes not yet acknowledged ("" keeps them in memory only).
	Journal string

	mu      sync.Mutex
	loaded  bool
	pending map[string]api.FiscalOutcome // document id -> issued outcome not yet acknowledged
}

// FiscalAgentInterval is the default claim interval.
const FiscalAgentInterval = 10 * time.Second

// claimBatch bounds the documents handled in one run (claimed one by one).
const claimBatch = 10

// Run calls RunOnce every interval until ctx is done.
func (a *FiscalAgent) Run(ctx context.Context) {
	every := a.Every
	if every <= 0 {
		every = FiscalAgentInterval
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if _, err := a.RunOnce(ctx); err != nil && ctx.Err() == nil {
			a.log().Warn("remote: fiscal agent", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (a *FiscalAgent) log() *slog.Logger {
	if a.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return a.Log
}

// ErrNotBridgeDevice: the instance refused the claim (this app is not the bridge device of the marina).
var ErrNotBridgeDevice = errors.New("remote: this app is not the fiscal bridge device of the marina")

// RunOnce resends the unacknowledged outcomes, then claims and handles up to claimBatch documents one by one; it
// returns how many outcomes the instance acknowledged.
func (a *FiscalAgent) RunOnce(ctx context.Context) (int, error) {
	if a.Driver == nil || (a.Reachable != nil && !a.Reachable()) {
		return 0, nil
	}
	drv := a.Driver()
	if drv == nil {
		return 0, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.load()
	n := 0
	for _, id := range slices.Sorted(maps.Keys(a.pending)) {
		ok, err := a.report(ctx, id, a.pending[id])
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	for range claimBatch {
		var claim api.FiscalClaim
		code, err := a.Upstream.JSON(ctx, http.MethodPost, "/api/device/v1/fiscal/claim", nil, api.FiscalClaimRequest{Max: 1}, &claim)
		switch {
		case err != nil:
			return n, err
		case code == http.StatusForbidden:
			return n, nil // not the bridge device: nothing to do
		case code != http.StatusOK:
			return n, errors.New("remote: fiscal claim answered " + http.StatusText(code))
		}
		if len(claim.Items) == 0 {
			return n, nil
		}
		for _, d := range claim.Items {
			out, done := a.pending[d.DocumentId]
			if done {
				out.ClaimId = d.ClaimId // printed already: report it again under the new claim
			} else {
				out = a.issue(ctx, drv, d)
			}
			if out.Status == api.FiscalOutcomeStatus(statusIssued) {
				a.pending[d.DocumentId] = out
				if err := a.save(); err != nil {
					// Without the journal a lost answer could print the document twice: stop issuing.
					return n, err
				}
			}
			ok, err := a.report(ctx, d.DocumentId, out)
			if err != nil {
				return n, err
			}
			if ok {
				n++
			}
			if out.Status == api.FiscalOutcomeStatus(statusDeferred) {
				return n, nil // the device is unreachable: try again at the next interval
			}
		}
	}
	return n, nil
}

const (
	statusIssued   = "issued"
	statusDeferred = "deferred"
)

// report sends one outcome. An acknowledged outcome (2xx) leaves the journal; a refused one stays there while it
// can still be applied (409 claim_expired: the document is claimed again later) and is dropped when the document
// is gone (404). A transport error is returned: the outcome stays journalled.
func (a *FiscalAgent) report(ctx context.Context, documentID string, out api.FiscalOutcome) (bool, error) {
	h := http.Header{}
	h.Set("Idempotency-Key", documentID+":"+out.ClaimId)
	code, err := a.Upstream.JSON(ctx, http.MethodPost, "/api/device/v1/fiscal/documents/"+url.PathEscape(documentID)+"/outcome", h, out, nil)
	if err != nil {
		return false, err
	}
	ok := code == http.StatusNoContent || code == http.StatusOK
	if !ok {
		a.log().Warn("remote: fiscal outcome refused", "document_id", documentID, "status", code)
	}
	if _, journalled := a.pending[documentID]; journalled && (ok || code == http.StatusNotFound) {
		delete(a.pending, documentID)
		if err := a.save(); err != nil {
			return ok, err
		}
	}
	return ok, nil
}

// load reads the journal once (a missing or unreadable file is an empty journal, logged).
func (a *FiscalAgent) load() {
	if a.loaded {
		return
	}
	a.loaded = true
	a.pending = map[string]api.FiscalOutcome{}
	if a.Journal == "" {
		return
	}
	b, err := os.ReadFile(a.Journal)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			a.log().Warn("remote: fiscal journal unreadable", "error", err)
		}
		return
	}
	if err := json.Unmarshal(b, &a.pending); err != nil {
		a.log().Warn("remote: fiscal journal unreadable", "error", err)
		a.pending = map[string]api.FiscalOutcome{}
	}
}

// save writes the journal atomically (0600: document ids and device references, no personal data).
func (a *FiscalAgent) save() error {
	if a.Journal == "" {
		return nil
	}
	b, err := json.Marshal(a.pending)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.Journal), 0o700); err != nil {
		return err
	}
	tmp := a.Journal + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.Journal)
}

type deferrer interface{ Deferred() bool }

func (a *FiscalAgent) issue(ctx context.Context, drv ext.FiscalDriver, d api.FiscalClaimDocument) api.FiscalOutcome {
	req := ext.FiscalDocumentRequest{
		DocumentID: d.DocumentId, Number: d.Number, BusinessDate: d.BusinessDate, TotalCents: d.TotalCents,
		Method: d.Method, Currency: "EUR",
	}
	for _, l := range d.Lines {
		req.Lines = append(req.Lines, ext.FiscalLine{
			Description: l.Description, QtyMilli: l.QtyMilli, GrossCents: l.GrossCents, VATRateBP: int64(l.VatRateBp),
		})
	}
	res, err := drv.Issue(ctx, req)
	out := api.FiscalOutcome{ClaimId: d.ClaimId}
	var df deferrer
	switch {
	case err == nil:
		out.Status = api.FiscalOutcomeStatus(statusIssued)
		if res != nil {
			if res.ExternalRef != "" {
				ref := res.ExternalRef
				out.ExternalRef = &ref
			}
			if !res.IssuedAt.IsZero() {
				at := res.IssuedAt.UTC()
				out.IssuedAt = &at
			}
		}
	case errors.As(err, &df) && df.Deferred():
		out.Status = api.FiscalOutcomeStatus(statusDeferred)
	default:
		out.Status = api.FiscalOutcomeStatus("failed")
		c := "driver_error"
		out.ErrorCode = &c
	}
	return out
}

// FiscalDriver is the local fiscal device (ext.FiscalDriver).
type FiscalDriver = ext.FiscalDriver
