// Package fiscal tracks the commercial documents of receipt-mode payments through ext.FiscalDriver (spec 16.3;
// m5-plan §5.2): daily numbering, issue (inline or deferred), void, and the corrispettivi per day, VAT rate and
// method. Invoiced charges never enter the corrispettivi.
package fiscal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Deps are the collaborators of the service.
type Deps struct {
	DB     *store.DB
	Clock  clock.Clock
	Audit  *audit.Writer
	Outbox *event.Outbox
	Log    *slog.Logger
	// Drivers are the extension drivers; none and export are always registered.
	Drivers []ext.FiscalDriver
}

// Service is the fiscal service.
type Service struct {
	d       Deps
	drivers map[string]ext.FiscalDriver
	wake    chan struct{}
}

// NewService returns the service with the core drivers and the registered ones.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	s := &Service{d: d, drivers: map[string]ext.FiscalDriver{}, wake: make(chan struct{}, 1)}
	// M6b: "bridge" is a core driver too (bridge.go); its documents are issued by a desktop, never by the job.
	for _, drv := range append([]ext.FiscalDriver{NoneDriver(), ExportDriver(), BridgeDriver()}, d.Drivers...) {
		s.drivers[drv.Code()] = drv
	}
	return s
}

// Wake signals the job fiscal.issue that a deferred document is pending.
func (s *Service) Wake() <-chan struct{} { return s.wake }

func (s *Service) kick() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// DriverCodes returns the codes of the registered extension drivers (meta capability fiscal.drivers).
func (s *Service) DriverCodes() []string {
	out := make([]string, 0, len(s.d.Drivers)+1)
	for _, d := range s.d.Drivers {
		out = append(out, d.Code())
	}
	return append(out, DriverBridge) // M6b: the core driver issued by a desktop in remote mode
}

// KnownDriver reports whether a driver code is registered (billing settings validation).
func (s *Service) KnownDriver(code string) bool {
	_, ok := s.drivers[code]
	return ok
}

func inline(code string) bool { return code == DriverNone || code == DriverExport }

// Line is one line of a fiscal document (fiscal_documents.lines).
type Line struct {
	Description string      `json:"description"`
	QtyMilli    int64       `json:"qty_milli"`
	Gross       money.Cents `json:"gross_cents"`
	VATRateBP   int64       `json:"vat_rate_bp"`
	VATNature   string      `json:"vat_nature,omitempty"`
}

// Bucket is the VAT of one rate or nature (fiscal_documents.vat).
type Bucket struct {
	RateBP int64       `json:"vat_rate_bp"`
	Nature string      `json:"vat_nature,omitempty"`
	Net    money.Cents `json:"net_cents"`
	VAT    money.Cents `json:"vat_cents"`
	Gross  money.Cents `json:"gross_cents"`
}

// RecordTx implements payment.FiscalRecorder: the next daily number and the fiscal document of a receipt-mode
// charge paid by p, in the payment transaction (issued inline, or pending for the fiscal.issue job). The document
// covers what p matched to ch and is not yet covered by an earlier document of the same pair; nothing is written
// when that is zero.
func (s *Service) RecordTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, p *store.Payment, ch *store.Charge) (*store.FiscalDocument, error) {
	if ch.FiscalMode != "receipt" {
		return nil, nil
	}
	amount, err := coveredAmount(ctx, tx, scope, p.ID, ch.ID)
	if err != nil {
		return nil, err
	}
	if amount <= 0 {
		return nil, nil
	}
	lines, err := store.CollectChargeLines(ctx, tx, scope, ch.ID)
	if err != nil {
		return nil, err
	}
	docLines, buckets := documentLines(lines, money.Cents(ch.TotalCents), amount)
	settings, err := store.GetBillingSettings(ctx, tx, scope, s.now())
	if err != nil {
		return nil, err
	}
	driver := settings.FiscalDriver
	if _, ok := s.drivers[driver]; !ok {
		driver = DriverNone
	}
	day := p.ReceivedOn
	if day.IsZero() {
		day = s.today(ctx, tx, scope)
	}
	n, err := store.NextFiscalNumber(ctx, tx, scope, day)
	if err != nil {
		return nil, err
	}
	now := s.now()
	doc := &store.FiscalDocument{
		ID: store.NewID(), MarinaID: scope.ID(), PaymentID: p.ID, ChargeID: ch.ID, Driver: driver,
		Number: store.FiscalNumber(day, n), BusinessDate: day, Status: store.FiscalPending,
		Lines: store.MustJSON(docLines), VAT: store.MustJSON(buckets), TotalCents: int64(amount),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if inline(driver) {
		doc.Status, doc.IssuedAt, doc.Attempts = store.FiscalIssued, now, 1
	}
	if err := store.InsertFiscalDocument(ctx, tx, doc); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, tx, scope, actor, "fiscal_document.issue", doc.ID, map[string]any{
		"number": doc.Number, "payment_id": p.ID, "charge_id": ch.ID, "driver": driver, "status": doc.Status,
		"total_cents": doc.TotalCents, "business_date": day,
	}); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, doc); err != nil {
		return nil, err
	}
	if doc.Status == store.FiscalPending {
		store.AfterCommit(ctx, s.kick)
	}
	return doc, nil
}

