package middleware

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

type remediationBodySnapshot struct {
	bytes     int64
	complete  bool
	errorKind string
	duration  time.Duration
	started   bool
}

// Read and Close retain the underlying body's return values and cancellation
// semantics. The mutex protects observation when net/http closes during Read.
type remediationBody struct {
	io.ReadCloser
	declared  int64
	log       *zap.Logger
	mu        sync.Mutex
	bytes     int64
	eof       bool
	errorKind string
	readTime  time.Duration
	started   bool
}

func newRemediationBody(req *http.Request, l *zap.Logger) *remediationBody {
	body := req.Body
	if body == nil {
		body = http.NoBody
	}
	return &remediationBody{ReadCloser: body, declared: req.ContentLength, log: l}
}

func (b *remediationBody) Read(p []byte) (int, error) {
	start := time.Now()
	n, err := b.ReadCloser.Read(p)
	elapsed := time.Since(start)
	b.mu.Lock()
	b.started = true
	b.bytes += int64(n)
	b.readTime += elapsed
	if errors.Is(err, io.EOF) {
		b.eof = true
	} else if err != nil && b.errorKind == "" {
		b.errorKind = remediationBodyErrorKind(err)
	}
	b.mu.Unlock()
	return n, err
}

// Close is delegated verbatim; a close error is not a request-body read error.
func (b *remediationBody) Close() error { return b.ReadCloser.Close() }

func (b *remediationBody) snapshot() remediationBodySnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	complete := b.errorKind == "" && (b.eof || (b.started && b.declared > 0 && b.bytes >= b.declared))
	if b.declared > 0 && b.bytes < b.declared {
		complete = false
	}
	kind := b.errorKind
	if kind == "" && !complete {
		switch {
		case !b.started:
			kind = "not_read"
		case b.eof && b.declared > b.bytes:
			kind = "truncated_body"
		default:
			kind = "not_complete"
		}
	}
	return remediationBodySnapshot{bytes: b.bytes, complete: complete, errorKind: kind, duration: b.readTime, started: b.started}
}

func (b *remediationBody) logCompletion() {
	// Optional telemetry must not turn a successful handler into a failure.
	defer func() { _ = recover() }()
	s := b.snapshot()
	if b.log == nil || !s.started {
		return
	}
	b.log.Info("gateway remediation body read completed",
		zap.Int64("declared_content_length", b.declared),
		zap.Int64("body_bytes_read", s.bytes),
		zap.Bool("body_read_complete", s.complete),
		zap.String("body_read_error_kind", s.errorKind),
		// Sum of time inside underlying Read calls; excludes handler processing,
		// authentication, and pauses between calls. Not client upload throughput.
		zap.Int64("body_read_ms", s.duration.Milliseconds()),
	)
}

func remediationBodyErrorKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return "client_disconnect"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "truncated_body"
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return "max_bytes"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "transport"
	}
	return "io_read"
}
