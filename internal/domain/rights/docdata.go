package rights

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Print data of the rights kinds (m4-plan §6.2): holder_chain (subject: berth), mandate_statement (mandate),
// fee_notice (a holder instalment of a fee line), common_charges_holder (a holder instalment of a common run),
// common_charges_items (common run) and transfer (right transfer). The views are the frozen docs/views structs.

// DataSource returns the docs data source of the rights kinds (registered by internal/app).
func (s *Service) DataSource() docs.DataSource { return dataSource{s} }

type dataSource struct{ s *Service }

func (dataSource) Kinds() []docs.Kind {
	return []docs.Kind{
		docs.KindHolderChain, docs.KindMandateStatement, docs.KindFeeNotice, docs.KindCommonChargesHolder,
		docs.KindCommonChargesItems, docs.KindTransfer,
	}
}

// View fills the view of a subject inside the compose transaction.
func (ds dataSource) View(ctx context.Context, tx bun.IDB, scope store.MarinaScope, kind docs.Kind, _ docs.SubjectType, subjectID, _ string) (any, error) {
	s := ds.s
	switch kind {
	case docs.KindHolderChain:
		return s.holderChainView(ctx, tx, scope, subjectID)
	case docs.KindMandateStatement:
		st, err := s.statementTx(ctx, tx, scope, subjectID, calendar.Date{}, calendar.Date{})
		if err != nil {
			return nil, err
		}
		return s.statementView(ctx, tx, scope, st)
	case docs.KindTransfer:
		return s.transferDocView(ctx, tx, scope, subjectID)
	case docs.KindCommonChargesItems:
		return s.itemsStatementTx(ctx, tx, scope, subjectID)
	case docs.KindCommonChargesHolder, docs.KindFeeNotice:
		h, err := store.GetHolderInstallment(ctx, tx, scope, subjectID)
		if err != nil {
			return nil, err
		}
		if kind == docs.KindFeeNotice {
			return s.feeNoticeView(ctx, tx, scope, h)
		}
		vs, err := s.holderStatementsTx(ctx, tx, scope, h.RunID, h.CustomerID)
		if err != nil {
			return nil, err
		}
		if len(vs) == 0 {
			return nil, store.ErrNotFound
		}
		return vs[0], nil
	}
	return nil, docs.ErrNoDataSource
}

// DemoView returns demo data for template previews (no database access).
func (dataSource) DemoView(kind docs.Kind, _ string) (any, error) {
	marina := views.Marina{Name: "Marina Demo", LegalName: "Marina Demo S.r.l.", ConcessionExpiresOn: calendar.MustParse("2056-12-31")}
	holder := views.Party{Code: "C-0001", Kind: "person", DisplayName: "Rossi Mario", FirstName: "Mario", LastName: "Rossi"}
	right := views.RightInfo{
		Kind: KindOwnershipShare, Berth: views.Place{Code: "F12", Type: "berth", Length: 1200, Width: 400},
		FromOn: calendar.MustParse("2020-01-01"), ToOn: calendar.MustParse("2056-12-30"), TitleDeed: "Atto di cessione", TitleNumber: "1234/2020",
	}
	inst := []views.Installment{
		{Seq: 1, Kind: "installment", IssueOn: calendar.MustParse("2027-01-01"), DueOn: calendar.MustParse("2027-01-31"), Amount: 60_000, Status: "issued"},
		{Seq: 2, Kind: "installment", IssueOn: calendar.MustParse("2027-06-01"), DueOn: calendar.MustParse("2027-06-30"), Amount: 60_000, Status: "scheduled"},
	}
	switch kind {
	case docs.KindHolderChain:
		return views.HolderChainView{Marina: marina, Berth: right.Berth, Entries: []views.HolderChainEntry{
			{Right: right, Status: RightActive, Holders: []views.Holder{{Party: holder, Share: 1000}}},
		}}, nil
	case docs.KindMandateStatement:
		return views.MandateStatementView{
			Marina: marina, Holder: holder, Right: right, From: calendar.MustParse("2027-07-01"),
			To: calendar.MustParse("2027-08-31"), OwnerShareBP: 6000, MinGuaranteed: 100_000,
			Lines: []views.MandateLine{{
				ContractCode: "TRN-2027-0042", FirstNight: calendar.MustParse("2027-07-10"),
				LastNight: calendar.MustParse("2027-07-16"), Nights: 7, Revenue: 70_000, OwnerShare: 42_000,
			}},
			Revenue: 70_000, OwnerShare: 42_000, TopUp: 58_000,
		}, nil
	case docs.KindFeeNotice:
		return views.FeeNoticeView{
			Marina: marina, Holder: holder, Right: right, Year: 2027, Base: 120_000, IstatBP: 170,
			Updated: 122_040, Days: 365, Amount: 122_040, Share: 1000, HolderAmount: 122_040, Installments: inst,
		}, nil
	case docs.KindCommonChargesHolder:
		return views.CommonChargesHolderView{Marina: marina, Holder: holder, Year: 2027, Kind: RunAdvance, Lines: []views.CommonChargeLine{
			{Item: "Pulizia pontili", Table: "GEN", Berth: "F12", Millis: 3, ItemAmount: 1_234_567, BerthShare: 3_704, HolderAmount: 3_704},
		}, Total: 3_704, Installments: inst[:1]}, nil
	case docs.KindCommonChargesItems:
		return views.CommonChargesItemsView{Marina: marina, Year: 2027, Kind: RunAdvance, Items: []views.CommonChargeItemTotal{
			{Item: "Pulizia pontili", Table: "GEN", Amount: 1_234_567, Allocated: 1_234_567, MarinaShare: 61_728},
		}, Total: 1_234_567}, nil
	case docs.KindTransfer:
		return views.TransferView{
			Marina: marina, Right: right, EffectiveOn: calendar.MustParse("2027-09-01"),
			From:          []views.Holder{{Party: holder, Share: 1000}},
			To:            []views.Holder{{Party: views.Party{Code: "C-0002", Kind: "person", DisplayName: "Bianchi Anna"}, Share: 1000}},
			DeclaredPrice: 4_500_000, TakeoverFee: 50_000,
		}, nil
	}
	return nil, docs.ErrNoDataSource
}