// coveredAmount is Σ matches of the payment on the charge minus what earlier documents of the pair cover.
func coveredAmount(ctx context.Context, tx bun.IDB, scope store.MarinaScope, paymentID, chargeID string) (money.Cents, error) {
	var matched, documented int64
	if err := tx.NewSelect().TableExpr("payment_matches").ColumnExpr("COALESCE(SUM(amount_cents), 0)").
		Where("marina_id = ?", scope.ID()).Where("payment_id = ?", paymentID).Where("charge_id = ?", chargeID).
		Scan(ctx, &matched); err != nil {
		return 0, fmt.Errorf("fiscal: matched amount: %w", err)
	}
	if err := tx.NewSelect().TableExpr("fiscal_documents").ColumnExpr("COALESCE(SUM(total_cents), 0)").
		Where("marina_id = ?", scope.ID()).Where("payment_id = ?", paymentID).Where("charge_id = ?", chargeID).
		Where("refund_id IS NULL").Where("status <> ?", store.FiscalVoid).Scan(ctx, &documented); err != nil {
		return 0, fmt.Errorf("fiscal: documented amount: %w", err)
	}
	return money.Cents(matched - documented), nil
}

// documentLines returns the lines and VAT buckets of a document covering amount of a charge of total: the
// charge lines when the whole charge is covered, otherwise the amount spread over the lines by gross (largest
// remainder) with net in proportion.
func documentLines(lines []store.ChargeLine, total, amount money.Cents) ([]Line, []Bucket) {
	shares := make([]money.Cents, len(lines))
	if amount == total {
		for i, l := range lines {
			shares[i] = money.Cents(l.GrossCents)
		}
	} else if len(lines) > 0 {
		weights := make([]int64, len(lines))
		positive := false
		for i, l := range lines {
			if l.GrossCents > 0 {
				weights[i], positive = l.GrossCents, true
			}
		}
		if !positive {
			weights[0] = 1
		}
		if s, err := money.Allocate(amount, weights); err == nil {
			shares = s
		}
	}
	var out []Line
	var rows []Bucket
	for i, l := range lines {
		g := shares[i]
		if g == 0 {
			continue
		}
		net := money.Cents(l.NetCents)
		if g != money.Cents(l.GrossCents) && l.GrossCents != 0 {
			net = money.Cents(l.NetCents).MulRat(int64(g), l.GrossCents)
		}
		out = append(out, Line{Description: l.Description, QtyMilli: l.QtyMilli, Gross: g, VATRateBP: l.VATRateBP, VATNature: l.VATNature})
		rows = append(rows, Bucket{RateBP: l.VATRateBP, Nature: l.VATNature, Net: net, VAT: g - net, Gross: g})
	}
	return out, sumBuckets(rows)
}

func sumBuckets(rows []Bucket) []Bucket {
	var out []Bucket
	for _, r := range rows {
		found := false
		for i := range out {
			if out[i].RateBP == r.RateBP && out[i].Nature == r.Nature {
				out[i].Net += r.Net
				out[i].VAT += r.VAT
				out[i].Gross += r.Gross
				found = true
			}
		}
		if !found {
			out = append(out, r)
		}
	}
	sortBuckets(out)
	return out
}

// RunIssue is the job fiscal.issue: pending documents of deferred drivers are sent to their driver after commit and
// the outcome applied with a conditional update. No-op with inline drivers.
func (s *Service) RunIssue(ctx context.Context) error {
	// M6b: bridge documents wait for their desktop (BridgeOutcome), the job leaves them alone.
	docs, err := store.PendingFiscalDocumentsExcept(ctx, s.d.DB.DB, DriverBridge, 50)
	if err != nil {
		return err
	}
	var errs []error
	for i := range docs {
		if err := s.issueOne(ctx, &docs[i]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) issueOne(ctx context.Context, doc *store.FiscalDocument) error {
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, doc.MarinaID)
	if err != nil {
		return err
	}
	drv, ok := s.drivers[doc.Driver]
	if !ok || doc.Driver == DriverBridge {
		return nil // driver uninstalled (or the bridge, issued by a desktop): the document stays pending until it is registered again
	}
	req := ext.FiscalDocumentRequest{
		MarinaID: doc.MarinaID, DocumentID: doc.ID, Number: doc.Number, BusinessDate: doc.BusinessDate.String(),
		TotalCents: doc.TotalCents, Currency: "EUR",
	}
	var lines []Line
	_ = json.Unmarshal(doc.Lines, &lines)
	for _, l := range lines {
		req.Lines = append(req.Lines, ext.FiscalLine{Description: l.Description, QtyMilli: l.QtyMilli, GrossCents: int64(l.Gross), VATRateBP: l.VATRateBP, VATNature: l.VATNature})
	}
	if p, err := store.GetPayment(ctx, s.d.DB.DB, scope, doc.PaymentID); err == nil {
		req.Method = p.Method
	}
	res, callErr := drv.Issue(ctx, req) // outside any transaction
	if callErr != nil {
		var d Deferrer
		if errors.As(callErr, &d) && d.Deferred() {
			return nil // device unreachable: retried by the next run
		}
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		out := *doc
		if callErr != nil {
			out.Status, out.ErrorCode = store.FiscalFailed, "driver_error"
		} else {
			out.Status, out.ErrorCode, out.IssuedAt = store.FiscalIssued, "", s.now()
			if res != nil {
				out.ExternalRef = res.ExternalRef
				if !res.IssuedAt.IsZero() {
					out.IssuedAt = res.IssuedAt
				}
			}
		}
		changed, err := store.SetFiscalOutcome(ctx, tx, scope, &out, s.now())
		if err != nil || !changed {
			return err
		}
		action := "fiscal_document.issue"
		if out.Status == store.FiscalFailed {
			action = "fiscal_document.fail"
		}
		if err := s.audit(ctx, tx, scope, audit.Actor{Type: audit.ActorSystem, ID: "system:fiscal.issue"}, action, doc.ID,
			map[string]any{"status": out.Status, "error_code": out.ErrorCode}); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, &out)
	})
}

