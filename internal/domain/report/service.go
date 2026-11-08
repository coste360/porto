// Package report serves the M7 reports, the manager dashboard and the period comparison (spec 21, m7-plan): each
// request is authorised (reports.operational / reports.all, spec 26.1), runs in one read-only transaction
// (store.DB.ReadTx, INV-61: no audit row, no outbox event), reads raw marina-scoped rows from internal/store and
// hands them to the pure calc, align and swap packages. A dashboard tile is the Totals of the very report call its
// drill link makes (INV-62).
package report

import (
	"context"
	"errors"
	"log/slog"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/store"
)

// Deps are the collaborators of the service. Prices is used read-only for list prices (m7-plan D22).
type Deps struct {
	DB     *store.DB
	Clock  clock.Clock
	Prices *pricelist.Service
	Log    *slog.Logger
}

// Service is the report service.
type Service struct {
	d Deps
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{d: d}
}

// Every report method validates q (q.Report must match), authorises p on marinaID (another marina: auth.ErrNotFound,
// missing permission: auth.ErrForbidden), and returns the result. With q.Format json the records are the page of
// the selected section; with csv or xlsx they are every record of the section (ErrTooLarge beyond MaxExportRows),
// which export turns into the file. Totals and groups never depend on the page.

// exec authorises, opens the read transaction, resolves the period defaults and runs fn.
func exec[T any](ctx context.Context, s *Service, p *auth.Principal, marinaID string, q Params, key Key, needAll bool,
	fn func(r *run, q Params) (T, error),
) (T, error) {
	var out T
	acc, err := s.authorize(ctx, p, marinaID, needAll)
	if err != nil {
		return out, err
	}
	if q.Report != "" && q.Report != key {
		return out, filterErr("report")
	}
	q.Report = key
	if err := checkPeriod(q.From, q.To); err != nil {
		return out, err
	}
	start := s.d.Clock.Now()
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r := s.newRun(ctx, tx, p, acc)
		var err error
		if q.From, q.To, err = r.period(q.From, q.To); err != nil {
			return err
		}
		if key == calc.KeyReceivables {
			if q.AsOf.IsZero() {
				q.AsOf = r.today
			}
			if q.AsOf.After(r.today) {
				return ErrInvalidPeriod
			}
		}
		out, err = fn(r, q)
		return err
	})
	s.d.Log.DebugContext(ctx, "report", "report", string(key), "days", q.From.DaysUntil(q.To)+1,
		"duration_ms", s.d.Clock.Now().Sub(start).Milliseconds())
	return out, err
}

// finish fills the envelope and keeps the page of the selected section (every record for an export).
func finish[T, R any](res *calc.Result[T, R], q Params, today calendar.Date) error {
	recs, err := envelope(&res.Envelope, q, today, res.Records)
	res.Records = recs
	return err
}

func envelope[R any](env *calc.Envelope, q Params, today calendar.Date, recs []R) ([]R, error) {
	env.Report, env.From, env.To, env.Today = q.Report, q.From, q.To, today
	env.Filters, env.IgnoredFilters = q.filters()
	env.GroupBy = q.GroupBy
	env.Section = q.Section
	if env.Section == "" && len(specs[q.Report].sections) > 0 {
		env.Section = specs[q.Report].sections[0]
	}
	if env.Notes == nil {
		env.Notes = []calc.Note{}
	}
	if env.IgnoredFilters == nil {
		env.IgnoredFilters = []string{}
	}
	if recs == nil {
		recs = []R{}
	}
	if q.Format == FormatCSV || q.Format == FormatXLSX {
		if len(recs) > MaxExportRows {
			return nil, ErrTooLarge
		}
		page, info := calc.PageOf(recs, 1, 0)
		env.Page = info
		return page, nil
	}
	size := q.PageSize
	if size <= 0 {
		size = DefaultPageSize
	}
	page, info := calc.PageOf(recs, q.Page, min(size, MaxPageSize))
	env.Page = info
	return page, nil
}

// Occupancy returns the R1 occupancy (reports.operational or reports.all).
func (s *Service) Occupancy(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*OccupancyResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyOccupancy, false, func(r *run, q Params) (*OccupancyResult, error) {
		res, _, err := r.runOccupancy(q)
		if err != nil {
			return nil, err
		}
		return res, finish(&res.Result, q, r.today)
	})
}

// Efficiency returns the R2 occupancy efficiency (reports.operational or reports.all).
func (s *Service) Efficiency(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*EfficiencyResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyEfficiency, false, func(r *run, q Params) (*EfficiencyResult, error) {
		res, _, err := r.runEfficiency(q)
		if err != nil {
			return nil, err
		}
		return res, finish(&res.Result, q, r.today)
	})
}

