package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// SEPA mandates (m5-plan §5.5): at most one active mandate per customer and provider (sepa_mandates_active_uidx).
// Provider references are KeyRing-encrypted. Owned by CASH-DRIVERS.

// SEPA mandate statuses.
const (
	MandatePending = "pending"
	MandateActive  = "active"
	MandateRevoked = "revoked"
	MandateFailed  = "failed"
)

// InsertSEPAMandate inserts a mandate.
func InsertSEPAMandate(ctx context.Context, tx bun.IDB, m *SEPAMandate) error {
	_, err := tx.NewInsert().Model(m).Exec(ctx)
	return err
}

// GetSEPAMandate returns a mandate of the marina.
func GetSEPAMandate(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*SEPAMandate, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	m := new(SEPAMandate)
	err := db.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx)
	return m, notFound(err)
}

// ListSEPAMandates returns the mandates of a customer, newest first.
func ListSEPAMandates(ctx context.Context, db bun.IDB, s MarinaScope, customerID string) ([]SEPAMandate, error) {
	out := []SEPAMandate{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("customer_id = ?", customerID).OrderExpr("id DESC").Scan(ctx)
	return out, err
}

// SEPAMandateBySetupIntent returns the mandate created with a mandate_setup intent.
func SEPAMandateBySetupIntent(ctx context.Context, db bun.IDB, s MarinaScope, intentID string) (*SEPAMandate, error) {
	m := new(SEPAMandate)
	err := db.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("setup_intent_id = ?", intentID).Scan(ctx)
	return m, notFound(err)
}

// SEPAMandateByRef returns the mandate of a provider mandate reference.
func SEPAMandateByRef(ctx context.Context, db bun.IDB, s MarinaScope, provider, ref string) (*SEPAMandate, error) {
	m := new(SEPAMandate)
	err := db.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("provider_code = ?", provider).
		Where("mandate_ref = ?", ref).OrderExpr("id DESC").Limit(1).Scan(ctx)
	return m, notFound(err)
}

// ActiveSEPAMandate returns the active mandate of a customer with a provider.
func ActiveSEPAMandate(ctx context.Context, db bun.IDB, s MarinaScope, customerID, provider string) (*SEPAMandate, error) {
	m := new(SEPAMandate)
	err := db.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("customer_id = ?", customerID).
		Where("provider_code = ?", provider).Where("status = ?", MandateActive).Limit(1).Scan(ctx)
	return m, notFound(err)
}

// ActivateSEPAMandate moves a pending mandate to active with its provider references. A customer that already has
// an active mandate with the provider gets the old one revoked first (in the same transaction).
func ActivateSEPAMandate(ctx context.Context, tx bun.IDB, s MarinaScope, m *SEPAMandate, now time.Time) (bool, error) {
	now = UTC(now)
	if _, err := tx.NewUpdate().Model((*SEPAMandate)(nil)).Set("status = ?", MandateRevoked).Set("revoked_at = ?", now).
		Set("version = version + 1").Set("updated_at = ?", now).Where("marina_id = ?", s.ID()).
		Where("customer_id = ?", m.CustomerID).Where("provider_code = ?", m.ProviderCode).
		Where("status = ?", MandateActive).Where("id <> ?", m.ID).Exec(ctx); err != nil {
		return false, err
	}
	res, err := tx.NewUpdate().Model((*SEPAMandate)(nil)).Set("status = ?", MandateActive).Set("activated_at = ?", now).
		Set("provider_customer_ref_enc = ?", nullString(m.ProviderCustomerRefEnc)).
		Set("payment_method_ref_enc = ?", nullString(m.PaymentMethodRefEnc)).Set("mandate_ref = ?", nullString(m.MandateRef)).
		Set("iban_last4 = ?", m.IBANLast4).Set("version = version + 1").Set("updated_at = ?", now).
		Where("marina_id = ?", s.ID()).Where("id = ?", m.ID).Where("status = ?", MandatePending).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// SetSEPAMandateStatus moves a mandate from one of from to status (revoked, failed).
func SetSEPAMandateStatus(ctx context.Context, tx bun.IDB, s MarinaScope, id string, from []string, status string, now time.Time) (bool, error) {
	now = UTC(now)
	q := tx.NewUpdate().Model((*SEPAMandate)(nil)).Set("status = ?", status).Set("version = version + 1").
		Set("updated_at = ?", now).Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status IN (?)", bun.List(from))
	if status == MandateRevoked {
		q = q.Set("revoked_at = ?", now)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// SEPACandidate is an issued open instalment charge of a customer with an active mandate.
type SEPACandidate struct {
	ChargeID   string `bun:"charge_id"`
	CustomerID string `bun:"customer_id"`
	ContractID string `bun:"contract_id"`
	MandateID  string `bun:"mandate_id"`
	Policy     JSON   `bun:"policy"`
}

// SEPACandidates returns the open instalment charges due on or before upTo whose customer has an active mandate
// with provider and that have no live intent. The contract policy is returned for the caller's sepa check.
func SEPACandidates(ctx context.Context, db bun.IDB, s MarinaScope, provider string, upTo calendar.Date, limit int) ([]SEPACandidate, error) {
	out := []SEPACandidate{}
	err := db.NewSelect().TableExpr("charges AS ch").
		Join("JOIN contracts AS c ON c.id = ch.contract_id AND c.marina_id = ch.marina_id").
		Join("JOIN sepa_mandates AS sm ON sm.customer_id = ch.customer_id AND sm.marina_id = ch.marina_id AND sm.status = ? AND sm.provider_code = ?", MandateActive, provider).
		ColumnExpr("ch.id AS charge_id, ch.customer_id, ch.contract_id, sm.id AS mandate_id, c.policy").
		Where("ch.marina_id = ?", s.ID()).Where("ch.origin_type = ?", ChargeOriginInstallment).
		Where("ch.payment_status IN (?)", bun.List([]string{ChargeOpen, ChargePartiallyPaid})).
		Where("ch.total_cents > 0").Where("ch.due_on <= ?", upTo).
		Where("NOT EXISTS (SELECT 1 FROM payment_intent_charges pic WHERE pic.charge_id = ch.id AND pic.open_flag = ?)", true).
		OrderExpr("ch.due_on, ch.id").Limit(int64(limit)).Scan(ctx, &out)
	return out, err
}

// FailedSEPAIntents counts failed sepa_collect intents per charge (retry attempts).
func FailedSEPAIntents(ctx context.Context, db bun.IDB, s MarinaScope, chargeIDs []string) (map[string]int, error) {
	out := map[string]int{}
	if len(chargeIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ChargeID string `bun:"charge_id"`
		N        int    `bun:"n"`
	}
	err := db.NewSelect().TableExpr("payment_intent_charges AS pic").
		Join("JOIN payment_intents AS pi ON pi.id = pic.intent_id").ColumnExpr("pic.charge_id, COUNT(*) AS n").
		Where("pic.marina_id = ?", s.ID()).Where("pi.purpose = ?", IntentPurposeSEPACollect).
		Where("pic.charge_id IN (?)", bun.List(chargeIDs)).GroupExpr("pic.charge_id").Scan(ctx, &rows)
	for _, r := range rows {
		out[r.ChargeID] = r.N
	}
	return out, err
}
