package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *checkinRepository) RewardHistory(ctx context.Context, filter service.CheckinHistoryFilter) (*service.CheckinHistory, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+checkinRecordColumns+` FROM checkin_records
 WHERE ($1::bigint=0 OR user_id=$1) AND ($2::bigint=0 OR id<$2)
 AND ($4::text='' OR business_date>=NULLIF($4,'')::date)
 AND ($5::text='' OR business_date<=NULLIF($5,'')::date)
 AND ($6::text='' OR reward_type=$6) ORDER BY id DESC LIMIT $3`, filter.UserID, filter.Cursor, filter.Limit+1, filter.DateFrom, filter.DateTo, filter.RewardType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	h := &service.CheckinHistory{Items: []service.CheckinRecord{}}
	for rows.Next() {
		record, err := scanCheckinRecord(rows)
		if err != nil {
			return nil, err
		}
		h.Items = append(h.Items, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(h.Items) > filter.Limit {
		h.Items = h.Items[:filter.Limit]
		h.NextCursor = h.Items[filter.Limit-1].ID
	}
	return h, nil
}

func (r *checkinRepository) Budgets(ctx context.Context, from, to string, limit int) (*service.CheckinBudgets, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT business_date::text,limit_usd::text,spent_usd::text,reserved_usd::text,(limit_usd-spent_usd-reserved_usd)::text FROM checkin_budgets
 WHERE ($1::text='' OR business_date>=NULLIF($1,'')::date) AND ($2::text='' OR business_date<=NULLIF($2,'')::date) ORDER BY business_date DESC LIMIT $3`, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := &service.CheckinBudgets{Items: []service.CheckinBudget{}}
	for rows.Next() {
		var b service.CheckinBudget
		if err := rows.Scan(&b.BusinessDate, &b.LimitUSD, &b.SpentUSD, &b.ReservedUSD, &b.RemainingUSD); err != nil {
			return nil, err
		}
		result.Items = append(result.Items, b)
	}
	return result, rows.Err()
}
