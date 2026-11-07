package contract

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/schedule"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Refund rules of the cancellation policy (m4-plan §3.3, §3.5).
const (
	RefundProRata = "pro_rata"
	RefundNone    = "none"
)

// PolicyVersion is the current version of Policy. Version 1 rows (M3) are read as version 2 with the defaults:
// a nil Payment, Renewal or Cancellation means "the marina default" (contract_settings.default_policy, then the
// built-in default).
const PolicyVersion = 2

// Policy is contracts.policy and contract_settings.default_policy (versioned JSON, m4-plan §3.3). Unknown keys
// are rejected.
type Policy struct {
	Version       int    `json:"version"`
	RepriceOnMove bool   `json:"reprice_on_move"`
	ReleasePolicy string `json:"release_policy"` // never, with_consent
	AllowSegments bool   `json:"allow_segments"`

	Payment      *PaymentPolicy      `json:"payment,omitempty"`
	Renewal      *RenewalPolicy      `json:"renewal,omitempty"`
	Cancellation *CancellationPolicy `json:"cancellation,omitempty"`
}

// PaymentPolicy is the payment part of a policy (JSON form of schedule.PaymentPolicy).
type PaymentPolicy struct {
	Deposit            *DepositPolicy      `json:"deposit"`
	DepositDueDays     int                 `json:"deposit_due_days"`
	Schedule           SchedulePolicy      `json:"schedule"`
	IssueDaysBeforeDue int                 `json:"issue_days_before_due"`
	RoundingUnitCents  int64               `json:"rounding_unit_cents"`
	EarlyPayment       *EarlyPaymentPolicy `json:"early_payment"`
	SEPA               bool                `json:"sepa"`
}

// DepositPolicy is {"bp": n} or {"cents": n}.
type DepositPolicy struct {
	BP    *int64 `json:"bp,omitempty"`
	Cents *int64 `json:"cents,omitempty"`
}

// SchedulePolicy is one of the three schedule modes.
type SchedulePolicy struct {
	Mode                 string       `json:"mode"`
	Dates                []FixedDueOn `json:"dates,omitempty"`
	EveryMonths          int          `json:"every_months,omitempty"`
	Count                int          `json:"count,omitempty"`
	FirstDueOffsetMonths int          `json:"first_due_offset_months,omitempty"`
	DueOffsetDays        int          `json:"due_offset_days,omitempty"`
}

// FixedDueOn is a due date "MM-DD" or "YYYY-MM-DD" with its weight in basis points.
type FixedDueOn struct {
	DueOn string `json:"due_on"`
	BP    int64  `json:"bp"`
}

// EarlyPaymentPolicy is the early-payment alternative.
type EarlyPaymentPolicy struct {
	DiscountBP      int64 `json:"discount_bp"`
	PayByOffsetDays int   `json:"pay_by_offset_days"`
}

// RenewalPolicy is the renewal clause.
type RenewalPolicy struct {
	Tacit      bool `json:"tacit"`
	NoticeDays int  `json:"notice_days"`
}

// CancellationPolicy is the termination clause.
type CancellationPolicy struct {
	NoticeDays int    `json:"notice_days"`
	Refund     string `json:"refund"` // pro_rata, none
	PenaltyBP  int64  `json:"penalty_bp"`
}

// BuiltinPayment is the payment policy when neither the contract nor the marina sets one: the whole amount due
// on the start date, issued 15 days before.
func BuiltinPayment() PaymentPolicy {
	return PaymentPolicy{Schedule: SchedulePolicy{Mode: schedule.ModeSingle}, IssueDaysBeforeDue: 15, RoundingUnitCents: 1}
}

// BuiltinRenewal and BuiltinCancellation are the defaults of the other clauses.
func BuiltinRenewal() RenewalPolicy { return RenewalPolicy{NoticeDays: 60} }

// BuiltinCancellation is the default termination clause: 30 days notice, refund pro rata, no penalty.
func BuiltinCancellation() CancellationPolicy {
	return CancellationPolicy{NoticeDays: 30, Refund: RefundProRata}
}

// Planner converts p to the planner's policy.
func (p PaymentPolicy) Planner() schedule.PaymentPolicy {
	out := schedule.PaymentPolicy{
		DepositDueDays: p.DepositDueDays, IssueDaysBeforeDue: p.IssueDaysBeforeDue,
		RoundingUnit: money.Cents(p.RoundingUnitCents), SEPA: p.SEPA,
		Schedule: schedule.Schedule{
			Mode: p.Schedule.Mode, EveryMonths: p.Schedule.EveryMonths, Count: p.Schedule.Count,
			FirstDueOffsetMonths: p.Schedule.FirstDueOffsetMonths, DueOffsetDays: p.Schedule.DueOffsetDays,
		},
	}
	if d := p.Deposit; d != nil {
		out.Deposit = &schedule.Deposit{}
		if d.BP != nil {
			out.Deposit.BP = *d.BP
		}
		if d.Cents != nil {
			out.Deposit.Cents = money.Cents(*d.Cents)
		}
	}
	if e := p.EarlyPayment; e != nil {
		out.EarlyPayment = &schedule.EarlyPayment{DiscountBP: e.DiscountBP, PayByOffsetDays: e.PayByOffsetDays}
	}
	for _, dd := range p.Schedule.Dates {
		f := schedule.FixedDue{BP: dd.BP}
		if len(dd.DueOn) == 5 {
			f.MonthDay = dd.DueOn
		} else if day, err := calendar.Parse(dd.DueOn); err == nil {
			f.Date = day
		} else {
			f.MonthDay = "invalid"
		}
		out.Schedule.Dates = append(out.Schedule.Dates, f)
	}
	return out
}

