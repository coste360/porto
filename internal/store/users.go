package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// InsertUser inserts a user. A taken email is ErrConflict.
func InsertUser(ctx context.Context, db bun.IDB, u *User) error {
	_, err := db.NewInsert().Model(u).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// InsertUserIfAbsent inserts u unless a user with the same email exists, reporting whether it inserted.
// ON CONFLICT DO NOTHING keeps the surrounding transaction usable on PostgreSQL (a unique violation would
// abort it), so the caller can read the concurrently created user in the same transaction.
func InsertUserIfAbsent(ctx context.Context, db bun.IDB, u *User) (bool, error) {
	res, err := db.NewInsert().Model(u).On("CONFLICT (email) DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// GetUser returns a user by id.
func GetUser(ctx context.Context, db bun.IDB, id string) (*User, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	u := new(User)
	if err := db.NewSelect().Model(u).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return u, nil
}

// UserByEmail returns a user by normalised (lowercase) email.
func UserByEmail(ctx context.Context, db bun.IDB, email string) (*User, error) {
	u := new(User)
	if err := db.NewSelect().Model(u).Where("email = ?", email).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return u, nil
}

// UsersByIDs returns the users with the given ids.
func UsersByIDs(ctx context.Context, db bun.IDB, ids []string) ([]User, error) {
	out := []User{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("id IN (?)", bun.List(ids)).Scan(ctx)
	return out, err
}

// CountUsers returns the number of users.
func CountUsers(ctx context.Context, db bun.IDB) (int, error) {
	return toInt(db.NewSelect().Table("users").Count(ctx))
}

// SetUserPassword stores a new password hash.
func SetUserPassword(ctx context.Context, db bun.IDB, userID, hash string, at time.Time) error {
	res, err := db.NewUpdate().Table("users").
		Set("password_hash = ?", hash).Set("password_changed_at = ?", at).Set("updated_at = ?", at).
		Where("id = ?", userID).Exec(ctx)
	return expectOne(res, err)
}

// RehashUserPassword replaces the hash after a parameter upgrade without touching password_changed_at.
func RehashUserPassword(ctx context.Context, db bun.IDB, userID, hash string, at time.Time) error {
	_, err := db.NewUpdate().Table("users").Set("password_hash = ?", hash).Set("updated_at = ?", at).
		Where("id = ?", userID).Exec(ctx)
	return err
}

// UpdateUserProfile stores name and locale.
func UpdateUserProfile(ctx context.Context, db bun.IDB, userID, name, locale string, at time.Time) error {
	res, err := db.NewUpdate().Table("users").
		Set("name = ?", name).Set("locale = ?", locale).Set("updated_at = ?", at).
		Where("id = ?", userID).Exec(ctx)
	return expectOne(res, err)
}

// TouchUserLogin records a successful login.
func TouchUserLogin(ctx context.Context, db bun.IDB, userID string, at time.Time) error {
	_, err := db.NewUpdate().Table("users").Set("last_login_at = ?", at).Set("updated_at = ?", at).
		Where("id = ?", userID).Exec(ctx)
	return err
}

// InsertMembership inserts a grant. A duplicate (same user and scope) is ErrConflict.
func InsertMembership(ctx context.Context, db bun.IDB, m *Membership) error {
	_, err := db.NewInsert().Model(m).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// MembershipsOfUser returns every grant of a user, organization-wide ones first.
func MembershipsOfUser(ctx context.Context, db bun.IDB, userID string) ([]Membership, error) {
	out := []Membership{}
	if !ValidID(userID) {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("user_id = ?", userID).
		OrderExpr("CASE WHEN marina_id IS NULL THEN 0 ELSE 1 END, created_at, id").Scan(ctx)
	return out, err
}

// MarinaMembership returns the marina-level grant of a user on the scope's marina.
func MarinaMembership(ctx context.Context, db bun.IDB, s MarinaScope, userID string) (*Membership, error) {
	if !ValidID(userID) {
		return nil, ErrNotFound
	}
	m := new(Membership)
	err := db.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("user_id = ?", userID).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// OrgMembership returns the organization-wide grant of a user.
func OrgMembership(ctx context.Context, db bun.IDB, orgID, userID string) (*Membership, error) {
	if !ValidID(userID) || !ValidID(orgID) {
		return nil, ErrNotFound
	}
	m := new(Membership)
	err := db.NewSelect().Model(m).Where("organization_id = ?", orgID).Where("marina_id IS NULL").
		Where("user_id = ?", userID).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// UpdateMembershipRole changes the role of a grant.
func UpdateMembershipRole(ctx context.Context, db bun.IDB, id, role string, at time.Time) error {
	res, err := db.NewUpdate().Table("memberships").Set("role = ?", role).Set("updated_at = ?", at).
		Where("id = ?", id).Exec(ctx)
	return expectOne(res, err)
}

// DeleteMembership removes a grant.
func DeleteMembership(ctx context.Context, db bun.IDB, id string) error {
	res, err := db.NewDelete().Table("memberships").Where("id = ?", id).Exec(ctx)
	return expectOne(res, err)
}

// CountActiveOrgOwners counts the organization-wide owners whose user is active.
func CountActiveOrgOwners(ctx context.Context, db bun.IDB, orgID string) (int, error) {
	return toInt(db.NewSelect().TableExpr("memberships AS m").Join("JOIN users AS u ON u.id = m.user_id").
		Where("m.organization_id = ?", orgID).Where("m.marina_id IS NULL").Where("m.role = 'owner'").
		Where("u.active = ?", true).Count(ctx))
}

// MemberRow is a grant joined with its user, for team listings.
type MemberRow struct {
	MembershipID   string    `bun:"membership_id"`
	UserID         string    `bun:"user_id"`
	OrganizationID string    `bun:"organization_id"`
	MarinaID       string    `bun:"marina_id,nullzero"`
	Role           string    `bun:"role"`
	Email          string    `bun:"email"`
	Name           string    `bun:"name"`
	Active         bool      `bun:"active"`
	HasPassword    bool      `bun:"has_password"`
	TOTPEnabled    bool      `bun:"totp_enabled"`
	LastLoginAt    time.Time `bun:"last_login_at,nullzero"`
}

func memberQuery(db bun.IDB) *bun.SelectQuery {
	return db.NewSelect().
		ColumnExpr("m.id AS membership_id, m.user_id, m.organization_id, m.marina_id, m.role").
		ColumnExpr("u.email, u.name, u.active, u.last_login_at").
		ColumnExpr("(u.password_hash IS NOT NULL) AS has_password").
		ColumnExpr("EXISTS (SELECT 1 FROM user_totp t WHERE t.user_id = u.id AND t.confirmed_at IS NOT NULL) AS totp_enabled").
		TableExpr("memberships AS m").Join("JOIN users AS u ON u.id = m.user_id")
}

// MarinaMembers returns the grants that apply to the scope's marina: its marina-level grants and the
// organization-wide grants of its organization, ordered by name.
func MarinaMembers(ctx context.Context, db bun.IDB, s MarinaScope) ([]MemberRow, error) {
	out := []MemberRow{}
	err := memberQuery(db).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("m.marina_id = ?", s.ID()).
				WhereOr("m.organization_id = ? AND m.marina_id IS NULL", s.OrgID())
		}).
		OrderExpr("u.name, u.id, m.marina_id").Scan(ctx, &out)
	return out, err
}

// OrgOwners returns the organization-wide owners.
func OrgOwners(ctx context.Context, db bun.IDB, s OrgScope) ([]MemberRow, error) {
	out := []MemberRow{}
	err := memberQuery(db).
		Where("m.organization_id = ?", s.ID()).Where("m.marina_id IS NULL").Where("m.role = 'owner'").
		OrderExpr("u.name, u.id").Scan(ctx, &out)
	return out, err
}
