//go:build integration

package audit_test

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestWriter_WriteRedactsAndCarriesRequest(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		c := clock.NewFake(time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC))
		w := audit.NewWriter(c)
		ctx := reqctx.With(context.Background(), &reqctx.Info{RequestID: "req-42", ClientIP: netip.MustParseAddr("203.0.113.5")})
		err := w.Write(ctx, db.DB, audit.Entry{
			Actor: audit.Actor{Type: audit.ActorUser, ID: "u1"}, Action: "member.add", EntityType: "user", EntityID: "u2",
			Diff: map[string]any{"role": "dock", "email": "x@y.z", "token": "secret"},
		})
		if err != nil {
			t.Fatal(err)
		}
		var rows []store.AuditEntry
		if err := db.NewSelect().Model(&rows).Scan(ctx); err != nil {
			t.Fatal(err)
		}
		r := rows[0]
		if len(rows) != 1 || r.RequestID != "req-42" || r.IP != "203.0.113.5" || r.ActorType != "user" ||
			string(r.Diff.Compact()) != `{"role":"dock"}` || !r.CreatedAt.Equal(c.Now()) {
			t.Fatalf("row %+v diff %s", r, r.Diff)
		}
		if err := w.Write(ctx, db.DB, audit.Entry{}); err == nil {
			t.Fatal("empty entry accepted")
		}
	})
}
