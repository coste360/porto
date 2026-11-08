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

	// M1 counts of the data created by this run (0 when that part already existed, m1-plan §6).
	Maps      int
	Berths    int
	Customers int
	Vessels   int
	Documents int

	// M2 counts (0 when the pricing part already existed, m2-plan §6).
	Items      int
	PriceLists int
	PriceRules int

	// M3 counts (0 when the occupancy part already existed, m3-plan §10).
	Contracts   int // mooring contracts
	Transits    int
	Quotes      int
	Inquiries   int
	Allocations int
	BerthRows   int // berth_occupancies rows
	CellRows    int // cell_occupancies rows
	LandRows    int // land_occupancies rows
	Releases    int
	Presences   int

	// M4 counts (0 when that part already existed, m4-plan §10).
	Installments       int
	RenewalCampaigns   int
	RenewalProposals   int
	Charters           int
	Addenda            int
	WaitlistEntries    int
	Rights             int
	Mandates           int
	Transfers          int
	FeeRuns            int
	MillesimalTables   int
	CommonChargeRuns   int
	HolderInstallments int
	Templates          int
	GeneratedDocuments int
	Communications     int
	Deadlines          int

	// M5 counts (0 when that part already existed, m5-plan §10).
	Charges          int
	ChargeExports    int
	OpeningBalances  int
	Payments         int
	Refunds          int
	CustomersCredit  int
	CashSessions     int
	CounterSales     int
	FiscalDocuments  int
	PaymentRequests  int
	ReminderItems    int
	DunningExclusion int
}

// SeedDemo creates a minimal demo: organization "Porto Demo", marina "Marina Demo" (slug demo) and one user per
// role (<role>@demo.porto.test), all with the same password. When password is empty a random one is generated
// and returned (shown once by the CLI). Then it seeds the M1 data through the domain services (m1-plan §6): the
// harbour when the marina has no maps, the registry when it has no customers. Every part is idempotent: with an
// existing demo marina the users are left alone and only the missing parts are seeded.
func (a *App) SeedDemo(ctx context.Context, password string) (*SeedResult, error) {
	if m, err := store.MarinaBySlug(ctx, a.DB.DB, DemoMarinaSlug); err == nil {
		res := &SeedResult{OrganizationID: m.OrganizationID, MarinaID: m.ID}
		owner, err := store.UserByEmail(ctx, a.DB.DB, string(auth.RoleOwner)+"@"+DemoEmailDomain)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return res, nil // demo marina without its demo owner: leave it alone
			}
			return nil, err
		}
		if err := a.seedM1(ctx, demoOwner(owner.ID, m.OrganizationID), res); err != nil {
			return nil, err
		}
		return res, nil
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
	p := demoOwner(res.UserID, res.OrganizationID)
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
	if err := a.seedM1(ctx, p, out); err != nil {
		return nil, err
	}
	return out, nil
}

func demoOwner(userID, orgID string) *auth.Principal {
	return &auth.Principal{
		Kind: auth.KindUser, UserID: userID,
		Grants: []auth.Grant{{OrganizationID: orgID, Role: auth.RoleOwner}},
	}
}

// seedM1 seeds the harbour (seed_harbor.go, when the marina has no maps), the registry (seed_registry.go, when
// it has no customers), from M2 the pricing catalogue and price lists (seed_pricing.go, when the marina has no
// price lists) and from M3 contracts, transits, quotes and presences (seed_occupancy.go, when the marina has no
// contracts). Each part decides its own idempotency.
func (a *App) seedM1(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if err := a.seedHarbor(ctx, p, res); err != nil {
		return fmt.Errorf("seed: harbour: %w", err)
	}
	if err := a.seedRegistry(ctx, p, res); err != nil {
		return fmt.Errorf("seed: registry: %w", err)
	}
	if err := a.seedPricing(ctx, p, res); err != nil {
		return fmt.Errorf("seed: pricing: %w", err)
	}
	if err := a.seedOccupancy(ctx, p, res); err != nil {
		return fmt.Errorf("seed: occupancy: %w", err)
	}
	// M4 (m4-plan §10): documents first (templates used by the others), then contracts, then rights.
	if err := a.seedDocuments(ctx, p, res); err != nil {
		return fmt.Errorf("seed: documents: %w", err)
	}
	if err := a.seedContractsM4(ctx, p, res); err != nil {
		return fmt.Errorf("seed: contracts m4: %w", err)
	}
	if err := a.seedRights(ctx, p, res); err != nil {
		return fmt.Errorf("seed: rights: %w", err)
	}
	// M5 (m5-plan §10): charges (backfill of the M4 instalments, exports), then the ledger, then cash and
	// collection. Each part is a no-op when the demo marina already has its data.
	if err := a.seedBilling(ctx, p, res); err != nil {
		return fmt.Errorf("seed: billing: %w", err)
	}
	if err := a.seedPayments(ctx, p, res); err != nil {
		return fmt.Errorf("seed: payments: %w", err)
	}
	if err := a.seedCash(ctx, p, res); err != nil {
		return fmt.Errorf("seed: cash: %w", err)
	}
	// M9 (m9-plan §15.2): shed and yard storage contracts with one scenario (STORAGE), then equipment, meters,
	// readings and maintenance plans (UTILITIES). Each part is a no-op when the demo marina already has its data.
	if err := a.seedStorage(ctx, p, res); err != nil {
		return fmt.Errorf("seed: storage: %w", err)
	}
	if err := a.seedUtilities(ctx, p, res); err != nil {
		return fmt.Errorf("seed: utilities: %w", err)
	}
	return nil
}
