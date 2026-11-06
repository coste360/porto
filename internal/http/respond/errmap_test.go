package respond

import (
	"fmt"
	"testing"

	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/domain/layout"
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
