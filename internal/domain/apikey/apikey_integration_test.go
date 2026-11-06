//go:build integration

package apikey_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/apikey"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestAPIKey_CreateAuthenticateRevoke(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		s := apikey.NewService(e.DB, e.Clock, e.Audit, e.Outbox, e.Log)
		ctx := fixture.Ctx()

		secret, key, err := s.Create(ctx, e.Owner, e.MarinaID, apikey.CreateInput{Name: "Gestionale", Scopes: []auth.Permission{auth.PermMarinaRead, auth.PermCustomersRead}})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(secret, "po_live_") || !strings.HasPrefix(secret, key.Prefix+"_") || len(key.Prefix) != len("po_live_")+8 {
			t.Fatalf("secret format %q / prefix %q", secret, key.Prefix)
		}
		row, _ := store.APIKeyByPrefix(context.Background(), db.DB, key.Prefix)
		if strings.Contains(row.KeyHash, secret) || row.KeyHash == secret {
			t.Fatal("secret stored in clear")
		}
		p, err := e.Auth.AuthenticateAPIKey(ctx, secret)
		if err != nil || p.Kind != auth.KindAPIKey || p.APIKeyID != key.ID {
			t.Fatalf("authenticate: %+v %v", p, err)
		}
		if _, err := p.Authorize(ctx, db.DB, e.MarinaID, auth.PermMarinaRead); err != nil {
			t.Fatalf("key reads marina: %v", err)
		}
		if _, err := p.Authorize(ctx, db.DB, e.MarinaID, auth.PermUsersManage); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("key manages users: %v", err)
		}
		if _, err := e.Auth.AuthenticateAPIKey(ctx, secret[:len(secret)-1]+"x"); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatal("tampered key accepted")
		}
		list, _ := s.List(ctx, e.Owner, e.MarinaID)
		if len(list) != 1 || list[0].ID != key.ID {
			t.Fatalf("list %+v", list)
		}
		if err := s.Revoke(ctx, e.Owner, e.MarinaID, key.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Revoke(ctx, e.Owner, e.MarinaID, key.ID); err != nil {
			t.Fatalf("revoke twice: %v", err)
		}
		if _, err := e.Auth.AuthenticateAPIKey(ctx, secret); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatal("revoked key accepted")
		}
		if len(e.Events(t, event.APIKeyCreated)) != 1 || len(e.Events(t, event.APIKeyRevoked)) != 1 {
			t.Fatal("events")
		}
		if err := s.Revoke(ctx, e.Owner, e.MarinaID, store.NewID()); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("revoke unknown: %v", err)
		}
	})
}

func TestAPIKey_CannotHaveUsersManage(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		s := apikey.NewService(e.DB, e.Clock, e.Audit, e.Outbox, e.Log)
		ctx := fixture.Ctx()
		for _, scopes := range [][]auth.Permission{
			{auth.PermUsersManage},
			{auth.PermMarinaRead, auth.PermOrganizationManage},
			{"made.up"},
			{},
		} {
			if _, _, err := s.Create(ctx, e.Owner, e.MarinaID, apikey.CreateInput{Name: "k", Scopes: scopes}); !errors.Is(err, apikey.ErrInvalidScope) {
				t.Errorf("scopes %v: %v", scopes, err)
			}
		}
		// The owner of another organization cannot mint keys for this marina (404, no existence leak).
		_, otherMarina, other := e.OtherOrganization(t)
		if _, _, err := s.Create(ctx, other, e.MarinaID, apikey.CreateInput{Name: "x", Scopes: []auth.Permission{auth.PermMarinaRead}}); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("cross-org create: %v", err)
		}
		past := e.Clock.Now().Add(-time.Hour)
		if _, _, err := s.Create(ctx, other, otherMarina, apikey.CreateInput{Name: "x", Scopes: []auth.Permission{auth.PermMarinaRead}, ExpiresAt: &past}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("expiry in the past: %v", err)
		}
		future := e.Clock.Now().Add(time.Hour)
		secret, _, err := s.Create(ctx, other, otherMarina, apikey.CreateInput{Name: "x", Scopes: []auth.Permission{auth.PermMarinaRead}, ExpiresAt: &future})
		if err != nil {
			t.Fatal(err)
		}
		e.Clock.Advance(2 * time.Hour)
		if _, err := e.Auth.AuthenticateAPIKey(ctx, secret); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatal("expired key accepted")
		}
		viewer := e.AddUser(t, "viewer@porto.test", auth.RoleViewer, e.OrgID, e.MarinaID)
		if _, _, err := s.Create(ctx, viewer, e.MarinaID, apikey.CreateInput{Name: "x", Scopes: []auth.Permission{auth.PermMarinaRead}}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("viewer creates key: %v", err)
		}
	})
}
