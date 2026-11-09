package respond

import (
	"fmt"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/profile"
	"github.com/coste360/porto/internal/domain/sensor"
	"github.com/coste360/porto/internal/domain/sensor/watch"
	"github.com/coste360/porto/internal/http/problem"
)

func TestMapError_M12Sentinels(t *testing.T) {
	cases := map[error]problem.Type{
		sensor.ErrVersionMismatch:                               problem.VersionConflict,
		sensor.ErrCodeTaken:                                     problem.CodeTaken,
		sensor.ErrSegmentOverlap:                                problem.SegmentOverlap,
		sensor.ErrResourceHasSensor:                             problem.ResourceHasSensor,
		sensor.ErrInvalidResource:                               problem.InvalidSensorResource,
		sensor.ErrIngestUnauthorized:                            problem.IngestUnauthorized,
		sensor.ErrInvalidPayload:                                problem.InvalidPayload,
		sensor.ErrBusy:                                          problem.NotReady,
		watch.ErrAlertStale:                                     problem.AlertStale,
		watch.ErrActionNotAllowed:                               problem.AlertActionNotAllowed,
		watch.ErrConsentRequired:                                problem.ConsentRequired,
		profile.ErrPhotoInvalid:                                 problem.PhotoInvalid,
		profile.ErrUnsupportedMedia:                             problem.UnsupportedMediaType,
		profile.ErrPhotoTooLarge:                                problem.BadRequest,
		profile.ErrNotPublished:                                 problem.NotFound,
		pricelist.ErrNoPublicMatrix:                             problem.NotFound,
		sensor.ErrNotImplemented:                                problem.NotImplemented,
		watch.ErrNotImplemented:                                 problem.NotImplemented,
		profile.ErrNotImplemented:                               problem.NotImplemented,
		&sensor.TimestampError{}:                                problem.IngestTimestampOutOfRange,
		&sensor.RateLimitError{RetryAfter: time.Second}:         problem.RateLimited,
		&profile.IncompleteError{Missing: []string{"position"}}: problem.ProfileIncomplete,
	}
	for err, want := range cases {
		got, _, ok := MapError(fmt.Errorf("wrapped: %w", err))
		if !ok || got != want {
			t.Errorf("%v: %q, %v; want %q", err, got, ok, want)
		}
	}
}