// AllocationWaste returns the R3 allocation waste; money only with reports.all (reports.operational or reports.all).
func (s *Service) AllocationWaste(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*WasteResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyAllocationWaste, false, func(r *run, q Params) (*WasteResult, error) {
		res, err := r.runWaste(q)
		if err != nil {
			return nil, err
		}
		return res, finish(&res.Result, q, r.today)
	})
}

// LostDemand returns the R4 lost demand; values only with reports.all (reports.operational or reports.all).
func (s *Service) LostDemand(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*LostDemandResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyLostDemand, false, func(r *run, q Params) (*LostDemandResult, error) {
		res, _, err := r.runLostDemand(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// Customers returns the R5 customers and renewals (reports.all).
func (s *Service) Customers(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*CustomersResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyCustomers, true, func(r *run, q Params) (*CustomersResult, error) {
		res, _, err := r.runCustomers(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// Production returns the R6 production (reports.all).
func (s *Service) Production(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*ProductionResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyProduction, true, func(r *run, q Params) (*ProductionResult, error) {
		res, _, err := r.runProduction(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// Accruals returns the R7 competence, deferrals and accruals (reports.all).
func (s *Service) Accruals(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*AccrualsResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyAccruals, true, func(r *run, q Params) (*AccrualsResult, error) {
		res, err := r.runAccruals(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// Charges returns the R8 detailed charges (reports.all).
func (s *Service) Charges(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*ChargesResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyCharges, true, func(r *run, q Params) (*ChargesResult, error) {
		res, _, err := r.runCharges(q)
		if err != nil {
			return nil, err
		}
		return res, finish(&res.Result, q, r.today)
	})
}

// Payments returns the R9 detailed payments, base incasso (reports.all).
func (s *Service) Payments(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*PaymentsResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyPayments, true, func(r *run, q Params) (*PaymentsResult, error) {
		res, _, err := r.runPayments(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// RevenueClasses returns the R10 revenue classes (reports.all).
func (s *Service) RevenueClasses(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*RevenueClassesResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyRevenueClasses, true, func(r *run, q Params) (*RevenueClassesResult, error) {
		res, err := r.runRevenueClasses(q)
		if err != nil {
			return nil, err
		}
		if res.Classes == nil {
			res.Classes = []calc.RevenueClassGroup{}
		}
		res.Records, err = envelope(&res.Envelope, q, r.today, res.Records)
		return res, err
	})
}

// Receivables returns the R12 receivables and reminders (reports.all).
func (s *Service) Receivables(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*ReceivablesResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyReceivables, true, func(r *run, q Params) (*ReceivablesResult, error) {
		res, err := r.runReceivables(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// Presences returns the R13 presences and releases; money only with reports.all (reports.operational or reports.all).
func (s *Service) Presences(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*PresencesResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyPresences, false, func(r *run, q Params) (*PresencesResult, error) {
		res, _, err := r.runPresences(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// Rights returns the R14 rights and condominium (reports.all).
func (s *Service) Rights(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*RightsResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyRights, true, func(r *run, q Params) (*RightsResult, error) {
		res, err := r.runRights(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// Cash returns the R15 cash and fiscal receipts (reports.all).
func (s *Service) Cash(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*CashResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyCash, true, func(r *run, q Params) (*CashResult, error) {
		res, err := r.runCash(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// Vessels returns the R16 hosted vessels (reports.operational or reports.all).
func (s *Service) Vessels(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*VesselsResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyVessels, false, func(r *run, q Params) (*VesselsResult, error) {
		res, _, err := r.runVessels(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// BerthRevenue returns the R17 revenue per metre of berth (reports.all).
func (s *Service) BerthRevenue(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*BerthRevenueResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyBerthRevenue, true, func(r *run, q Params) (*BerthRevenueResult, error) {
		res, _, err := r.runBerthRevenue(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// access is an authorised report request: Full is true with reports.all (money columns and money reports);
// with reports.operational only it is false.
type access struct {
	Scope store.MarinaScope
	Full  bool
}

// authorize checks reports.all, else (when the report does not need it) reports.operational. Another marina is
// auth.ErrNotFound; a grant without either permission is auth.ErrForbidden.
func (s *Service) authorize(ctx context.Context, p *auth.Principal, marinaID string, needAll bool) (access, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermReportsAll)
	if err == nil {
		return access{Scope: scope, Full: true}, nil
	}
	if needAll || !errors.Is(err, auth.ErrForbidden) {
		return access{}, err
	}
	scope, err = p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermReportsOperational)
	if err != nil {
		return access{}, err
	}
	return access{Scope: scope}, nil
}

// Check authorises p on marinaID for any report (reports.operational or reports.all). Handlers call it before
// answering a parse error (ParseParams), so another marina is still 404 and never 422.
func (s *Service) Check(ctx context.Context, p *auth.Principal, marinaID string) error {
	_, err := s.authorize(ctx, p, marinaID, false)
	return err
}
