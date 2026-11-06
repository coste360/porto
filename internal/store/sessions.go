package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// InsertSession inserts a session.
func InsertSession(ctx context.Context, db bun.IDB, s *Session) error {
	_, err := db.NewInsert().Model(s).Exec(ctx)
	return err
}

// SessionByTokenHash returns a session by the SHA-256 of its token.
func SessionByTokenHash(ctx context.Context, db bun.IDB, hash string) (*Session, error) {
	s := new(Session)
	if err := db.NewSelect().Model(s).Where("token_hash = ?", hash).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return s, nil
}

// TouchSession slides the idle expiry of a session.
func TouchSession(ctx context.Context, db bun.IDB, id string, lastSeen, idleExpires time.Time) error {
	_, err := db.NewUpdate().Table("sessions").
		Set("last_seen_at = ?", lastSeen).Set("idle_expires_at = ?", idleExpires).Set("updated_at = ?", lastSeen).
		Where("id = ?", id).Exec(ctx)
	return err
}

// DeleteSession removes one session by id.
func DeleteSession(ctx context.Context, db bun.IDB, id string) error {
	_, err := db.NewDelete().Table("sessions").Where("id = ?", id).Exec(ctx)
	return err
}

// DeleteSessionByTokenHash removes one session by token hash.
func DeleteSessionByTokenHash(ctx context.Context, db bun.IDB, hash string) error {
	_, err := db.NewDelete().Table("sessions").Where("token_hash = ?", hash).Exec(ctx)
	return err
}

// DeleteUserSessions removes every session of a user except exceptID (when not empty).
func DeleteUserSessions(ctx context.Context, db bun.IDB, userID, exceptID string) (int64, error) {
	q := db.NewDelete().Table("sessions").Where("user_id = ?", userID)
	if exceptID != "" {
		q = q.Where("id <> ?", exceptID)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}

// DeleteExpiredSessions removes sessions past their absolute or idle expiry.
func DeleteExpiredSessions(ctx context.Context, db bun.IDB, now time.Time) (int64, error) {
	res, err := db.NewDelete().Table("sessions").
		WhereOr("expires_at <= ?", now).WhereOr("idle_expires_at <= ?", now).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}

// GetTOTP returns the TOTP enrolment of a user (pending or confirmed).
func GetTOTP(ctx context.Context, db bun.IDB, userID string) (*UserTOTP, error) {
	t := new(UserTOTP)
	if err := db.NewSelect().Model(t).Where("user_id = ?", userID).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// ReplacePendingTOTP drops an unconfirmed enrolment of the user and inserts t. A confirmed enrolment is left in
// place and the call fails with ErrConflict.
func ReplacePendingTOTP(ctx context.Context, db bun.IDB, t *UserTOTP) error {
	if _, err := db.NewDelete().Table("user_totp").Where("user_id = ?", t.UserID).Where("confirmed_at IS NULL").
		Exec(ctx); err != nil {
		return err
	}
	_, err := db.NewInsert().Model(t).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// ConfirmTOTP marks a pending enrolment confirmed.
func ConfirmTOTP(ctx context.Context, db bun.IDB, userID string, at time.Time) error {
	res, err := db.NewUpdate().Table("user_totp").Set("confirmed_at = ?", at).Set("updated_at = ?", at).
		Where("user_id = ?", userID).Where("confirmed_at IS NULL").Exec(ctx)
	return expectOne(res, err)
}

// AdvanceTOTPStep records step as the last accepted time step. It returns false when a step >= step was already
// used (replay protection, also under concurrency).
func AdvanceTOTPStep(ctx context.Context, db bun.IDB, userID string, step int64, at time.Time) (bool, error) {
	res, err := db.NewUpdate().Table("user_totp").Set("last_step = ?", step).Set("updated_at = ?", at).
		Where("user_id = ?", userID).Where("last_step < ?", step).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// DeleteTOTP removes the enrolment and the recovery codes of a user.
func DeleteTOTP(ctx context.Context, db bun.IDB, userID string) error {
	if _, err := db.NewDelete().Table("recovery_codes").Where("user_id = ?", userID).Exec(ctx); err != nil {
		return err
	}
	_, err := db.NewDelete().Table("user_totp").Where("user_id = ?", userID).Exec(ctx)
	return err
}

// ReplaceRecoveryCodes deletes the recovery codes of a user and stores the given hashes.
func ReplaceRecoveryCodes(ctx context.Context, db bun.IDB, userID string, hashes []string, at time.Time) error {
	if _, err := db.NewDelete().Table("recovery_codes").Where("user_id = ?", userID).Exec(ctx); err != nil {
		return err
	}
	if len(hashes) == 0 {
		return nil
	}
	rows := make([]RecoveryCode, len(hashes))
	for i, h := range hashes {
		rows[i] = RecoveryCode{ID: NewID(), UserID: userID, CodeHash: h, CreatedAt: at, UpdatedAt: at}
	}
	_, err := db.NewInsert().Model(&rows).Exec(ctx)
	return err
}

// UseRecoveryCode consumes an unused recovery code; false when no such code exists.
func UseRecoveryCode(ctx context.Context, db bun.IDB, userID, hash string, at time.Time) (bool, error) {
	res, err := db.NewUpdate().Table("recovery_codes").Set("used_at = ?", at).Set("updated_at = ?", at).
		Where("user_id = ?", userID).Where("code_hash = ?", hash).Where("used_at IS NULL").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// CountUnusedRecoveryCodes returns how many recovery codes a user has left.
func CountUnusedRecoveryCodes(ctx context.Context, db bun.IDB, userID string) (int, error) {
	return toInt(db.NewSelect().Table("recovery_codes").Where("user_id = ?", userID).Where("used_at IS NULL").Count(ctx))
}

// InsertResetToken inserts a password reset or invitation token.
func InsertResetToken(ctx context.Context, db bun.IDB, t *PasswordResetToken) error {
	_, err := db.NewInsert().Model(t).Exec(ctx)
	return err
}

// ConsumeResetToken marks a valid (unused, unexpired) token used and returns it; ErrNotFound otherwise. It is a
// single conditional UPDATE, so a token is consumed at most once under concurrency.
func ConsumeResetToken(ctx context.Context, db bun.IDB, hash string, now time.Time) (*PasswordResetToken, error) {
	var rows []PasswordResetToken
	err := db.NewRaw(`UPDATE password_reset_tokens SET used_at = ?, updated_at = ?
		WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?
		RETURNING id, user_id, token_hash, purpose, expires_at, used_at, created_at, updated_at`,
		now, now, hash, now).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

// InvalidateResetTokens marks every unused token of a user used.
func InvalidateResetTokens(ctx context.Context, db bun.IDB, userID string, now time.Time) error {
	_, err := db.NewUpdate().Table("password_reset_tokens").Set("used_at = ?", now).Set("updated_at = ?", now).
		Where("user_id = ?", userID).Where("used_at IS NULL").Exec(ctx)
	return err
}

// DeleteStaleResetTokens removes tokens that expired or were used before the given instant.
func DeleteStaleResetTokens(ctx context.Context, db bun.IDB, before time.Time) (int64, error) {
	res, err := db.NewDelete().Table("password_reset_tokens").
		WhereOr("expires_at <= ?", before).WhereOr("used_at <= ?", before).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}
