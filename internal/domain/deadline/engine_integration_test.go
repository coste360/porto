//go:build integration

package deadline_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

type engineEnv struct {
	*fixture.Env
	dl    *deadline.Service
	cust  *customer.Service
	scope store.MarinaScope
}

func newEngineEnv(t *testing.T, db *store.DB) *engineEnv {
	t.Helper()
	e := fixture.New(t, db)
	files := filestore.New(t.TempDir(), e.Keys)
	dl := deadline.NewService(deadline.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Notify: e.Notify, Log: e.Log})
	scope, err := store.SystemMarinaScope(context.Background(), db.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	cust := customer.NewService(customer.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log})
	return &engineEnv{Env: e, dl: dl, cust: cust, scope: scope}
}

func (e *engineEnv) today() calendar.Date { return calendar.In(e.Clock.Now(), time.UTC) }

func (e *engineEnv) concession(t *testing.T, expiresIn int, obligations ...int) (string, []string) {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(e.Clock.Now())
	c := &store.Concession{
		ID: store.NewID(), MarinaID: e.MarinaID, ExpiresOn: e.today().AddDays(expiresIn), Surfaces: store.JSON("[]"),
		Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := e.DB.NewInsert().Model(c).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, d := range obligations {
		o := &store.ConcessionObligation{
			ID: store.NewID(), MarinaID: e.MarinaID, ConcessionID: c.ID, Kind: "periodic_report",
			DueOn: e.today().AddDays(d), Recurrence: "none", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if _, err := e.DB.NewInsert().Model(o).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, o.ID)
	}
	return c.ID, ids
}

func (e *engineEnv) sync(t *testing.T) deadline.SyncResult {
	t.Helper()
	r, err := e.dl.SyncMarina(fixture.Ctx(), e.scope, audit.System)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (e *engineEnv) rows(t *testing.T) []store.Deadline {
	t.Helper()
	rows, err := store.EngineDeadlines(context.Background(), e.DB.DB, e.scope, deadline.EngineKinds())
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestDeadlines_SyncIdempotent(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEngineEnv(t, db)
		ctx := context.Background()
		_, obl := e.concession(t, 100, 20, 40)
		if r := e.sync(t); r.Created != 3 || r.Updated != 0 || r.Closed != 0 {
			t.Fatalf("first sync %+v", r)
		}
		for range 2 {
			if r := e.sync(t); r != (deadline.SyncResult{}) {
				t.Fatalf("second sync changed %+v", r)
			}
		}
		// A changed due date updates; a done obligation closes; a dismissed row stays dismissed.
		if _, err := db.NewUpdate().Table("concession_obligations").Set("due_on = ?", e.today().AddDays(25)).Where("id = ?", obl[0]).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewUpdate().Table("concession_obligations").Set("done_on = ?", e.today()).Where("id = ?", obl[1]).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if r := e.sync(t); r.Updated != 1 || r.Closed != 1 {
			t.Fatalf("sync after changes %+v", r)
		}
		statuses := map[string]string{}
		for _, d := range e.rows(t) {
			statuses[d.SourceID] = d.Status
		}
		if statuses[obl[0]] != "open" || statuses[obl[1]] != "done" {
			t.Fatalf("statuses %v", statuses)
		}
		if r := e.sync(t); r != (deadline.SyncResult{}) {
			t.Fatalf("not idempotent after changes %+v", r)
		}
		if e.AuditCount(t, "deadline.sync") != 2 {
			t.Fatal("sync runs not audited once each")
		}
	})
}

func TestConcurrency_DeadlineSyncParallel(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEngineEnv(t, db)
		days := make([]int, 60)
		for i := range days {
			days[i] = i + 1
		}
		e.concession(t, 365, days...)
		var wg sync.WaitGroup
		errs := make(chan error, 100)
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := e.dl.SyncMarina(fixture.Ctx(), e.scope, audit.System); err != nil && !store.IsSerializationFailure(err) {
					errs <- err
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		if n := len(e.rows(t)); n != 61 {
			t.Fatalf("%d deadlines, want 61", n)
		}
	})
}

