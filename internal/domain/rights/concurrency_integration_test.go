//go:build integration

package rights

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/allot"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

const attempts = 100

// TestConcurrency_RightTransferSingle: 100 parallel completions of two transfers of the same right ⇒ exactly one
// completes, one right chain (old right transferred + one new right), owner rows intact.
func TestConcurrency_RightTransferSingle(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		a1 := e.Berths[0]
		c2, c3 := e.AddCustomer(t, "Bianchi"), e.AddCustomer(t, "Verdi")
		r := e.right(t, a1, "2027-01-01", "2027-12-31", e.one(e.Customer), e.Vessel)
		var trs []*Transfer
		for _, c := range []string{c2, c3} {
			tr, err := e.Rights.CreateTransfer(ctx, e.Owner, e.MarinaID, TransferInput{RightID: r.ID, EffectiveOn: d("2027-09-01"), ToHolders: e.one(c), FeesPolicy: FeesReassign})
			if err != nil {
				t.Fatal(err)
			}
			trs = append(trs, tr)
		}
		var ok atomic.Int32
		race(attempts, func(i int) {
			tr := trs[i%2]
			if _, err := e.Rights.CompleteTransfer(ctx, e.Owner, e.MarinaID, tr.ID, tr.Version); err == nil {
				ok.Add(1)
			} else if !isErr(err, ErrInvalidTransition) && !isErr(err, ErrVersionMismatch) && !isErr(err, store.ErrVersionMismatch) {
				t.Errorf("unexpected: %v", err)
			}
		})
		if ok.Load() != 1 {
			t.Fatalf("completions %d", ok.Load())
		}
		if n, _ := store.CountBerthRights(context.Background(), db.DB, e.scope(t)); n != 2 {
			t.Fatalf("rights %d", n)
		}
		if got := e.ownerRows(t, a1); got != restOfYear {
			t.Fatalf("owner rows %d", got)
		}
		if n := e.AuditCount(t, "transfer.complete"); n != 1 {
			t.Fatalf("audit %d", n)
		}
		e.invariants(t)
	})
}

// TestConcurrency_MandateOverlap: 100 parallel overlapping mandates on one right ⇒ one wins.
func TestConcurrency_MandateOverlap(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		a1 := e.Berths[0]
		r := e.right(t, a1, "2027-01-01", "2027-12-31", e.one(e.Customer), "")
		var ok atomic.Int32
		race(attempts, func(i int) {
			from := d("2027-07-01").AddDays(i % 10)
			_, err := e.Rights.CreateMandate(ctx, e.Owner, e.MarinaID, MandateInput{
				RightID: r.ID, FromOn: from, ToOn: from.AddDays(20),
				OwnerShareBP: 5000, Channels: []string{"transit"}, Settlement: "credit",
			})
			if err == nil {
				ok.Add(1)
			} else if !isErr(err, ErrMandateOverlap) && !isErr(err, store.ErrVersionMismatch) {
				t.Errorf("unexpected: %v", err)
			}
		})
		if ok.Load() != 1 {
			t.Fatalf("mandates %d", ok.Load())
		}
		if got := e.ownerRows(t, a1); got != restOfYear-21 {
			t.Fatalf("owner rows %d", got)
		}
		e.invariants(t)
	})
}

// TestConcurrency_FeeRunIssueOnce: 100 parallel issues of the same simulated run ⇒ one issue, instalments once.
func TestConcurrency_FeeRunIssueOnce(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		e.right(t, e.Berths[0], "2027-01-01", "2027-12-31", e.one(e.Customer), "")
		ft := e.feeTable(t, 2027)
		run, err := e.Rights.CreateFeeRun(ctx, e.Owner, e.MarinaID, 2027, ft.ID, []byte(semesters))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Rights.SimulateFeeRun(ctx, e.Owner, e.MarinaID, run.ID, run.Version); err != nil {
			t.Fatal(err)
		}
		cur, _ := e.Rights.GetFeeRun(ctx, e.Owner, e.MarinaID, run.ID)
		var ok atomic.Int32
		race(attempts, func(int) {
			if _, err := e.Rights.IssueFeeRun(ctx, e.Owner, e.MarinaID, run.ID, cur.Version); err == nil {
				ok.Add(1)
			} else if !isErr(err, ErrInvalidTransition) && !isErr(err, ErrVersionMismatch) {
				t.Errorf("unexpected: %v", err)
			}
		})
		if ok.Load() != 1 {
			t.Fatalf("issues %d", ok.Load())
		}
		if rows := e.holderRows(t, store.HolderInstallmentFilter{RunID: run.ID}); len(rows) != 2 || sumOf(rows) != 80_000 {
			t.Fatalf("instalments %d Σ %d", len(rows), sumOf(rows))
		}
	})
}

