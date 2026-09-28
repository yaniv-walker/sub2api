package handler

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/service"
	"github.com/gin-gonic/gin"
)

type analyticsRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	Days int       `json:"days"`
}

func parseAnalyticsRange(c *gin.Context) (analyticsRange, error) {
	now := time.Now().UTC()
	days := 30
	if raw := c.Query("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 90 {
			return analyticsRange{}, fmt.Errorf("days must be between 1 and 90")
		}
		days = parsed
	}
	if c.Query("from") == "" && c.Query("to") == "" {
		return analyticsRange{From: now.AddDate(0, 0, -days), To: now, Days: days}, nil
	}
	if c.Query("from") == "" || c.Query("to") == "" {
		return analyticsRange{}, fmt.Errorf("from and to must be specified together")
	}
	from, err := time.Parse(time.RFC3339, c.Query("from"))
	if err != nil {
		return analyticsRange{}, fmt.Errorf("from must be RFC3339")
	}
	to, err := time.Parse(time.RFC3339, c.Query("to"))
	if err != nil {
		return analyticsRange{}, fmt.Errorf("to must be RFC3339")
	}
	if !from.Before(to) || to.After(now) || to.Sub(from) > 90*24*time.Hour {
		return analyticsRange{}, fmt.Errorf("invalid or future time range (maximum 90 days)")
	}
	return analyticsRange{From: from.UTC(), To: to.UTC(), Days: int(math.Ceil(to.Sub(from).Hours() / 24))}, nil
}

func analyticsError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func (h *MonitorHandler) analyticsUpstream(c *gin.Context) (monitorUpstream, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		analyticsError(c, 404, "UPSTREAM_NOT_FOUND", "upstream not found")
		return monitorUpstream{}, false
	}
	upstreams, err := h.loadMonitorUpstreams(c.Request.Context())
	if err != nil {
		analyticsError(c, 500, "ANALYTICS_UNAVAILABLE", "unable to read upstream data")
		return monitorUpstream{}, false
	}
	for _, upstream := range upstreams {
		if upstream.ID != id || !upstream.Configured || len(upstream.Accounts) == 0 {
			continue
		}
		if !upstream.Enabled {
			analyticsError(c, 409, "UPSTREAM_DISABLED", "upstream is disabled")
			return monitorUpstream{}, false
		}
		return upstream, true
	}
	analyticsError(c, 404, "UPSTREAM_NOT_FOUND", "upstream not found or has no associated accounts")
	return monitorUpstream{}, false
}

func upstreamAccountIDs(upstream monitorUpstream) []int64 {
	ids := make([]int64, 0, len(upstream.Accounts))
	for _, account := range upstream.Accounts {
		ids = append(ids, account.info.ID)
	}
	return ids
}

func (h *MonitorHandler) GetUpstreamErrors(c *gin.Context) {
	rng, err := parseAnalyticsRange(c)
	if err != nil {
		analyticsError(c, 400, "INVALID_RANGE", err.Error())
		return
	}
	upstream, ok := h.analyticsUpstream(c)
	if !ok {
		return
	}
	records, err := h.analyzer.RecordsForUpstream(c.Request.Context(), upstream.Type, upstreamAccountIDs(upstream), rng.From, rng.To)
	if err != nil {
		analyticsError(c, 500, "ANALYTICS_UNAVAILABLE", "unable to read error history")
		return
	}
	type typeRow struct {
		Type     string    `json:"type"`
		Count    int       `json:"count"`
		LastSeen time.Time `json:"last_seen"`
	}
	type dayRow struct {
		Date  string `json:"date"`
		Count int    `json:"count"`
	}
	byType := make(map[string]*typeRow)
	byDay := make(map[string]int)
	affected := make(map[int64]bool)
	recent := make([]gin.H, 0, 20)
	for _, record := range records {
		affected[record.AccountID] = true
		row := byType[record.ErrorType]
		if row == nil {
			row = &typeRow{Type: record.ErrorType}
			byType[record.ErrorType] = row
		}
		row.Count++
		if record.OccurredAt.After(row.LastSeen) {
			row.LastSeen = record.OccurredAt
		}
		date := record.OccurredAt.UTC().Format("2006-01-02")
		byDay[date]++
		if len(recent) < 20 {
			var code *string
			if record.HTTPStatus != nil {
				value := strconv.Itoa(*record.HTTPStatus)
				code = &value
			}
			// Do not echo upstream error bodies: they may contain credentials.
			recent = append(recent, gin.H{"account_id": record.AccountID, "error_type": record.ErrorType, "error_code": code, "message": "上游请求失败", "occurred_at": record.OccurredAt})
		}
	}
	types := make([]typeRow, 0, len(byType))
	for _, row := range byType {
		types = append(types, *row)
	}
	sort.Slice(types, func(i, j int) bool {
		if types[i].Count == types[j].Count {
			return types[i].Type < types[j].Type
		}
		return types[i].Count > types[j].Count
	})
	daily := make([]dayRow, 0, len(byDay))
	for date, count := range byDay {
		daily = append(daily, dayRow{date, count})
	}
	sort.Slice(daily, func(i, j int) bool { return daily[i].Date < daily[j].Date })
	c.JSON(http.StatusOK, gin.H{"upstream_id": upstream.ID, "upstream": gin.H{"name": upstream.Name, "base_url": upstream.BaseURL, "type": upstream.Type}, "range": rng, "total_errors": len(records), "error_rate": nil, "error_rate_unit": "unknown", "affected_account_count": len(affected), "by_type": types, "daily": daily, "recent": recent})
}