func TestDeadlines_NotifyOncePerLeadDay(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEngineEnv(t, db)
		ctx := fixture.Ctx()
		cs := store.DefaultContractSettings(e.MarinaID, e.Clock.Now())
		enc, err := e.Keys.EncryptString("office@porto.test", "office-email:"+e.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		cs.OfficeEmailEnc = enc
		if _, err := db.NewInsert().Model(cs).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := e.dl.ReplacePolicies(ctx, e.Owner, e.MarinaID, []deadline.Policy{
			{Kind: deadline.KindConcessionObligation, LeadDays: []int{10, 3}, Channels: []string{"panel", "office_email"}},
		}); err != nil {
			t.Fatal(err)
		}
		ps, _ := e.dl.ListPolicies(ctx, e.Owner, e.MarinaID)
		for _, p := range ps {
			if p.Kind == deadline.KindConcessionObligation && len(p.Channels) != 2 {
				t.Fatalf("channels not stored: %+v", p)
			}
		}
		if _, err := e.dl.ReplacePolicies(ctx, e.Owner, e.MarinaID, []deadline.Policy{
			{Kind: deadline.KindConcessionObligation, LeadDays: []int{10}, Channels: []string{"fax"}},
		}); err == nil {
			t.Fatal("unknown channel accepted")
		}
		e.concession(t, 400, 20)
		e.sync(t)
		count := func() int {
			n, err := db.NewSelect().Table("message_log").Where("template = ?", "deadline_notice_office").Count(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			return int(n)
		}
		step := func(days, want int) {
			t.Helper()
			e.Clock.Advance(time.Duration(days) * 24 * time.Hour)
			for range 3 {
				if err := e.dl.RunNotify(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if got := count(); got != want {
				t.Fatalf("after %d more days: %d office notices, want %d", days, got, want)
			}
		}
		step(0, 0)
		step(11, 1) // 9 days left: lead day 10
		step(1, 1)
		step(6, 2) // 2 days left: lead day 3
		step(10, 2)
		if _, err := e.Notify.DeliverDue(context.Background(), 10); err != nil {
			t.Fatal(err)
		}
		for _, m := range e.Mailbox.Messages() {
			if m.To != "office@porto.test" || !strings.Contains(m.Text, "Adempimento concessione") {
				t.Fatalf("notice %q to %s", m.Text, m.To)
			}
		}
	})
}

func TestDeadlines_InsurancePolicyBlock(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEngineEnv(t, db)
		ctx := context.Background()
		now := store.UTC(e.Clock.Now())
		v := &store.Vessel{
			ID: store.NewID(), MarinaID: e.MarinaID, Name: "Libeccio", HullType: "monohull_sail", LOACm: 1100, BeamCm: 380,
			DimensionsSource: "declared", Engines: store.JSON("[]"), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertVessel(ctx, db.DB, v); err != nil {
			t.Fatal(err)
		}
		on := e.today().AddDays(30)
		if ok, err := deadline.InsuranceValid(ctx, db.DB, e.scope, v.ID, on); err != nil || ok {
			t.Fatalf("no policy: %v %v", ok, err)
		}
		d := &store.Document{
			ID: store.NewID(), MarinaID: e.MarinaID, OwnerType: "vessel", VesselID: v.ID, Type: "insurance_policy",
			ExpiresOn: e.today().AddDays(60), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertDocument(ctx, db.DB, d); err != nil {
			t.Fatal(err)
		}
		if ok, _ := deadline.InsuranceValid(ctx, db.DB, e.scope, v.ID, on); !ok {
			t.Fatal("valid policy refused")
		}
		if ok, _ := deadline.InsuranceValid(ctx, db.DB, e.scope, v.ID, e.today().AddDays(61)); ok {
			t.Fatal("expired policy accepted")
		}
	})
}

func TestImport_Deadlines(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEngineEnv(t, db)
		ctx := fixture.Ctx()
		reg := csvimport.NewRegistry()
		reg.Register(e.dl.ImportHandler())
		res, err := e.cust.Create(ctx, e.Owner, e.MarinaID, customer.Patch{
			Kind: ptr(customer.KindPerson), LastName: ptr("Rossi"), FiscalCode: ptr("RSSMRA80A01H501U"),
		})
		if err != nil {
			t.Fatal(err)
		}
		csv := "kind;due_on;cf;note\ninsurance;2027-03-01;rssmra80a01h501u;rinnovo\ncontract_end;2027-13-01;;\nnope;2027-01-01;;\n"
		in := csvimport.Input{
			Entity: csvimport.EntityDeadlines, CSV: csv, HasHeader: true, DryRun: true,
			Mapping: csvimport.Mapping{"kind": 0, "due_on": 1, "customer_fiscal_code": 2, "note": 3},
		}
		rep, err := reg.Run(ctx, e.dl.ImportEnv(), e.Owner, e.MarinaID, in)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Rows != 3 || rep.Valid != 1 || len(rep.Rejected) != 2 || rep.Rejected[0].Row != 3 || rep.Rejected[1].Row != 4 {
			t.Fatalf("dry run %+v", rep)
		}
		in.DryRun = false
		if rep, err = reg.Run(ctx, e.dl.ImportEnv(), e.Owner, e.MarinaID, in); err != nil || len(rep.Created) != 0 {
			t.Fatalf("apply with rejections wrote %+v %v", rep, err)
		}
		in.CSV = "kind;due_on;cf;note\ninsurance;2027-03-01;RSSMRA80A01H501U;rinnovo\n"
		rep, err = reg.Run(ctx, e.dl.ImportEnv(), e.Owner, e.MarinaID, in)
		if err != nil || rep.Created["deadline"] != 1 {
			t.Fatalf("apply %+v %v", rep, err)
		}
		list, _ := e.dl.List(ctx, e.Owner, e.MarinaID, deadline.Filter{}, "", 50)
		if len(list.Items) != 1 || list.Items[0].Customer == nil || list.Items[0].Customer.ID != res.Customer.ID {
			t.Fatalf("imported deadlines %+v", list.Items)
		}
		run, err := reg.Get(ctx, e.dl.ImportEnv(), e.Owner, e.MarinaID, rep.RunID)
		if err != nil || !run.Undoable {
			t.Fatalf("run %+v %v", run, err)
		}
		if _, err := reg.Undo(ctx, e.dl.ImportEnv(), e.Owner, e.MarinaID, rep.RunID, run.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := reg.Undo(ctx, e.dl.ImportEnv(), e.Owner, e.MarinaID, rep.RunID, 0); !errors.Is(err, csvimport.ErrNotUndoable) {
			t.Fatalf("second undo: %v", err)
		}
		list, _ = e.dl.List(ctx, e.Owner, e.MarinaID, deadline.Filter{}, "", 50)
		if len(list.Items) != 0 {
			t.Fatal("undo left deadlines")
		}
	})
}

// TestDeadlines_DueChangeResetsNotified: a new due date of the source clears the lead days already notified, in
// the same scoped, version-checked update that moves the due date.
func TestDeadlines_DueChangeResetsNotified(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEngineEnv(t, db)
		ctx := context.Background()
		_, obl := e.concession(t, 400, 20)
		e.sync(t)
		if _, err := db.NewUpdate().Table("deadlines").Set("notified_at = ?", store.JSON("[10]")).
			Where("source_id = ?", obl[0]).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if r := e.sync(t); r != (deadline.SyncResult{}) {
			t.Fatalf("sync without changes %+v", r)
		}
		if _, err := db.NewUpdate().Table("concession_obligations").Set("due_on = ?", e.today().AddDays(30)).Where("id = ?", obl[0]).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if r := e.sync(t); r.Updated != 1 {
			t.Fatalf("sync after the due change %+v", r)
		}
		for _, d := range e.rows(t) {
			if d.SourceID != obl[0] {
				continue
			}
			if string(d.NotifiedAt) != "[]" || !d.DueOn.Equal(e.today().AddDays(30)) || d.Version != 2 {
				t.Fatalf("deadline after the due change: notified %s due %s version %d", d.NotifiedAt, d.DueOn, d.Version)
			}
		}
	})
}
