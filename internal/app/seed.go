package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/team"
	"github.com/coste360/porto/internal/store"
)

// Demo data (m0-plan §1.4).
const (
	DemoOrganization = "Porto Demo"
	DemoMarinaName   = "Marina Demo"
	DemoMarinaSlug   = "demo"
	DemoEmailDomain  = "demo.porto.test"
)

// SeedResult describes the demo data.
type SeedResult struct {
	Created        bool // false when the demo marina already existed (nothing was changed)
	OrganizationID string
	MarinaID       string
	OwnerEmail     string
	Password       string // the password of every demo user; generated when none was given
	Users          []string
}

// SeedDemo creates a minimal demo: organization "Porto Demo", marina "Marina Demo" (slug demo) and one user per
// role (<role>@demo.porto.test), all with the same password. It is a no-op when the demo marina exists. When
// password is empty a random one is generated and returned (shown once by the CLI).
func (a *App) SeedDemo(ctx context.Context, password string) (*SeedResult, error) {
	if m, err := store.MarinaBySlug(ctx, a.DB.DB, DemoMarinaSlug); err == nil {
		return &SeedResult{OrganizationID: m.OrganizationID, MarinaID: m.ID}, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if password == "" {
		p, err := crypto.RandomToken(15)
		if err != nil {
			return nil, err
		}
		password = p
	}
	owner := string(auth.RoleOwner) + "@" + DemoEmailDomain
	res, err := a.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
		Email: owner, Name: "Demo Owner", Password: password, Organization: DemoOrganization,
		MarinaName: DemoMarinaName, MarinaSlug: DemoMarinaSlug, Timezone: "Europe/Rome", Locale: "it",
	})
	if err != nil {
		return nil, fmt.Errorf("seed: owner: %w", err)
	}
	out := &SeedResult{
		Created: true, OrganizationID: res.OrganizationID, MarinaID: res.MarinaID, OwnerEmail: owner,
		Password: password, Users: []string{owner},
	}
	p := &auth.Principal{
		Kind: auth.KindUser, UserID: res.UserID,
		Grants: []auth.Grant{{OrganizationID: res.OrganizationID, Role: auth.RoleOwner}},
	}
	for _, role := range auth.Roles() {
		if role == auth.RoleOwner {
			continue
		}
		email := string(role) + "@" + DemoEmailDomain
		m, err := a.Team.AddMember(ctx, p, res.MarinaID, team.AddMemberInput{
			Email: email, Name: "Demo " + string(role), Locale: "it", Role: role,
		})
		if err != nil {
			return nil, fmt.Errorf("seed: %s: %w", role, err)
		}
		if err := a.Auth.SetPassword(ctx, m.UserID, password); err != nil {
			return nil, fmt.Errorf("seed: %s password: %w", role, err)
		}
		out.Users = append(out.Users, email)
	}
	return out, nil
}
