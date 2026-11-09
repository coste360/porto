// Package fakesensor is an in-memory ext.SensorSource for tests, development and the demo (m12-plan §3.4, D27):
// readings pushed with Push (per marina) are returned by Poll after the cursor, which is the decimal count of
// readings already returned. Poll is idempotent for a given cursor. It is registered only in dev, demo and tests.
package fakesensor

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/coste360/porto/pkg/ext"
)

// Code is the driver code.
const Code = "fake"

// ErrUnavailable is the error Poll returns after Fail (tests of the error class).
var ErrUnavailable = errors.New("fakesensor: unavailable")

// Source is the fake source.
type Source struct {
	mu       sync.Mutex
	readings map[string][]ext.SensorReading // by marina
	fail     error
	polls    int
	noHTTP   int // polls without the host's egress client
}

// New returns an empty fake source.
func New() *Source { return &Source{readings: map[string][]ext.SensorReading{}} }

// Code implements ext.SensorSource.
func (s *Source) Code() string { return Code }

// ConfigKeys implements ext.SensorSource: one public key (label) and one secret (token), to exercise the config
// handling.
func (s *Source) ConfigKeys() (public []string, secret []string) {
	return []string{"label"}, []string{"token"}
}

// Push appends readings for a marina.
func (s *Source) Push(marinaID string, rs ...ext.SensorReading) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readings[marinaID] = append(s.readings[marinaID], rs...)
}

// Fail makes the next polls return err (nil restores normal polling).
func (s *Source) Fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = err
}

// Polls returns the number of Poll calls.
func (s *Source) Polls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.polls
}

// PollsWithoutHTTP returns the number of Poll calls whose config carried no egress client (a real driver would
// then have no anti-SSRF client to use).
func (s *Source) PollsWithoutHTTP() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.noHTTP
}

// Poll implements ext.SensorSource.
func (s *Source) Poll(ctx context.Context, cfg ext.SensorSourceConfig, cursor string, limit int) ([]ext.SensorReading, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polls++
	if cfg.HTTP == nil {
		s.noHTTP++
	}
	if err := ctx.Err(); err != nil {
		return nil, cursor, err
	}
	if s.fail != nil {
		return nil, cursor, s.fail
	}
	from := 0
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 0 {
			return nil, cursor, errors.New("fakesensor: bad cursor")
		}
		from = n
	}
	all := s.readings[cfg.MarinaID]
	if from > len(all) {
		from = len(all)
	}
	to := min(len(all), from+max(limit, 0))
	out := append([]ext.SensorReading(nil), all[from:to]...)
	return out, strconv.Itoa(to), nil
}

var _ ext.SensorSource = (*Source)(nil)
