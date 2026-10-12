package service

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	hostservice "github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/model"
	"github.com/google/uuid"
)

type BalanceAlertStore interface {
	DueIDs(context.Context) ([]int64, error)
	Claim(context.Context, int64, string) (*model.BalanceAlertRule, error)
	StoreState(context.Context, int64, string, model.BalanceAlertState, bool) error
}
type BalanceAlertReader interface {
	PrepareAlertBalances(context.Context) (BalanceAlertReadFunc, error)
}
type BalanceAlertReadFunc func(context.Context, int64) (BalanceAlertSample, error)
type BalanceAlertSender interface {
	Send(context.Context, hostservice.NotificationEmailSendInput) error
}
type BalanceAlertSample struct {
	Name, URL, Type string
	Balance         float64
}

type BalanceAlertChecker struct {
	store    BalanceAlertStore
	reader   BalanceAlertReader
	sender   BalanceAlertSender
	settings *hostservice.SettingService
	clock    func() time.Time
}

func NewBalanceAlertChecker(store BalanceAlertStore, reader BalanceAlertReader, sender BalanceAlertSender, settings *hostservice.SettingService) *BalanceAlertChecker {
	return &BalanceAlertChecker{store: store, reader: reader, sender: sender, settings: settings, clock: time.Now}
}

// Poll due rules frequently, but each upstream is sampled at most every five minutes.
func (c *BalanceAlertChecker) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		c.checkDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *BalanceAlertChecker) allowed(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	settings, err := c.settings.GetUpstreamMonitorSettings(ctx)
	return err == nil && settings.Enabled
}

func (c *BalanceAlertChecker) checkDue(ctx context.Context) {
	if !c.allowed(ctx) {
		return
	}
	ids, err := c.store.DueIDs(ctx)
	if err != nil {
		slog.Warn("upstream balance alert rules unavailable")
		return
	}
	if len(ids) == 0 {
		return
	}
	read, err := c.reader.PrepareAlertBalances(ctx)
	if err != nil {
		slog.Warn("upstream balance alert targets unavailable")
		return
	}
	jobs := make(chan int64)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				if ctx.Err() == nil {
					c.checkOne(ctx, id, read)
				}
			}
		}()
	}
	for _, id := range ids {
		select {
		case jobs <- id:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		}
	}
	close(jobs)
	wg.Wait()
}

func (c *BalanceAlertChecker) checkOne(parent context.Context, id int64, read BalanceAlertReadFunc) {
	ctx, cancel := context.WithTimeout(parent, 6*time.Minute)
	defer cancel()
	if !c.allowed(ctx) {
		return
	}
	owner := uuid.NewString()
	rule, err := c.store.Claim(ctx, id, owner)
	if err != nil || rule == nil {
		return
	}
	checkpoint := func() error { return c.store.StoreState(ctx, id, owner, rule.State, false) }
	sample, err := read(ctx, id)
	now := time.Now().UTC()
	if err != nil || math.IsNaN(sample.Balance) || math.IsInf(sample.Balance, 0) {
		rule.State.LastCheckedAt = &now
		rule.State.LastError = "balance_unavailable"
	} else if c.allowed(ctx) {
		if err = c.evaluate(ctx, rule, sample, now, checkpoint); err != nil {
			slog.Warn("upstream balance alert checkpoint failed", "upstream_id", id)
		}
	}
	// Parent may have been cancelled while SMTP was completing. Persist its result.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer finishCancel()
	if err := c.store.StoreState(finishCtx, id, owner, rule.State, true); err != nil {
		slog.Warn("upstream balance alert state unavailable", "upstream_id", id)
	}
}