// uniqueSnapshots never adds balances from different accounts: each event is
// one observation of the same upstream wallet. Equal timestamps are deduped.
func uniqueSnapshots(snapshots []*ent.UpstreamBalanceSnapshot) []*ent.UpstreamBalanceSnapshot {
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].SnapshotAt.Equal(snapshots[j].SnapshotAt) {
			return snapshots[i].ID < snapshots[j].ID
		}
		return snapshots[i].SnapshotAt.Before(snapshots[j].SnapshotAt)
	})
	result := make([]*ent.UpstreamBalanceSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if len(result) == 0 || !snapshot.SnapshotAt.Equal(result[len(result)-1].SnapshotAt) {
			result = append(result, snapshot)
		}
	}
	return result
}

func (h *MonitorHandler) snapshotsForUpstream(c *gin.Context, upstream monitorUpstream, rng analyticsRange) ([]*ent.UpstreamBalanceSnapshot, bool) {
	snapshots, err := h.predictor.UpstreamSnapshots(c.Request.Context(), upstream.Type, upstreamAccountIDs(upstream), rng.From, rng.To)
	if err != nil {
		analyticsError(c, 500, "ANALYTICS_UNAVAILABLE", "unable to read balance history")
		return nil, false
	}
	return uniqueSnapshots(snapshots), true
}

func (h *MonitorHandler) GetUpstreamUsage(c *gin.Context) {
	rng, err := parseAnalyticsRange(c)
	if err != nil {
		analyticsError(c, 400, "INVALID_RANGE", err.Error())
		return
	}
	upstream, ok := h.analyticsUpstream(c)
	if !ok {
		return
	}
	snapshots, ok := h.snapshotsForUpstream(c, upstream, rng)
	if !ok {
		return
	}
	type dailyRow struct {
		Date         string  `json:"date"`
		Cost         float64 `json:"cost"`
		Balance      float64 `json:"balance"`
		RequestCount *int    `json:"request_count"`
	}
	daily := make([]dailyRow, 0)
	anomalies := make([]gin.H, 0)
	var start, end *float64
	totalCost := 0.0
	currency := "CNY"
	if len(snapshots) > 0 {
		a, b := snapshots[0].Balance, snapshots[len(snapshots)-1].Balance
		start, end = &a, &b
		if snapshots[0].Currency != "" {
			currency = snapshots[0].Currency
		}
	}
	byDay := make(map[string]*dailyRow)
	for i := 1; i < len(snapshots); i++ {
		current := snapshots[i]
		date := current.SnapshotAt.UTC().Format("2006-01-02")
		row := byDay[date]
		if row == nil {
			row = &dailyRow{Date: date}
			byDay[date] = row
		}
		row.Balance = current.Balance
		delta := snapshots[i-1].Balance - current.Balance
		if delta > 0 {
			row.Cost += delta
			totalCost += delta
		} else if delta < 0 {
			anomalies = append(anomalies, gin.H{"date": date, "kind": "balance_increased", "amount": -delta})
		}
	}
	for _, row := range byDay {
		daily = append(daily, *row)
	}
	sort.Slice(daily, func(i, j int) bool { return daily[i].Date < daily[j].Date })
	c.JSON(http.StatusOK, gin.H{"upstream_id": upstream.ID, "range": rng, "currency": currency, "starting_balance": start, "ending_balance": end, "total_cost": totalCost, "average_daily_cost": totalCost / float64(rng.Days), "total_requests": nil, "daily": daily, "anomalies": anomalies, "data_quality": gin.H{"snapshot_count": len(snapshots), "request_events_available": false}})
}

func (h *MonitorHandler) GetUpstreamPrediction(c *gin.Context) {
	upstream, ok := h.analyticsUpstream(c)
	if !ok {
		return
	}
	rng := analyticsRange{From: time.Now().UTC().AddDate(0, 0, -90), To: time.Now().UTC(), Days: 90}
	snapshots, ok := h.snapshotsForUpstream(c, upstream, rng)
	if !ok {
		return
	}
	var balance *float64
	var burn, daysLeft, confidence *float64
	var depleted *time.Time
	if len(snapshots) > 0 {
		value := snapshots[len(snapshots)-1].Balance
		balance = &value
	}
	if len(snapshots) >= 2 {
		points := make([]service.BalanceDataPoint, len(snapshots))
		for i, snapshot := range snapshots {
			points[i] = service.BalanceDataPoint{Date: snapshot.SnapshotAt, Balance: snapshot.Balance}
		}
		result, err := h.predictor.Predict(0, *balance, points)
		if err == nil && result.DailyAverageCost > 0 && result.PredictedDaysLeft >= 0 && !result.PredictedDepletedAt.IsZero() {
			burn, daysLeft, confidence = &result.DailyAverageCost, &result.PredictedDaysLeft, &result.Confidence
			depleted = &result.PredictedDepletedAt
		}
	}
	c.JSON(http.StatusOK, gin.H{"upstream_id": upstream.ID, "current_balance": balance, "daily_burn_rate": burn, "estimated_days_left": daysLeft, "estimated_depletion_date": depleted, "confidence": confidence, "algorithm": "moving_average", "data_points": len(snapshots)})
}
