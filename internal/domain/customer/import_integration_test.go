//go:build integration

package customer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestImport_CustomersVessels(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		ctx := fixture.Ctx()
		svc := customer.NewService(customer.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: filestore.New(t.TempDir(), e.Keys), Log: e.Log})
		reg := csvimport.NewRegistry()
		reg.Register(svc.ImportHandler())
		env := csvimport.Env{DB: db, Clock: e.Clock, Audit: e.Audit}
		if _, err := svc.Create(ctx, e.Owner, e.MarinaID, customer.Patch{Kind: ptr(customer.KindPerson), LastName: ptr("Verdi"), FiscalCode: ptr("VRDGPP80A01H501U")}); err != nil {
			t.Fatal(err)
		}
		mapping := csvimport.Mapping{
			"first_name": 0, "last_name": 1, "fiscal_code": 2, "email": 3, "vessel_name": 4,
			"vessel_registration": 5, "vessel_loa_cm": 6, "vessel_beam_cm": 7,
		}
		csv := "nome,cognome,cf,email,barca,matricola,loa,beam\n" +
			"Mario,Rossi,RSSMRA80A01H501U,mario@example.test,Libeccio,RM1234,1134,380\n" +
			"Mario,Rossi,RSSMRA80A01H501U,,Grecale,RM5678,900,300\n" +
			"Giuseppe,Verdi,VRDGPP80A01H501U,,Maestrale,NA111,800,280\n" +
			"Luca,,,,,,,\n" +
			"Anna,Neri,,anna@,,,,\n"
		in := csvimport.Input{Entity: csvimport.EntityCustomersVessels, CSV: csv, Delimiter: "comma", HasHeader: true, Mapping: mapping, DryRun: true}
		rep, err := reg.Run(ctx, env, e.Owner, e.MarinaID, in)
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Duplicates) != 1 || rep.Duplicates[0].Row != 4 || len(rep.Rejected) != 2 {
			t.Fatalf("dry run %+v", rep)
		}
		for _, r := range rep.Rejected {
			if r.Ref != "" && r.Ref != "RM1234" {
				t.Fatalf("report leaks data: %+v", r)
			}
		}
		in.CSV = csv[:len(csv)-len("Luca,,,,,,,\nAnna,Neri,,anna@,,,,\n")]
		in.DryRun = false
		rep, err = reg.Run(ctx, env, e.Owner, e.MarinaID, in)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Created["customer"] != 1 || rep.Created["vessel"] != 3 {
			t.Fatalf("apply %+v", rep)
		}
		page, _ := svc.List(ctx, e.Owner, e.MarinaID, customer.Filter{}, "", 50)
		if len(page.Items) != 2 {
			t.Fatalf("%d customers", len(page.Items))
		}
		// A created record changed later blocks the undo.
		var vesselID string
		if err := db.NewSelect().Table("vessels").Column("id").Where("registration = ?", "NA111").Scan(context.Background(), &vesselID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewUpdate().Table("vessels").Set("version = version + 1").Where("id = ?", vesselID).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := reg.Undo(ctx, env, e.Owner, e.MarinaID, rep.RunID, 0); !errors.Is(err, csvimport.ErrNotUndoable) {
			t.Fatalf("undo of a changed import: %v", err)
		}
		if _, err := db.NewUpdate().Table("vessels").Set("version = 1").Where("id = ?", vesselID).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := reg.Undo(ctx, env, e.Owner, e.MarinaID, rep.RunID, 0); err != nil {
			t.Fatal(err)
		}
		page, _ = svc.List(ctx, e.Owner, e.MarinaID, customer.Filter{}, "", 50)
		if len(page.Items) != 1 {
			t.Fatalf("%d customers after undo", len(page.Items))
		}
		runs, _ := reg.List(ctx, env, e.Owner, e.MarinaID, "", "", 50)
		if len(runs.Items) != 2 || runs.Items[0].Status != store.ImportUndone {
			t.Fatalf("runs %+v", runs.Items)
		}
	})
}