func marinaView(ctx context.Context, db bun.IDB, scope store.MarinaScope) (views.Marina, error) {
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil {
		return views.Marina{}, err
	}
	return views.Marina{Name: m.Name, ConcessionExpiresOn: m.ConcessionExpiresOn}, nil
}

func partyOf(c *store.Customer) views.Party {
	return views.Party{
		Kind: c.Kind, DisplayName: displayName(c), FirstName: c.FirstName, LastName: c.LastName,
		CompanyName: c.CompanyName, FiscalCode: c.FiscalCode, VATNumber: c.VATNumber,
	}
}

func parties(ctx context.Context, db bun.IDB, scope store.MarinaScope, ids []string) (map[string]views.Party, error) {
	cs, err := store.CustomersByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	out := map[string]views.Party{}
	for i := range cs {
		out[cs[i].ID] = partyOf(&cs[i])
	}
	return out, nil
}

func rightInfo(r *store.BerthRight, b *store.Berth) views.RightInfo {
	var t Title
	_ = json.Unmarshal(r.Title, &t)
	ri := views.RightInfo{Kind: r.Kind, FromOn: r.FromOn, ToOn: r.ToOn, TitleDeed: t.Deed, TitleNumber: t.Number, TitleNotary: t.Notary, TitleDate: t.Date}
	if b != nil {
		ri.Berth = views.Place{Code: b.Code, Type: "berth", Length: views.Cm(b.LengthCm), Width: views.Cm(b.WidthCm), FirstNight: r.FromOn, LastNight: r.ToOn}
	}
	return ri
}

