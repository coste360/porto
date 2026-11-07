package respond

import (
	"fmt"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/cash"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/domainerr"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/http/problem"

	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/domain/signature"
)

func TestMapError_M1Sentinels(t *testing.T) {
	cases := map[error]problem.Type{
		harbor.ErrMapNotEmpty:        problem.MapNotEmpty,
		harbor.ErrNotGeoreferenced:   problem.MapNotGeoreferenced,
		harbor.ErrVersionMismatch:    problem.VersionConflict,
		harbor.ErrCSVInvalid:         problem.CSVInvalid,
		harbor.ErrInvalidFile:        problem.InvalidFile,
		harbor.ErrFileTooLarge:       problem.FileTooLarge,
		harbor.ErrUnsupportedMedia:   problem.UnsupportedMediaType,
		customer.ErrAnonymized:       problem.CustomerAnonymized,
		customer.ErrMerged:           problem.CustomerMerged,
		customer.ErrVersionMismatch:  problem.VersionConflict,
		vessel.ErrVersionMismatch:    problem.VersionConflict,
		document.ErrInvalidFile:      problem.InvalidFile,
		document.ErrFileTooLarge:     problem.FileTooLarge,
		document.ErrUnsupportedMedia: problem.UnsupportedMediaType,
		document.ErrLinkExpired:      problem.DownloadLinkExpired,
		document.ErrVersionMismatch:  problem.VersionConflict,
		deadline.ErrVersionMismatch:  problem.VersionConflict,
	}
	for err, want := range cases {
		got, _, ok := MapError(fmt.Errorf("wrapped: %w", err))
		if !ok || got != want {
			t.Errorf("%v: %q, %v; want %q", err, got, ok, want)
		}
	}
}

func TestMapError_MapInvalidCarriesIssues(t *testing.T) {
	rep := layout.Report{
		Errors:   []layout.Issue{{Code: "berth_overlap", Severity: layout.SeverityError, Kind: "berth", Refs: []layout.Ref{"a", "b"}}},
		Warnings: []layout.Issue{{Code: "depth_missing", Severity: layout.SeverityWarning, Kind: "berth", Refs: []layout.Ref{"a"}}},
	}
	err := fmt.Errorf("save: %w", &harbor.MapInvalidError{Report: rep})
	typ, opts, ok := MapError(err)
	if !ok || typ != problem.MapInvalid {
		t.Fatalf("type = %q, %v", typ, ok)
	}
	p := problem.New(typ, opts...)
	issues, _ := p.Issues.([]MapIssue)
	if p.Status != 422 || len(issues) != 2 || issues[0].Code != "berth_overlap" || len(issues[0].Refs) != 2 ||
		issues[1].Severity != "warning" {
		t.Fatalf("problem = %+v", p)
	}
}

func TestMapError_M2Sentinels(t *testing.T) {
	cases := map[error]problem.Type{
		pricing.ErrNoPrice:                 problem.NoPrice,
		pricing.ErrPromoNotApplicable:      problem.PromoNotApplicable,
		pricing.ErrInvalidInput:            problem.ValidationFailed,
		pricing.ErrOverflow:                problem.ValidationFailed,
		catalog.ErrInUse:                   problem.InUse,
		catalog.ErrVersionMismatch:         problem.VersionConflict,
		pricelist.ErrImmutable:             problem.PriceListImmutable,
		pricelist.ErrOverlap:               problem.PriceListOverlap,
		pricelist.ErrInvalid:               problem.PriceListInvalid,
		pricelist.ErrVersionMismatch:       problem.VersionConflict,
		pricelist.ErrNoActiveList:          problem.NoPrice,
		domainerr.ErrImmutable:             problem.PriceListImmutable,
		&pricing.InputError{Field: "x"}:    problem.ValidationFailed,
		&catalog.InUseError{References: 1}: problem.InUse,
	}
	for err, want := range cases {
		got, _, ok := MapError(fmt.Errorf("wrapped: %w", err))
		if !ok || got != want {
			t.Errorf("%v: %q, %v; want %q", err, got, ok, want)
		}
	}
}