// validatePayment adds the payment errors of p to v.
func validatePayment(v *auth.ValidationError, p *PaymentPolicy) {
	if p == nil {
		return
	}
	if p.Deposit != nil && p.Deposit.BP != nil && p.Deposit.Cents != nil {
		v.Add("policy.payment.deposit", "invalid")
		return
	}
	if err := schedule.Validate(p.Planner()); err != nil {
		v.Add("policy.payment", "invalid")
	}
}

func validateClauses(v *auth.ValidationError, r *RenewalPolicy, c *CancellationPolicy) {
	if r != nil && (r.NoticeDays < 0 || r.NoticeDays > 365) {
		v.Add("policy.renewal.notice_days", "invalid")
	}
	if c != nil {
		if c.Refund != RefundProRata && c.Refund != RefundNone {
			v.Add("policy.cancellation.refund", "invalid")
		}
		if c.PenaltyBP < 0 || c.PenaltyBP > 10_000 {
			v.Add("policy.cancellation.penalty_bp", "invalid")
		}
		if c.NoticeDays < 0 || c.NoticeDays > 365 {
			v.Add("policy.cancellation.notice_days", "invalid")
		}
	}
}

// PolicyInput are the policy fields given by a client (nil = unchanged / default). Payment, Renewal and
// Cancellation replace the whole clause.
type PolicyInput struct {
	Version       *int
	RepriceOnMove *bool
	ReleasePolicy *string
	AllowSegments *bool
	Payment       *PaymentPolicy
	Renewal       *RenewalPolicy
	Cancellation  *CancellationPolicy
}

// mergePolicy applies in on base and validates the result; the result is version 2.
func mergePolicy(v *auth.ValidationError, base Policy, in *PolicyInput) Policy {
	base.Version = PolicyVersion
	if in == nil {
		return base
	}
	if in.Version != nil && *in.Version != 1 && *in.Version != PolicyVersion {
		v.Add("policy.version", "invalid")
	}
	if in.RepriceOnMove != nil {
		base.RepriceOnMove = *in.RepriceOnMove
	}
	if in.ReleasePolicy != nil {
		if *in.ReleasePolicy != ReleaseNever && *in.ReleasePolicy != ReleaseWithConsent {
			v.Add("policy.release_policy", "invalid")
		}
		base.ReleasePolicy = *in.ReleasePolicy
	}
	if in.AllowSegments != nil {
		base.AllowSegments = *in.AllowSegments
	}
	if in.Payment != nil {
		validatePayment(v, in.Payment)
		p := *in.Payment
		base.Payment = &p
	}
	if in.Renewal != nil {
		r := *in.Renewal
		base.Renewal = &r
	}
	if in.Cancellation != nil {
		c := *in.Cancellation
		base.Cancellation = &c
	}
	validateClauses(v, in.Renewal, in.Cancellation)
	return base
}

// DecodePolicy reads a stored policy, rejecting unknown keys (m3-plan §4.7). An empty document is the zero
// policy.
func DecodePolicy(raw store.JSON) (Policy, error) {
	var p Policy
	if len(raw) == 0 || string(raw) == "{}" {
		return p, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Policy{}, fmt.Errorf("contract policy: %w", err)
	}
	return p, nil
}

// defaultPolicy returns the contract policy defaults of the marina occupancy settings (m3-plan §4.7). The M4
// clauses stay nil: they resolve to contract_settings.default_policy (then the built-in defaults) when the plan is
// made, and the resolved payment is then stored on the contract (planTx).
func defaultPolicy(st *store.OccupancySettings) Policy {
	return Policy{Version: PolicyVersion, RepriceOnMove: st.RepriceOnMove, ReleasePolicy: st.ReleasePolicy, AllowSegments: true}
}

// effective returns the clauses of p with the marina defaults (cs) and the built-in defaults filled in.
func effective(p Policy, cs *store.ContractSettings) (PaymentPolicy, RenewalPolicy, CancellationPolicy) {
	var d Policy
	if cs != nil {
		d, _ = DecodePolicy(cs.DefaultPolicy)
	}
	pay, ren, can := BuiltinPayment(), BuiltinRenewal(), BuiltinCancellation()
	switch {
	case p.Payment != nil:
		pay = *p.Payment
	case d.Payment != nil:
		pay = *d.Payment
	}
	switch {
	case p.Renewal != nil:
		ren = *p.Renewal
	case d.Renewal != nil:
		ren = *d.Renewal
	}
	switch {
	case p.Cancellation != nil:
		can = *p.Cancellation
	case d.Cancellation != nil:
		can = *d.Cancellation
	}
	return pay, ren, can
}