// TestConcurrency_CommonChargeRunOnce: 100 parallel issues of a balance run and of a competing balance run ⇒ one
// issued balance per budget.
func TestConcurrency_CommonChargeRunOnce(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		bud, items, _ := e.commonSetup(t)
		bud, _ = e.Rights.GetBudget(ctx, e.Owner, e.MarinaID, bud.ID)
		if _, err := e.Rights.SetBudgetActuals(ctx, e.Owner, e.MarinaID, bud.ID, bud.Version, []Actual{{ItemID: items[0].ID, ActualCents: 1000}, {ItemID: items[1].ID, ActualCents: 3}}, nil); err != nil {
			t.Fatal(err)
		}
		var runs []*CommonRun
		for range 2 {
			r, err := e.Rights.CreateCommonRun(ctx, e.Owner, e.MarinaID, bud.ID, CommonRunInput{Kind: RunBalance})
			if err != nil {
				t.Fatal(err)
			}
			runs = append(runs, r)
		}
		var ok atomic.Int32
		race(attempts, func(i int) {
			r := runs[i%2]
			if _, err := e.Rights.IssueCommonRun(ctx, e.Owner, e.MarinaID, r.ID, r.Version); err == nil {
				ok.Add(1)
			} else if !isErr(err, ErrInvalidTransition) && !isErr(err, store.ErrConflict) && !isErr(err, ErrVersionMismatch) {
				t.Errorf("unexpected: %v", err)
			}
		})
		if ok.Load() != 1 {
			t.Fatalf("issued balance runs %d", ok.Load())
		}
	})
}

// TestConcurrency_RightVsContractSameBerth: rights and transits race for the same berth and nights ⇒ never both
// (INV-01 with owner rows), invariants hold.
func TestConcurrency_RightVsContractSameBerth(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		a1 := e.Berths[0]
		var rightsOK, transitsOK atomic.Int32
		race(attempts, func(i int) {
			if i%2 == 0 {
				_, err := e.Rights.CreateRight(ctx, e.Owner, e.MarinaID, RightInput{
					BerthID: a1, Kind: KindMembership, FromOn: d(today),
					ToOn: d("2027-08-31"), Holders: []allot.Share{{CustomerID: e.Customer, Millis: 1000}},
				})
				if err == nil {
					rightsOK.Add(1)
				}
				return
			}
			_, err := e.Contracts.CreateTransit(ctx, e.Owner, e.MarinaID, contract.TransitInput{
				VesselID: e.Vessel, CustomerID: e.Customer, Arrival: d("2027-07-01"), Departure: d("2027-07-04"),
				Placements: []contract.Segment{contracttest.BerthSeg(a1, "2027-07-01", "2027-07-03")}, Confirm: &contract.ManualConfirmation{Reference: "x"},
			})
			if err == nil {
				transitsOK.Add(1)
			}
		})
		if rightsOK.Load()+transitsOK.Load() != 1 {
			t.Fatalf("rights %d transits %d", rightsOK.Load(), transitsOK.Load())
		}
		e.invariants(t)
	})
}