func (s *Service) holderChainView(ctx context.Context, tx bun.IDB, scope store.MarinaScope, berthID string) (*views.HolderChainView, error) {
	h, err := s.historyTx(ctx, tx, scope, nil, berthID)
	if err != nil {
		return nil, err
	}
	b, err := store.BerthByID(ctx, tx, scope, berthID)
	if err != nil {
		return nil, err
	}
	m, err := marinaView(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range h.Entries {
		for _, x := range e.Right.Holders {
			ids = append(ids, x.CustomerID)
		}
	}
	ps, err := parties(ctx, tx, scope, ids)
	if err != nil {
		return nil, err
	}
	out := &views.HolderChainView{Marina: m, Berth: views.Place{Code: b.Code, Type: "berth", Length: views.Cm(b.LengthCm), Width: views.Cm(b.WidthCm)}, Entries: []views.HolderChainEntry{}}
	for _, e := range h.Entries {
		r, err := store.GetBerthRight(ctx, tx, scope, e.Right.ID)
		if err != nil {
			return nil, err
		}
		ce := views.HolderChainEntry{Right: rightInfo(r, b), Status: e.Right.Status, Holders: []views.Holder{}}
		for _, x := range e.Right.Holders {
			ce.Holders = append(ce.Holders, views.Holder{Party: ps[x.CustomerID], Share: views.Millis(x.ShareMillis)})
		}
		out.Entries = append(out.Entries, ce)
	}
	return out, nil
}

func (s *Service) statementView(ctx context.Context, tx bun.IDB, scope store.MarinaScope, st *Statement) (*views.MandateStatementView, error) {
	m, err := marinaView(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	r, err := store.GetBerthRight(ctx, tx, scope, st.Mandate.RightID)
	if err != nil {
		return nil, err
	}
	b, err := store.BerthByID(ctx, tx, scope, r.BerthID)
	if err != nil {
		return nil, err
	}
	hs, err := store.HoldersOfRights(ctx, tx, scope, []string{r.ID})
	if err != nil {
		return nil, err
	}
	out := &views.MandateStatementView{
		Marina: m, Right: rightInfo(r, b), From: st.From, To: st.To, OwnerShareBP: views.BP(st.Mandate.OwnerShareBP),
		MinGuaranteed: money.Cents(st.Mandate.MinGuaranteedCents), Lines: []views.MandateLine{}, Revenue: money.Cents(st.RevenueCents),
		OwnerShare: money.Cents(st.OwnerShareCents), TopUp: money.Cents(st.GuaranteedTopUpCents),
	}
	if len(hs) > 0 {
		ps, err := parties(ctx, tx, scope, []string{hs[0].CustomerID})
		if err != nil {
			return nil, err
		}
		out.Holder = ps[hs[0].CustomerID]
	}
	for _, l := range st.Lines {
		out.Lines = append(out.Lines, views.MandateLine{
			ContractCode: l.ContractCode, FirstNight: l.FirstNight, LastNight: l.LastNight,
			Nights: l.Nights, Revenue: money.Cents(l.RevenueCents), OwnerShare: money.Cents(l.OwnerShareCents),
		})
	}
	return out, nil
}

func (s *Service) transferDocView(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string) (*views.TransferView, error) {
	t, err := store.GetRightTransfer(ctx, tx, scope, id)
	if err != nil {
		return nil, err
	}
	r, err := store.GetBerthRight(ctx, tx, scope, t.RightID)
	if err != nil {
		return nil, err
	}
	b, err := store.BerthByID(ctx, tx, scope, r.BerthID)
	if err != nil {
		return nil, err
	}
	m, err := marinaView(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	hs, err := store.HoldersOfRights(ctx, tx, scope, []string{r.ID})
	if err != nil {
		return nil, err
	}
	to := toHolderShares(t.ToHolders)
	var ids []string
	for _, h := range hs {
		ids = append(ids, h.CustomerID)
	}
	for _, h := range to {
		ids = append(ids, h.CustomerID)
	}
	ps, err := parties(ctx, tx, scope, ids)
	if err != nil {
		return nil, err
	}
	out := &views.TransferView{
		Marina: m, Right: rightInfo(r, b), EffectiveOn: t.EffectiveOn, From: []views.Holder{}, To: []views.Holder{},
		TakeoverFee: money.Cents(t.TakeoverFeeCents),
	}
	if t.DeclaredPriceCents != nil {
		out.DeclaredPrice = money.Cents(*t.DeclaredPriceCents)
	}
	for _, h := range hs {
		out.From = append(out.From, views.Holder{Party: ps[h.CustomerID], Share: views.Millis(h.ShareMillis)})
	}
	for _, h := range to {
		out.To = append(out.To, views.Holder{Party: ps[h.CustomerID], Share: views.Millis(h.Millis)})
	}
	return out, nil
}

func installmentsOf(rows []store.HolderInstallment) []views.Installment {
	out := []views.Installment{}
	for _, h := range rows {
		if h.Status == store.InstallmentCancelled {
			continue
		}
		out = append(out, views.Installment{Seq: h.Seq, Kind: h.Kind, IssueOn: h.IssueOn, DueOn: h.DueOn, Amount: money.Cents(h.AmountCents), Status: h.Status})
	}
	return out
}

func (s *Service) feeNoticeView(ctx context.Context, tx bun.IDB, scope store.MarinaScope, h *store.HolderInstallment) (*views.FeeNoticeView, error) {
	if h.SourceType != SourceManagementFee {
		return nil, store.ErrNotFound
	}
	l, err := store.GetFeeLine(ctx, tx, scope, h.SourceID)
	if err != nil {
		return nil, err
	}
	run, err := store.GetFeeRun(ctx, tx, scope, l.RunID)
	if err != nil {
		return nil, err
	}
	t, err := store.GetFeeTable(ctx, tx, scope, run.TableID)
	if err != nil {
		return nil, err
	}
	r, err := store.GetBerthRight(ctx, tx, scope, l.RightID)
	if err != nil {
		return nil, err
	}
	b, err := store.BerthByID(ctx, tx, scope, l.BerthID)
	if err != nil {
		return nil, err
	}
	m, err := marinaView(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	ps, err := parties(ctx, tx, scope, []string{h.CustomerID})
	if err != nil {
		return nil, err
	}
	var lh []LineHolder
	_ = json.Unmarshal(l.Holders, &lh)
	out := &views.FeeNoticeView{
		Marina: m, Holder: ps[h.CustomerID], Right: rightInfo(r, b), Year: run.Year, Base: money.Cents(l.BaseCents),
		IstatBP: views.BP(t.IstatBP), Updated: money.Cents(l.BaseCents).MulRat(10_000+t.IstatBP, 10_000), Days: l.Days, Amount: money.Cents(l.AmountCents),
	}
	for _, x := range lh {
		if x.CustomerID == h.CustomerID {
			out.Share, out.HolderAmount = views.Millis(x.ShareMillis), money.Cents(x.AmountCents)
		}
	}
	rows, err := store.AllHolderInstallments(ctx, tx, scope, store.HolderInstallmentFilter{CustomerID: h.CustomerID, RunID: run.ID})
	if err != nil {
		return nil, err
	}
	var mine []store.HolderInstallment
	for _, x := range rows {
		if x.SourceID == l.ID {
			mine = append(mine, x)
		}
	}
	out.Installments = installmentsOf(mine)
	return out, nil
}

// ---- common charges statements ----

// HolderStatements returns the statement of every holder of a run (customerID set: only that holder)
// (rights.manage).
func (s *Service) HolderStatements(ctx context.Context, p *auth.Principal, marinaID, runID, customerID string) ([]views.CommonChargesHolderView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out []views.CommonChargesHolderView
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		out, err = s.holderStatementsTx(ctx, tx, scope, runID, customerID)
		return err
	})
	return out, err
}

// ItemsStatement returns the per-item statement of a run (rights.manage).
func (s *Service) ItemsStatement(ctx context.Context, p *auth.Principal, marinaID, runID string) (*views.CommonChargesItemsView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *views.CommonChargesItemsView
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		out, err = s.itemsStatementTx(ctx, tx, scope, runID)
		return err
	})
	return out, err
}

