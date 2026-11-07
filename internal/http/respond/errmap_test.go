package respond

import (
	"fmt"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/domainerr"
	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/http/problem"
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
