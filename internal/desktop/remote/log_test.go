package remote

import (
	"io"
	"log/slog"
	"sync"
)

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(b)
}

func testLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(&syncWriter{w: w}, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