func TestMapError_M2Details(t *testing.T) {
	np := &pricing.NoPriceError{ItemID: "i", ContractType: pricing.ContractTransit, Night: calendar.MustParse("2027-07-01"), Reason: pricing.ReasonNoRule}
	typ, opts, ok := MapError(fmt.Errorf("quote: %w", np))
	if p := problem.New(typ, opts...); !ok || p.Status != 422 || p.Details != np {
		t.Fatalf("no_price = %+v", p)
	}
	typ, opts, _ = MapError(&pricelist.OverlapError{Conflicts: []pricelist.Conflict{{ID: "a", Code: "L"}}})
	if p := problem.New(typ, opts...); p.Type != problem.PriceListOverlap || p.Details == nil {
		t.Fatalf("overlap = %+v", p)
	}
	typ, opts, _ = MapError(&pricelist.InvalidError{Issues: []pricing.Issue{{Code: pricing.IssuePeriodOverlap, Severity: pricing.SeverityError}}})
	p := problem.New(typ, opts...)
	if issues, _ := p.PriceListIssues.([]pricing.Issue); p.Type != problem.PriceListInvalid || len(issues) != 1 {
		t.Fatalf("invalid = %+v", p)
	}
	typ, opts, _ = MapError(&catalog.InUseError{References: 3})
	if p := problem.New(typ, opts...); p.Type != problem.InUse || p.Details == nil {
		t.Fatalf("in_use = %+v", p)
	}
	typ, _, _ = MapError(pricelist.ErrNoActiveList)
	if typ != problem.NoPrice {
		t.Fatalf("no active list = %q", typ)
	}
}

func TestMapError_M3Sentinels(t *testing.T) {
	cases := map[error]problem.Type{
		occupancy.ErrResourceTaken:          problem.ResourceTaken,
		occupancy.ErrFitBlocked:             problem.FitBlocked,
		occupancy.ErrOverrideForbidden:      problem.Forbidden,
		occupancy.ErrRepriceForbidden:       problem.Forbidden,
		occupancy.ErrOverrideReasonRequired: problem.ValidationFailed,
		occupancy.ErrPlacementInvalid:       problem.PlacementInvalid,
		occupancy.ErrClosed:                 problem.ClosureActive,
		occupancy.ErrHoldExpired:            problem.HoldExpired,
		occupancy.ErrAllocationNotActive:    problem.Conflict,
		occupancy.ErrVersionMismatch:        problem.VersionConflict,
		occupancy.ErrUndoUnavailable:        problem.UndoUnavailable,
		occupancy.ErrReleaseInvalid:         problem.ReleaseInvalid,
		occupancy.ErrNotFound:               problem.NotFound,
		harbor.ErrElementInUse:              problem.InUse,
		contract.ErrInvalidTransition:       problem.InvalidTransition,
		contract.ErrQuoteExpired:            problem.QuoteExpired,
		contract.ErrCustomerRequired:        problem.CustomerRequired,
		contract.ErrPriceListChanged:        problem.Conflict,
		contract.ErrRepriceRefused:          problem.Conflict,
		contract.ErrVersionMismatch:         problem.VersionConflict,
		presence.ErrInvalidTransition:       problem.InvalidTransition,
	}
	for err, want := range cases {
		got, _, ok := MapError(fmt.Errorf("wrapped: %w", err))
		if !ok || got != want {
			t.Errorf("%v: %q, %v; want %q", err, got, ok, want)
		}
	}
}

