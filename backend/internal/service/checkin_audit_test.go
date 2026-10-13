package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckinAuditRejectsMalformedDatesAndTypes(t *testing.T) {
	s := NewCheckinService(nil, nil, nil)
	for _, f := range []CheckinHistoryFilter{{Limit: 20, DateFrom: "2026-02-30"}, {Limit: 20, DateFrom: "2026-10-14", DateTo: "2026-10-13"}, {Limit: 20, RewardType: "unsafe"}, {Limit: 1000}} {
		_, err := s.RewardHistory(context.Background(), f)
		require.Error(t, err)
	}
	_, err := s.Budgets(context.Background(), "x", "", 30)
	require.Error(t, err)
}
