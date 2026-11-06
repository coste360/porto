//go:build integration

package team_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/team"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func newSvc(e *fixture.Env) *team.Service {
	return team.NewService(e.DB, e.Clock, e.Auth, e.Audit, e.Outbox, e.Log)
}

func TestTeam_AddMemberInvitesAndAcceptance(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		s := newSvc(e)
		ctx := fixture.Ctx()

		m, err := s.AddMember(ctx, e.Owner, e.MarinaID, team.AddMemberInput{Email: "Nuovo@Porto.test", Name: "Nino Nuovo", Role: auth.RoleOffice, Locale: "en"})
		if err != nil {
			t.Fatal(err)
		}
		if m.Email != "nuovo@porto.test" || !m.InvitationPending || m.Scope != team.ScopeMarina {
			t.Fatalf("member %+v", m)
		}
		if _, err := s.AddMember(ctx, e.Owner, e.MarinaID, team.AddMemberInput{Email: "nuovo@porto.test", Name: "N", Role: auth.RoleDock}); !errors.Is(err, team.ErrAlreadyMember) {
			t.Fatalf("duplicate: %v", err)
		}
		if _, err := s.AddMember(ctx, e.Owner, e.MarinaID, team.AddMemberInput{Email: "x", Name: "", Role: "captain"}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("invalid: %v", err)
		}
		evs := e.Events(t, event.MemberAdded)
		if len(evs) != 1 || evs[0].MarinaID != e.MarinaID || strings.Contains(string(evs[0].Payload), "nuovo") {
			t.Fatalf("events %+v", evs)
		}

		// The invitation mail is queued in the same transaction and delivered by notify.
		if n, err := e.Notify.DeliverDue(context.Background(), 10); err != nil || n != 1 {
			t.Fatalf("deliver: %d %v", n, err)
		}
		mail := e.Mailbox.Messages()[0]
		if mail.To != "nuovo@porto.test" || mail.Template != "invitation" || mail.Locale != "en" || !strings.Contains(mail.Text, "Marina Test") {
			t.Fatalf("mail %+v", mail)
		}
		_, after, ok := strings.Cut(mail.Text, "#token=")
		if !ok {
			t.Fatal("no token link")
		}
		token := strings.Fields(strings.SplitN(after, "&", 2)[0])[0]
		if err := e.Auth.ConfirmPasswordReset(ctx, token, fixture.Password); err != nil {
			t.Fatalf("accept invitation: %v", err)
		}
		member := e.Login(t, "nuovo@porto.test")
		if !member.Can(e.OrgID, e.MarinaID, auth.PermCustomersWrite) || member.Can(e.OrgID, e.MarinaID, auth.PermUsersManage) {
			t.Fatal("office permissions")
		}
		if _, err := s.ListMembers(ctx, member, e.MarinaID); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("office lists members: %v", err)
		}

		list, err := s.ListMembers(ctx, e.Owner, e.MarinaID)
		if err != nil || len(list) != 2 {
			t.Fatalf("list %+v %v", list, err)
		}
		var owner, office team.Member
		for _, l := range list {
			switch l.Scope {
			case team.ScopeOrganization:
				owner = l
			default:
				office = l
			}
		}
		if owner.Role != auth.RoleOwner || office.InvitationPending || office.Role != auth.RoleOffice {
			t.Fatalf("listing %+v", list)
		}
	})
}

func TestTeam_ChangeRoleRemoveAndReset(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		s := newSvc(e)
		ctx := fixture.Ctx()
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)

		m, err := s.ChangeRole(ctx, e.Owner, e.MarinaID, dock.UserID, auth.RoleDirector)
		if err != nil || m.Role != auth.RoleDirector {
			t.Fatalf("change role: %+v %v", m, err)
		}
		if _, err := s.ChangeRole(ctx, e.Owner, e.MarinaID, e.OwnerID, auth.RoleViewer); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("org-level grant via marina route: %v", err)
		}
		if err := s.SendPasswordReset(ctx, e.Owner, e.MarinaID, dock.UserID); err != nil {
			t.Fatal(err)
		}
		if err := s.SendPasswordReset(ctx, e.Owner, e.MarinaID, store.NewID()); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("reset unknown: %v", err)
		}
		if err := s.RemoveMember(ctx, e.Owner, e.MarinaID, dock.UserID); err != nil {
			t.Fatal(err)
		}
		if err := s.RemoveMember(ctx, e.Owner, e.MarinaID, dock.UserID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("remove twice: %v", err)
		}
		if len(e.Events(t, event.MemberRoleChanged)) != 1 || len(e.Events(t, event.MemberRemoved)) != 1 ||
			e.AuditCount(t, "member.password_reset_sent") != 1 {
			t.Fatal("missing events or audit")
		}
		// Other marina of another organization: 404.
		_, otherMarina, _ := e.OtherOrganization(t)
		if _, err := s.ListMembers(ctx, e.Owner, otherMarina); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("cross-org: %v", err)
		}
	})
}

