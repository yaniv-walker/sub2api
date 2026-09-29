package handler

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/gin-gonic/gin"
)

func TestParseAnalyticsRangeRejectsInvalidDays(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest("GET", "/?days=91", nil)
	if _, err := parseAnalyticsRange(ctx); err == nil {
		t.Fatal("expected days validation error")
	}
}

func TestParseAnalyticsRangePrefersExplicitDates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest("GET", "/?days=1&from=2026-09-01T00:00:00Z&to=2026-09-03T00:00:00Z", nil)
	rng, err := parseAnalyticsRange(ctx)
	if err != nil {
		t.Fatalf("parse range: %v", err)
	}
	if rng.Days != 2 || !rng.From.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected range: %+v", rng)
	}
}

func TestUniqueSnapshotsDeduplicatesSharedBalanceTimestamps(t *testing.T) {
	now := time.Now().UTC()
	snapshots := []*ent.UpstreamBalanceSnapshot{
		{ID: 2, SnapshotAt: now, Balance: 90},
		{ID: 1, SnapshotAt: now, Balance: 90},
		{ID: 3, SnapshotAt: now.Add(time.Hour), Balance: 89},
	}
	got := uniqueSnapshots(snapshots)
	if len(got) != 2 {
		t.Fatalf("got %d snapshots, want 2", len(got))
	}
	if got[0].ID != 1 || got[1].Balance != 89 {
		t.Fatalf("unexpected deduplication: %+v", got)
	}
}