func (s *Service) runContext(ctx context.Context, tx bun.IDB, scope store.MarinaScope, runID string) (*store.CommonChargeRun, *store.CommonChargeBudget, map[string]store.CommonChargeItem, map[string]store.MillesimalTable, map[string]map[string]int64, error) {
	r, err := store.GetCommonRun(ctx, tx, scope, runID)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	b, err := store.GetBudget(ctx, tx, scope, r.BudgetID)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	items, err := store.ItemsOfBudget(ctx, tx, scope, b.ID)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	byID := map[string]store.CommonChargeItem{}
	var tids []string
	for _, it := range items {
		byID[it.ID] = it
		tids = append(tids, it.TableID)
	}
	tables := map[string]store.MillesimalTable{}
	for _, id := range tids {
		if _, ok := tables[id]; ok {
			continue
		}
		t, err := store.GetMillesimalTable(ctx, tx, scope, id)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		tables[id] = *t
	}
	es, err := store.EntriesOfTables(ctx, tx, scope, tids)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	millis := map[string]map[string]int64{}
	for tid, list := range es {
		millis[tid] = map[string]int64{}
		for _, e := range list {
			millis[tid][e.BerthID] = e.Millis
		}
	}
	return r, b, byID, tables, millis, nil
}

func (s *Service) holderStatementsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, runID, customerID string) ([]views.CommonChargesHolderView, error) {
	r, b, items, tables, millis, err := s.runContext(ctx, tx, scope, runID)
	if err != nil {
		return nil, err
	}
	allocs, err := store.AllocationsOfRun(ctx, tx, scope, r.ID, store.AllocationFilterCommon{CustomerID: customerID})
	if err != nil {
		return nil, err
	}
	m, err := marinaView(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	var cids, bids []string
	for _, a := range allocs {
		if a.CustomerID != "" {
			cids = append(cids, a.CustomerID)
			bids = append(bids, a.BerthID)
		}
	}
	ps, err := parties(ctx, tx, scope, cids)
	if err != nil {
		return nil, err
	}
	bs, err := store.RightsBerthsByIDs(ctx, tx, scope, bids)
	if err != nil {
		return nil, err
	}
	insts, err := store.AllHolderInstallments(ctx, tx, scope, store.HolderInstallmentFilter{RunID: r.ID, CustomerID: customerID})
	if err != nil {
		return nil, err
	}
	byCustomer := map[string]*views.CommonChargesHolderView{}
	var order []string
	for _, a := range allocs {
		if a.CustomerID == "" {
			continue
		}
		v := byCustomer[a.CustomerID]
		if v == nil {
			v = &views.CommonChargesHolderView{Marina: m, Holder: ps[a.CustomerID], Year: b.Year, Kind: r.Kind, Lines: []views.CommonChargeLine{}}
			byCustomer[a.CustomerID] = v
			order = append(order, a.CustomerID)
		}
		it := items[a.ItemID]
		v.Lines = append(v.Lines, views.CommonChargeLine{
			Item: it.Name, Table: tables[it.TableID].Code, Berth: bs[a.BerthID].Code, Millis: views.Millis(millis[it.TableID][a.BerthID]),
			ItemAmount: itemAmount(r, it), BerthShare: money.Cents(a.BerthShareCents), HolderAmount: money.Cents(a.AmountCents),
		})
		v.Total += money.Cents(a.AmountCents)
	}
	slices.SortFunc(order, func(a, b string) int { return strings.Compare(ps[a].DisplayName, ps[b].DisplayName) })
	out := make([]views.CommonChargesHolderView, 0, len(order))
	for _, cid := range order {
		v := byCustomer[cid]
		var mine []store.HolderInstallment
		for _, h := range insts {
			if h.CustomerID == cid {
				mine = append(mine, h)
			}
		}
		v.Installments = installmentsOf(mine)
		out = append(out, *v)
	}
	return out, nil
}

// itemAmount is the amount of an item in a run as stored in the run totals.
func itemAmount(r *store.CommonChargeRun, it store.CommonChargeItem) money.Cents {
	t := commonRunOf(r)
	for _, x := range t.Totals.Items {
		if x.ItemID == it.ID {
			return money.Cents(x.AmountCents)
		}
	}
	return 0
}

func (s *Service) itemsStatementTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, runID string) (*views.CommonChargesItemsView, error) {
	r, b, items, tables, _, err := s.runContext(ctx, tx, scope, runID)
	if err != nil {
		return nil, err
	}
	m, err := marinaView(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	cr := commonRunOf(r)
	out := &views.CommonChargesItemsView{Marina: m, Year: b.Year, Kind: r.Kind, Items: []views.CommonChargeItemTotal{}}
	for _, t := range cr.Totals.Items {
		it := items[t.ItemID]
		out.Items = append(out.Items, views.CommonChargeItemTotal{
			Item: it.Name, Table: tables[it.TableID].Code,
			Amount: money.Cents(t.AmountCents), Allocated: money.Cents(t.AllocatedCents), MarinaShare: money.Cents(t.MarinaShareCents),
		})
		out.Total += money.Cents(t.AllocatedCents)
	}
	return out, nil
}

// ---- PDF ----

// viewRenderer is the synchronous renderer of a view (DOCUMENTS: docs.Service renders a built-in or marina
// template of a kind on data, without storing a document; used by the statement and holder chain downloads).
type viewRenderer interface {
	RenderView(ctx context.Context, scope store.MarinaScope, kind docs.Kind, locale string, view any) ([]byte, error)
}

// RenderPDF renders a rights view as PDF (rights.manage). docs.ErrNotReady when the renderer is not available.
func (s *Service) RenderPDF(ctx context.Context, p *auth.Principal, marinaID string, kind docs.Kind, view any) ([]byte, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	r, ok := any(s.d.Docs).(viewRenderer)
	if !ok || s.d.Docs == nil {
		return nil, fmt.Errorf("%w: no synchronous renderer", docs.ErrNotReady)
	}
	return r.RenderView(ctx, scope, kind, "", view)
}

// HolderChainView returns the print view of the holder chain of a berth (rights.manage).
func (s *Service) HolderChainView(ctx context.Context, p *auth.Principal, marinaID, berthID string) (*views.HolderChainView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *views.HolderChainView
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		out, err = s.holderChainView(ctx, tx, scope, berthID)
		return err
	})
	return out, err
}

// MandateStatementView returns the print view of a mandate statement (rights.manage).
func (s *Service) MandateStatementView(ctx context.Context, p *auth.Principal, marinaID, id string, from, to calendar.Date) (*views.MandateStatementView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *views.MandateStatementView
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		st, err := s.statementTx(ctx, tx, scope, id, from, to)
		if err != nil {
			return err
		}
		out, err = s.statementView(ctx, tx, scope, st)
		return err
	})
	return out, err
}