// TestConcurrency_TransferVsHolderSettle (INV-31, holder clause): transfers of a right with fees_policy reassign and
// prorata race 100 goroutines settling and issuing the fee instalments of that right. Every transfer either
// completes or fails with 412, and C31h finds no fee line whose live holder instalments differ from its amount
// (an instalment paid or issued during the transfer is never replanned to the new holders as well).
func TestConcurrency_TransferVsHolderSettle(t *testing.T) {
	for _, policy := range []string{FeesReassign, FeesProRata} {
		t.Run(policy, func(t *testing.T) {
			testdb.Run(t, func(t *testing.T, db *store.DB) {
				e := newEnv(t, db)
				c2, c3 := e.AddCustomer(t, "Bianchi"), e.AddCustomer(t, "Verdi")
				r := e.right(t, e.Berths[0], "2027-01-01", "2027-12-31", []allot.Share{{CustomerID: e.Customer, Millis: 500}, {CustomerID: c2, Millis: 500}}, e.Vessel)
				e.issueFees(t, e.feeTable(t, 2027).ID, semesters)
				rows := e.holderRows(t, store.HolderInstallmentFilter{RightID: r.ID, Status: store.InstallmentScheduled})
				if len(rows) != 4 {
					t.Fatalf("fee instalments %d", len(rows))
				}
				tr, err := e.Rights.CreateTransfer(ctx, e.Owner, e.MarinaID, TransferInput{
					RightID: r.ID, EffectiveOn: d("2027-06-15"), ToHolders: e.one(c3), FeesPolicy: policy,
				})
				if err != nil {
					t.Fatal(err)
				}
				scope := e.scope(t)
				var completed, settled atomic.Int32
				race(attempts, func(i int) {
					switch {
					case i%10 == 0:
						_, err := e.Rights.CompleteTransfer(ctx, e.Owner, e.MarinaID, tr.ID, tr.Version)
						if err == nil {
							completed.Add(1)
						} else if !isErr(err, ErrInvalidTransition) && !isErr(err, ErrVersionMismatch) && !isErr(err, store.ErrVersionMismatch) {
							t.Errorf("transfer: %v", err)
						}
					case i%3 == 0:
						err := db.RunInTx(context.Background(), func(ctx context.Context, tx bun.Tx) error {
							_, err := e.Rights.IssueDueTx(ctx, tx, scope, d("2027-06-30"), 100)
							return err
						})
						if err != nil {
							t.Errorf("issue: %v", err)
						}
					default:
						h := rows[i%len(rows)]
						_, err := e.Rights.SettleHolderInstallment(ctx, e.Owner, e.MarinaID, h.ID, 0, SettleInput{Method: "cash"})
						if err == nil {
							settled.Add(1)
						} else if !isErr(err, ErrInvalidTransition) && !isErr(err, ErrVersionMismatch) && !isErr(err, store.ErrVersionMismatch) &&
							!isErr(err, store.ErrConflict) {
							t.Errorf("settle: %v", err)
						}
					}
				})
				if completed.Load() > 1 {
					t.Fatalf("completions %d", completed.Load())
				}
				if settled.Load() == 0 && completed.Load() == 0 {
					t.Fatal("nothing happened")
				}
				e.sharesInvariant(t)
			})
		})
	}
}

// TestRights_HolderIssueAndSettleWaitForRightLock: settling or issuing a holder instalment waits for the per-right
// lock held by a transfer (PostgreSQL row lock; SQLite serialises writers anyway), so a row the transfer cancels
// meanwhile is not paid or issued behind its back. Without the lock both calls would succeed at once on the row
// the transfer is about to replan.
func TestRights_HolderIssueAndSettleWaitForRightLock(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		if !store.IsPostgres(db.DB) {
			t.Skip("row locks are PostgreSQL only")
		}
		e := newEnv(t, db)
		r := e.right(t, e.Berths[0], "2027-01-01", "2027-12-31", e.one(e.Customer), "")
		e.issueFees(t, e.feeTable(t, 2027).ID, semesters)
		rows := e.holderRows(t, store.HolderInstallmentFilter{RightID: r.ID, Status: store.InstallmentScheduled})
		if len(rows) != 2 {
			t.Fatalf("fee instalments %d", len(rows))
		}
		bg := context.Background()
		scope := e.scope(t)
		for _, op := range []string{"settle", "issue"} {
			t.Run(op, func(t *testing.T) {
				h := rows[0]
				if op == "issue" {
					h = rows[1]
					if _, err := db.NewUpdate().TableExpr("holder_installments").Set("issue_on = ?", d("2027-06-10")).Where("id = ?", h.ID).Exec(bg); err != nil {
						t.Fatal(err)
					}
				}
				tx, err := db.BeginTx(bg, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = tx.Rollback() }()
				if _, err := store.LockBerthRight(bg, tx, scope, r.ID); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() {
					if op == "settle" {
						_, err := e.Rights.SettleHolderInstallment(ctx, e.Owner, e.MarinaID, h.ID, 0, SettleInput{Method: "cash"})
						done <- err
						return
					}
					done <- db.RunInTx(bg, func(ctx context.Context, itx bun.Tx) error {
						n, err := e.Rights.IssueDueTx(ctx, itx, scope, d("2027-06-30"), 100)
						if err == nil && n != 0 {
							err = errors.New("issued a row cancelled under the right lock")
						}
						return err
					})
				}()
				select {
				case err := <-done:
					t.Fatalf("%s did not wait for the right lock: %v", op, err)
				case <-time.After(300 * time.Millisecond):
				}
				if n, err := store.CancelHolderInstallments(bg, tx, scope, []string{h.ID}, contracttest.T0); err != nil || n != 1 {
					t.Fatalf("cancel %d %v", n, err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
				err = <-done
				if op == "settle" && !isErr(err, ErrVersionMismatch) && !isErr(err, ErrInvalidTransition) && !isErr(err, store.ErrVersionMismatch) {
					t.Fatalf("settle of a cancelled row: %v", err)
				}
				if op == "issue" && err != nil {
					t.Fatal(err)
				}
				if got, _ := store.GetHolderInstallment(bg, db.DB, scope, h.ID); got.Status != store.InstallmentCancelled {
					t.Fatalf("status %s", got.Status)
				}
			})
		}
	})
}
