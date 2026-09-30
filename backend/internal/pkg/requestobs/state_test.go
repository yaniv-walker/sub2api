package requestobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStateTracksWritesAndUpstreamAttemptsWithoutPayloads(t *testing.T) {
	start := time.Unix(100, 0)
	state := New(start)
	state.ObserveWrite(3, nil, start.Add(10*time.Millisecond))
	state.ObserveWrite(2, errors.New("client reset"), start.Add(80*time.Millisecond))
	state.RecordUpstreamAttempt(UpstreamAttempt{
		StatusCode:      503,
		Duration:        120 * time.Millisecond,
		ConnectDuration: 10 * time.Millisecond,
		Failed:          true,
	})

	snapshot := state.Snapshot()
	require.Equal(t, int64(5), snapshot.BytesWritten)
	require.Equal(t, int64(2), snapshot.WriteCount)
	require.Equal(t, 70*time.Millisecond, snapshot.MaxWriteGap)
	require.Equal(t, start.Add(80*time.Millisecond), snapshot.LastWriteAt)
	require.True(t, snapshot.WriteFailed)
	require.Equal(t, 1, snapshot.UpstreamAttempts)
	require.Equal(t, 1, snapshot.UpstreamErrors)
	require.Equal(t, httpStatusServiceUnavailable, snapshot.UpstreamLastStatus)
}

func TestStateContextRoundTrip(t *testing.T) {
	state := New(time.Now())
	ctx := WithContext(context.Background(), state)
	require.Same(t, state, FromContext(ctx))
	require.Nil(t, FromContext(context.Background()))
}

func TestErrorClassDoesNotExposeRawError(t *testing.T) {
	require.Equal(t, "context_canceled", ErrorClass(context.Canceled))
	require.Equal(t, "context_deadline", ErrorClass(context.DeadlineExceeded))
	require.Equal(t, "transport_error", ErrorClass(errors.New("secret upstream response")))
	require.Empty(t, ErrorClass(nil))
}

const httpStatusServiceUnavailable = 503