func TestTeam_OrgOwnersAndLastOwner(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		s := newSvc(e)
		ctx := fixture.Ctx()
		if err := s.RemoveOrgOwner(ctx, e.Owner, e.OrgID, e.OwnerID); !errors.Is(err, team.ErrLastOwner) {
			t.Fatalf("remove last owner: %v", err)
		}
		m, err := s.AddOrgOwner(ctx, e.Owner, e.OrgID, team.AddMemberInput{Email: "second@porto.test", Name: "Second Owner", Role: auth.RoleViewer})
		if err != nil || m.Role != auth.RoleOwner || m.Scope != team.ScopeOrganization {
			t.Fatalf("add owner: %+v %v", m, err)
		}
		owners, _ := s.ListOrgOwners(ctx, e.Owner, e.OrgID)
		if len(owners) != 2 {
			t.Fatalf("owners %+v", owners)
		}
		if err := s.RemoveOrgOwner(ctx, e.Owner, e.OrgID, e.OwnerID); err != nil {
			t.Fatalf("remove with a second owner: %v", err)
		}
		marinaDirector := e.AddUser(t, "md@porto.test", auth.RoleOwner, e.OrgID, e.MarinaID)
		if _, err := s.ListOrgOwners(ctx, marinaDirector, e.OrgID); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("marina-level owner lists org owners: %v", err)
		}
	})
}

func TestConcurrency_LastOwnerAlwaysKept(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		s := newSvc(e)
		ctx := fixture.Ctx()
		ids := []string{e.OwnerID}
		for _, email := range []string{"o2@porto.test", "o3@porto.test", "o4@porto.test"} {
			p := e.AddUser(t, email, auth.RoleOwner, e.OrgID, "")
			ids = append(ids, p.UserID)
		}
		var wg sync.WaitGroup
		errs := make(chan error, 100)
		for i := range 100 {
			target := ids[i%len(ids)]
			wg.Go(func() {
				err := s.RemoveOrgOwner(ctx, e.Owner, e.OrgID, target)
				if err != nil && !errors.Is(err, team.ErrLastOwner) && !errors.Is(err, auth.ErrNotFound) {
					errs <- err
				}
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		n, err := store.CountActiveOrgOwners(context.Background(), db.DB, e.OrgID)
		if err != nil || n != 1 {
			t.Fatalf("owners left = %d (%v), want exactly 1", n, err)
		}
	})
}

// Concurrent invitations of the same new email create one user and one membership; the losers see
// ErrAlreadyMember, never an internal error (on PostgreSQL a failed insert would abort the transaction).
func TestConcurrency_AddMemberSameNewEmail(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		s := newSvc(e)
		ctx := fixture.Ctx()
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok := 0
		errs := make(chan error, 100)
		for range 100 {
			wg.Go(func() {
				_, err := s.AddMember(ctx, e.Owner, e.MarinaID, team.AddMemberInput{Email: "race@porto.test", Name: "Rita Race", Role: auth.RoleOffice})
				switch {
				case err == nil:
					mu.Lock()
					ok++
					mu.Unlock()
				case !errors.Is(err, team.ErrAlreadyMember):
					errs <- err
				}
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		if ok != 1 {
			t.Fatalf("successful invitations = %d, want 1", ok)
		}
		n, err := db.DB.NewSelect().Model((*store.User)(nil)).Where("email = ?", "race@porto.test").Count(context.Background())
		if err != nil || n != 1 {
			t.Fatalf("users = %d (%v), want 1", n, err)
		}
	})
}