// ListFilter narrows List.
type ListFilter = store.FiscalDocumentFilter

// List returns the fiscal documents (charges.export).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string, f ListFilter, cursor string, limit int) (store.Page[store.FiscalDocument], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesExport)
	if err != nil {
		return store.Page[store.FiscalDocument]{}, err
	}
	return store.ListFiscalDocuments(ctx, s.d.DB.DB, scope, f, cursor, limit)
}

// DocumentsOfPayment returns the fiscal documents of a payment (receipts, payment detail).
func (s *Service) DocumentsOfPayment(ctx context.Context, db bun.IDB, scope store.MarinaScope, paymentID string) ([]store.FiscalDocument, error) {
	return store.FiscalDocumentsOfPayment(ctx, db, scope, paymentID)
}

// Retry moves a failed document back to pending for the job (charges.export); a pending one is retried as is.
func (s *Service) Retry(ctx context.Context, p *auth.Principal, marinaID, id string) (*store.FiscalDocument, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesExport)
	if err != nil {
		return nil, err
	}
	var doc *store.FiscalDocument
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.LockFiscalDocument(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		switch cur.Status {
		case store.FiscalPending:
		case store.FiscalFailed:
			if _, err := tx.NewUpdate().Model((*store.FiscalDocument)(nil)).Set("status = ?", store.FiscalPending).
				Set("error_code = NULL").Set("version = version + 1").Set("updated_at = ?", store.UTC(s.now())).
				Where("marina_id = ?", scope.ID()).Where("id = ?", id).Where("status = ?", store.FiscalFailed).Exec(ctx); err != nil {
				return err
			}
			cur.Status, cur.ErrorCode, cur.Version = store.FiscalPending, "", cur.Version+1
			if err := s.audit(ctx, tx, scope, p.Actor(), "fiscal_document.retry", id, map[string]any{"status": cur.Status}); err != nil {
				return err
			}
			if err := s.emit(ctx, tx, scope, cur); err != nil {
				return err
			}
		default:
			return ErrNotRetryable
		}
		doc = cur
		store.AfterCommit(ctx, s.kick)
		return nil
	})
	return doc, err
}

// Void voids an issued document through its driver (refunds.issue). The driver is called before the transaction;
// a driver error leaves the document issued.
func (s *Service) Void(ctx context.Context, p *auth.Principal, marinaID, id string) (*store.FiscalDocument, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRefundsIssue)
	if err != nil {
		return nil, err
	}
	doc, err := store.GetFiscalDocument(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, err
	}
	if doc.Status != store.FiscalIssued {
		return nil, ErrNotVoidable
	}
	if drv, ok := s.drivers[doc.Driver]; ok && !inline(doc.Driver) {
		if err := drv.Void(ctx, doc.ExternalRef); err != nil {
			return nil, ErrDriverFailed
		}
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ok, err := store.VoidFiscalDocument(ctx, tx, scope, id, s.now())
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotVoidable
		}
		if doc, err = store.GetFiscalDocument(ctx, tx, scope, id); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "fiscal_document.void", id, map[string]any{"number": doc.Number}); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, doc)
	})
	return doc, err
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) calendar.Date {
	loc := time.UTC
	if tz, err := store.MarinaTimezone(ctx, db, scope); err == nil {
		if l, err := calendar.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return occupancy.Today(s.d.Clock, loc)
}

func (s *Service) audit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action, id string, diff any) error {
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: "fiscal_document", EntityID: id, Diff: diff,
	})
}

func (s *Service) emit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, doc *store.FiscalDocument) error {
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.FiscalDocumentChanged, event.FiscalDocumentChangedData{
		FiscalDocumentID: doc.ID, Status: doc.Status,
	})
	return err
}