func TestMapError_M3Details(t *testing.T) {
	night := calendar.MustParse("2027-07-01")
	cases := []struct {
		err    error
		typ    problem.Type
		status int
	}{
		{&occupancy.ConflictError{ResourceType: "berth", ResourceID: "b1", From: night, To: night, AllocationIDs: []string{"a1"}}, problem.ResourceTaken, 409},
		{&occupancy.FitError{Result: fit.Result{Blocking: []fit.Issue{{Code: fit.CodeLOAExceeds, Limit: 1200, Actual: 1250}}, Overridable: true}}, problem.FitBlocked, 422},
		{&occupancy.PlacementError{Reason: occupancy.PlacementUnsnapped, AllocationID: "a1"}, problem.PlacementInvalid, 422},
		{&occupancy.ClosureError{ClosureID: "c1", FromDate: night, ToDate: night}, problem.ClosureActive, 409},
		{&occupancy.ReleaseError{Reason: "past_night"}, problem.ReleaseInvalid, 409},
		{&contract.TransitionError{From: "held", Action: contract.ActionNoShow}, problem.InvalidTransition, 409},
		{&harbor.ElementInUseError{References: []string{"b1"}}, problem.InUse, 409},
	}
	for _, c := range cases {
		typ, opts, ok := MapError(fmt.Errorf("op: %w", c.err))
		if !ok || typ != c.typ {
			t.Fatalf("%T: type = %q, %v", c.err, typ, ok)
		}
		p := problem.New(typ, opts...)
		if p.Status != c.status || p.Details == nil {
			t.Fatalf("%T: problem = %+v", c.err, p)
		}
	}
}

func TestMapError_M4Sentinels(t *testing.T) {
	cases := map[error]problem.Type{
		contract.ErrRenewalExists:            problem.RenewalExists,
		contract.ErrProposalClosed:           problem.ProposalClosed,
		contract.ErrInsuranceRequired:        problem.InsuranceRequired,
		contract.ErrCustomerEmailMissing:     problem.CustomerEmailMissing,
		contract.ErrPaperScanRequired:        problem.PaperScanRequired,
		contract.ErrVesselInOtherFleet:       problem.VesselInOtherFleet,
		contract.ErrVesselNotInFleet:         problem.VesselNotInFleet,
		rights.ErrSharesInvalid:              problem.SharesInvalid,
		rights.ErrTableIncomplete:            problem.TableIncomplete,
		rights.ErrTableActive:                problem.TableActive,
		rights.ErrTransferRequirements:       problem.TransferRequirements,
		rights.ErrMandateOverlap:             problem.MandateOverlap,
		docs.ErrTemplateInvalid:              problem.TemplateInvalid,
		docs.ErrTemplateLimits:               problem.TemplateLimits,
		docs.ErrNotReady:                     problem.NotReady,
		docs.ErrCodeTaken:                    problem.CodeTaken,
		signature.ErrOTPInvalid:              problem.OTPInvalid,
		signature.ErrOTPLocked:               problem.OTPLocked,
		signature.ErrOTPRateLimited:          problem.OTPRateLimited,
		signature.ErrSignatureOpen:           problem.SignatureOpen,
		signature.ErrInvalidWebhookSignature: problem.InvalidSignature,
		signature.ErrProviderUnknown:         problem.ProviderUnknown,
		magiclink.ErrInvalid:                 problem.LinkInvalid,
		csvimport.ErrNotUndoable:             problem.ImportNotUndoable,
		csvimport.ErrCSVInvalid:              problem.CSVInvalid,
	}
	for err, want := range cases {
		got, _, ok := MapError(fmt.Errorf("wrapped: %w", err))
		if !ok || got != want {
			t.Errorf("%v: %q, %v; want %q", err, got, ok, want)
		}
	}
}

func TestMapError_M4Details(t *testing.T) {
	cases := []struct {
		err  error
		want problem.Type
	}{
		{&contract.InsuranceError{VesselID: "v1"}, problem.InsuranceRequired},
		{&contract.SettleError{ID: "i1"}, problem.Conflict},
		{&docs.TemplateError{Issues: []docs.TemplateIssue{{Line: 3, Col: 5, Code: "unknown_path", Path: "x.y"}}}, problem.TemplateInvalid},
		{&signature.OTPError{AttemptsLeft: 4}, problem.OTPInvalid},
		{&rights.SumError{Err: rights.ErrSharesInvalid, SumMillis: 999}, problem.SharesInvalid},
		{&rights.SumError{Err: rights.ErrTableIncomplete, SumMillis: 999}, problem.TableIncomplete},
		{&rights.RequirementsError{}, problem.TransferRequirements},
	}
	for _, c := range cases {
		typ, opts, ok := MapError(fmt.Errorf("x: %w", c.err))
		if !ok || typ != c.want || len(opts) != 1 {
			t.Errorf("%v: %q %d %v, want %q with details", c.err, typ, len(opts), ok, c.want)
		}
	}
}

