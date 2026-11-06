// Package httpsink is a local HTTP receiver for tests of outgoing requests (webhooks). Domain tests use it so
// that they do not import net/http themselves.
package httpsink

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// Request is one received request.
type Request struct {
	Method string
	Path   string
	Header map[string]string // first value of each header, canonical keys
	Body   []byte
}

// Sink records requests and answers with a configurable status.
type Sink struct {
	URL string

	status atomic.Int32
	mu     sync.Mutex
	reqs   []Request
}

// New starts a sink answering 200; it stops when the test ends.
func New(t testing.TB) *Sink {
	t.Helper()
	s := &Sink{}
	s.status.Store(http.StatusOK)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		h := map[string]string{}
		for k, v := range r.Header {
			h[k] = v[0]
		}
		s.mu.Lock()
		s.reqs = append(s.reqs, Request{Method: r.Method, Path: r.URL.Path, Header: h, Body: b})
		s.mu.Unlock()
		w.WriteHeader(int(s.status.Load()))
		_, _ = w.Write([]byte("received \x00 ok"))
	}))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// SetStatus changes the response status.
func (s *Sink) SetStatus(code int) { s.status.Store(int32(code)) }

// Requests returns the received requests.
func (s *Sink) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.reqs...)
}

// Last returns the last received request.
func (s *Sink) Last() Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reqs[len(s.reqs)-1]
}

// Count returns the number of received requests.
func (s *Sink) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reqs)
}
