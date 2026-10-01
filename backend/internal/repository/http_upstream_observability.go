package repository

import (
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"go.uber.org/zap"
)

// observeUpstreamDo adds tracing at the HTTP boundary. The caller retains
// ownership of cancellation, decompression and response-body lifecycle.
func observeUpstreamDo(client *http.Client, req *http.Request, accountID int64) (*http.Response, error) {
	if requestobs.FromContext(req.Context()) == nil {
		return servertiming.Do(client, req)
	}
	startedAt := time.Now()
	var traceMu sync.Mutex
	var connectStartedAt time.Time
	var connectDuration time.Duration
	var firstByteDuration time.Duration
	var connectionReused bool

	trace := &httptrace.ClientTrace{
		ConnectStart: func(_, _ string) {
			traceMu.Lock()
			defer traceMu.Unlock()
			if connectStartedAt.IsZero() {
				connectStartedAt = time.Now()
			}
		},
		ConnectDone: func(_, _ string, _ error) {
			traceMu.Lock()
			defer traceMu.Unlock()
			if !connectStartedAt.IsZero() && connectDuration == 0 {
				connectDuration = time.Since(connectStartedAt)
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			traceMu.Lock()
			connectionReused = info.Reused
			traceMu.Unlock()
		},
		GotFirstResponseByte: func() {
			traceMu.Lock()
			if firstByteDuration == 0 {
				firstByteDuration = time.Since(startedAt)
			}
			traceMu.Unlock()
		},
	}
	ctx := httptrace.WithClientTrace(req.Context(), trace)
	resp, err := servertiming.Do(client, req.WithContext(ctx))
	duration := time.Since(startedAt)
	traceMu.Lock()
	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}
	attempt := requestobs.UpstreamAttempt{
		StatusCode:       statusCode,
		Duration:         duration,
		ConnectDuration:  connectDuration,
		FirstByte:        firstByteDuration,
		ConnectionReused: connectionReused,
		Failed:           err != nil,
	}
	traceMu.Unlock()
	if state := requestobs.FromContext(req.Context()); state != nil {
		state.RecordUpstreamAttempt(attempt)
	}
	logUpstreamAttempt(req, accountID, attempt, err)
	return resp, err
}

func logUpstreamAttempt(req *http.Request, accountID int64, attempt requestobs.UpstreamAttempt, err error) {
	// An optional logging sink must not interrupt body/cancellation ownership
	// in the caller. Ordinary write errors are already handled by Zap.
	defer func() { _ = recover() }()
	fields := []zap.Field{
		zap.String("component", "http.upstream"),
		zap.Int64("account_id", accountID),
		zap.Int("status_code", attempt.StatusCode),
		zap.Int64("duration_ms", attempt.Duration.Milliseconds()),
		zap.Bool("connection_reused", attempt.ConnectionReused),
	}
	if attempt.ConnectDuration > 0 {
		fields = append(fields, zap.Int64("connect_ms", attempt.ConnectDuration.Milliseconds()))
	}
	if attempt.FirstByte > 0 {
		fields = append(fields, zap.Int64("first_byte_ms", attempt.FirstByte.Milliseconds()))
	}
	if req != nil {
		fields = append(fields, zap.String("method", req.Method))
		if state := requestobs.FromContext(req.Context()); state != nil {
			snapshot := state.Snapshot()
			fields = append(fields,
				zap.Int("request_upstream_attempts", snapshot.UpstreamAttempts),
				zap.Bool("downstream_started", !snapshot.FirstWriteAt.IsZero()),
			)
		}
		if profile := service.HTTPUpstreamProfileFromContext(req.Context()); profile != "" {
			fields = append(fields, zap.String("upstream_profile", string(profile)))
		}
	}
	if err != nil {
		fields = append(fields, zap.String("error_class", requestobs.ErrorClass(err)))
	}
	requestLog := logger.FromContext(req.Context())
	if err != nil || attempt.StatusCode >= http.StatusInternalServerError {
		requestLog.Warn("http upstream request completed", fields...)
		return
	}
	requestLog.Info("http upstream request completed", fields...)
}