func TestMapError_M5Sentinels(t *testing.T) {
	cases := map[error]problem.Type{
		charge.ErrImmutable:                  problem.ChargeImmutable,
		charge.ErrCancelled:                  problem.ChargeCancelled,
		charge.ErrUseEdit:                    problem.UseEdit,
		charge.ErrExportEmpty:                problem.ExportEmpty,
		charge.ErrExportTooLarge:             problem.ExportTooLarge,
		charge.ErrChargeExists:               problem.ChargeExists,
		charge.ErrNoTerminationAmount:        problem.NoTerminationAmount,
		charge.ErrNotExported:                problem.NotExported,
		charge.ErrBillingItemMissing:         problem.BillingItemMissing,
		charge.ErrCustomerRequired:           problem.CustomerRequired,
		charge.ErrExporterUnknown:            problem.ProviderUnknown,
		charge.ErrNotImplemented:             problem.NotImplemented,
		payment.ErrMatchExceeds:              problem.MatchExceeds,
		payment.ErrRefundExceeds:             problem.RefundExceeds,
		payment.ErrAlreadyReversed:           problem.AlreadyReversed,
		payment.ErrCreditInsufficient:        problem.CreditInsufficient,
		payment.ErrNotDeposit:                problem.NotDeposit,
		payment.ErrNothingToSettle:           problem.NothingToSettle,
		payment.ErrIdempotencyMismatch:       problem.IdempotencyKeyReused,
		payment.ErrReleaseNotResold:          problem.ReleaseNotResold,
		cash.ErrSessionRequired:              problem.CashSessionRequired,
		cash.ErrSessionOpen:                  problem.CashSessionOpen,
		cash.ErrSessionClosed:                problem.CashSessionClosed,
		cash.ErrInsufficient:                 problem.CashInsufficient,
		cash.ErrRegisterInUse:                problem.RegisterInUse,
		collect.ErrProviderDisabled:          problem.ProviderDisabled,
		collect.ErrProviderRefundUnsupported: problem.ProviderRefundUnsupported,
		payment.ErrProviderRefundUnsupported: problem.ProviderRefundUnsupported,
		collect.ErrProviderError:             problem.ProviderError,
		collect.ErrChargesNotOpen:            problem.ChargesNotOpen,
		collect.ErrInvalidWebhookSignature:   problem.InvalidSignature,
		collect.ErrLinkInvalid:               problem.LinkInvalid,
		collect.ErrRetryLater:                problem.NotReady,
		contract.ErrNotImplementedM5:         problem.NotImplemented,
	}
	for err, want := range cases {
		got, _, ok := MapError(fmt.Errorf("wrapped: %w", err))
		if !ok || got != want {
			t.Errorf("%v: %q, %v; want %q", err, got, ok, want)
		}
	}
}

func TestMapError_M5Details(t *testing.T) {
	cases := []struct {
		err  error
		want problem.Type
	}{
		{&payment.LimitError{Limit: payment.LimitCharge, ChargeID: "c1"}, problem.MatchExceeds},
		{&charge.ExportTooLargeError{Count: 20001}, problem.ExportTooLarge},
		{&collect.ProviderError{Code: "card_declined"}, problem.ProviderError},
	}
	for _, c := range cases {
		typ, opts, ok := MapError(fmt.Errorf("x: %w", c.err))
		if !ok || typ != c.want || len(opts) != 1 {
			t.Errorf("%v: %q %d %v, want %q with details", c.err, typ, len(opts), ok, c.want)
		}
	}
}
