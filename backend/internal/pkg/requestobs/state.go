// Package requestobs contains the bounded, request-scoped state used by the
// HTTP observability middleware. It intentionally has no exporter dependency:
// the existing structured logger is the first integration point.
package requestobs

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

type contextKey struct{}

// TerminalReason is a conservative classification of how a request ended.
// Unknown is preferred over an unsupported inference.
type TerminalReason string

const (
	TerminalCompleted       TerminalReason = "completed"
	TerminalClientCancelled TerminalReason = "client_cancelled"
	TerminalDownstreamWrite TerminalReason = "downstream_write_error"
	TerminalUpstreamError   TerminalReason = "upstream_error"
	TerminalDeadline        TerminalReason = "deadline"
	TerminalHandlerError    TerminalReason = "handler_error"
	TerminalUnknown         TerminalReason = "unknown"
)

// UpstreamAttempt is the bounded summary of one upstream HTTP attempt.
// ErrorText is deliberately absent so raw upstream errors cannot enter logs.
type UpstreamAttempt struct {
	StatusCode       int
	Duration         time.Duration
	ConnectDuration  time.Duration
	FirstByte        time.Duration
	ConnectionReused bool
	Failed           bool
}

// Snapshot is safe to copy and use after a request has completed.
type Snapshot struct {
	StartedAt             time.Time
	FirstWriteAt          time.Time
	LastWriteAt           time.Time
	MaxWriteGap           time.Duration
	BytesWritten          int64
	WriteCount            int64
	WriteFailed           bool
	StatusCode            int
	UpstreamAttempts      int
	UpstreamErrors        int
	UpstreamLastStatus    int
	UpstreamLastDuration  time.Duration
	UpstreamTotalDuration time.Duration
}

// State tracks only bounded numeric and enum data. It never stores request or
// response bodies.
type State struct {
	mu sync.Mutex

	startedAt    time.Time
	firstWriteAt time.Time
	lastWriteAt  time.Time
	maxWriteGap  time.Duration
	bytesWritten int64
	writeCount   int64
	writeFailed  bool
	statusCode   int

	upstreamAttempts      int
	upstreamErrors        int
	upstreamLastStatus    int
	upstreamLastDuration  time.Duration
	upstreamTotalDuration time.Duration
}

func New(startedAt time.Time) *State {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return &State{startedAt: startedAt}
}

func WithContext(ctx context.Context, state *State) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if state == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, state)
}

func FromContext(ctx context.Context) *State {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(contextKey{}).(*State)
	return state
}

func (s *State) ObserveWrite(n int, err error, now time.Time) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > 0 {
		if s.firstWriteAt.IsZero() {
			s.firstWriteAt = now
		}
		if !s.lastWriteAt.IsZero() {
			if gap := now.Sub(s.lastWriteAt); gap > s.maxWriteGap {
				s.maxWriteGap = gap
			}
		}
		s.lastWriteAt = now
		s.bytesWritten += int64(n)
		s.writeCount++
	}
	if err != nil {
		s.writeFailed = true
	}
}

func (s *State) SetStatus(statusCode int) {
	if s == nil || statusCode <= 0 {
		return
	}
	s.mu.Lock()
	s.statusCode = statusCode
	s.mu.Unlock()
}

func (s *State) RecordUpstreamAttempt(attempt UpstreamAttempt) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.upstreamAttempts++
	if attempt.Failed || attempt.StatusCode >= http.StatusInternalServerError {
		s.upstreamErrors++
	}
	s.upstreamLastStatus = attempt.StatusCode
	s.upstreamLastDuration = attempt.Duration
	s.upstreamTotalDuration += attempt.Duration
	s.mu.Unlock()
}

func (s *State) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{
		StartedAt:             s.startedAt,
		FirstWriteAt:          s.firstWriteAt,
		LastWriteAt:           s.lastWriteAt,
		MaxWriteGap:           s.maxWriteGap,
		BytesWritten:          s.bytesWritten,
		WriteCount:            s.writeCount,
		WriteFailed:           s.writeFailed,
		StatusCode:            s.statusCode,
		UpstreamAttempts:      s.upstreamAttempts,
		UpstreamErrors:        s.upstreamErrors,
		UpstreamLastStatus:    s.upstreamLastStatus,
		UpstreamLastDuration:  s.upstreamLastDuration,
		UpstreamTotalDuration: s.upstreamTotalDuration,
	}
}

func ErrorClass(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline"
	default:
		return "transport_error"
	}
}
