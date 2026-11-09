package respond

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/rental/fleet"
	"github.com/coste360/porto/internal/domain/rental/handover"
	"github.com/coste360/porto/internal/domain/rental/reservation"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/http/problem"
)

func TestMapError_M11Sentinels(t *testing.T) {
	cases := map[error]problem.Type{
		fleet.ErrVersionMismatch:             problem.VersionConflict,
		fleet.ErrCodeTaken:                   problem.CodeTaken,
		fleet.ErrBandGapLtBuffer:             problem.BandGapLtBuffer,
		reservation.ErrInvalidTransition:     problem.InvalidTransition,
		reservation.ErrSlotTaken:             problem.RentalSlotTaken,
		reservation.ErrSkipperBusy:           problem.SkipperBusy,
		reservation.ErrHoldExpired:           problem.RentalHoldExpired,
		reservation.ErrUnitNotBookable:       problem.UnitNotBookable,
		reservation.ErrSkipperRequired:       problem.SkipperRequired,
		reservation.ErrSkipperUnavailable:    problem.SkipperUnavailable,
		reservation.ErrSkipperLicenceExpired: problem.SkipperLicenceExpired,
		handover.ErrHandoverLocked:           problem.HandoverLocked,
		handover.ErrEngineHoursDecreasing:    problem.EngineHoursDecreasing,
		handover.ErrLicenceRequired:          problem.LicenceRequired,
		handover.ErrLicenceExpired:           problem.LicenceExpired,
		handover.ErrPreauthUnavailable:       problem.PreauthUnavailable,
		handover.ErrDepositExceeds:           problem.DepositExceeds,
		handover.ErrNothingToCollect:         problem.NothingToCollect,
		handover.ErrCustomerRequired:         problem.CustomerRequired,
		handover.ErrOfflineTimeOutOfRange:    problem.OfflineTimeOutOfRange,
		fleet.ErrNotImplemented:              problem.NotImplemented,
		reservation.ErrNotImplemented:        problem.NotImplemented,
		handover.ErrNotImplemented:           problem.NotImplemented,
	}
	for err, want := range cases {
		got, _, ok := MapError(fmt.Errorf("wrapped: %w", err))
		if !ok || got != want {
			t.Errorf("%v: %q, %v; want %q", err, got, ok, want)
		}
	}
}

func TestMapError_M11Details(t *testing.T) {
	d := calendar.MustParse("2027-06-12")
	cases := []struct {
		err  error
		want problem.Type
		has  []string
	}{
		{
			&reservation.SlotConflict{Target: reservation.TargetUnit, ResourceID: "u1", Date: d, Slots: []int{24}, BookingIDs: []string{"b1"}},
			problem.RentalSlotTaken,
			[]string{`"target":"unit"`, `"slots":[24]`, `"block_ids":[]`, `"date":"2027-06-12"`},
		},
		{&reservation.SlotConflict{Target: reservation.TargetSkipper, Date: d}, problem.SkipperBusy, []string{`"booking_ids":[]`}},
		{&handover.IncompleteError{Missing: []string{"identity", "deposit"}}, problem.HandoverIncomplete, []string{`"missing":["identity","deposit"]`}},
		{&slots.RangeError{Code: slots.CodeOutsideRentalHours, Kind: "hourly"}, problem.OutsideRentalHours, []string{`"kind":"hourly"`}},
		{&slots.RangeError{Code: slots.CodeFormulaRangeMismatch, Kind: "half_day", Expected: "morning"}, problem.FormulaRangeMismatch, []string{`"expected":"morning"`}},
	}
	for _, c := range cases {
		typ, opts, ok := MapError(fmt.Errorf("x: %w", c.err))
		if !ok || typ != c.want || len(opts) != 1 {
			t.Fatalf("%v: %q %d %v, want %q with details", c.err, typ, len(opts), ok, c.want)
		}
		p := problem.New(typ, opts...)
		b, _ := json.Marshal(p.Details)
		for _, h := range c.has {
			if !strings.Contains(string(b), h) {
				t.Errorf("%s: details %s miss %s", c.want, b, h)
			}
		}
	}
}
