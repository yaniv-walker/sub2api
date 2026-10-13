package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrCheckinDisabled   = infraerrors.Forbidden("CHECKIN_DISABLED", "签到活动暂未开放")
	ErrCheckinIneligible = infraerrors.Forbidden("CHECKIN_INELIGIBLE", "有效累计充值未达到签到门槛")
	ErrCheckinBudget     = infraerrors.Conflict("CHECKIN_BUDGET_EXHAUSTED", "今日活动预算已用完，请明天再来")
	ErrCheckinVersion    = infraerrors.Conflict("CHECKIN_RULE_CONFLICT", "规则已更新，请刷新后重试")
)

// Monetary amounts cross the API and persistence boundary as decimal strings.
// Probability weights and bonus fractions use integer basis points (10000 = 100%).
type CheckinRewardOption struct {
	Amount string `json:"amount"`
	Weight int64  `json:"weight"`
}

type CheckinBonusTier struct {
	Threshold string `json:"threshold"`
	BonusBPS  int64  `json:"bonus_bps"`
}

type CheckinConfig struct {
	Version              int64                 `json:"version"`
	Enabled              bool                  `json:"enabled"`
	Timezone             string                `json:"timezone"`
	EffectiveAt          time.Time             `json:"effective_at"`
	MinimumPaidCreditUSD string                `json:"minimum_paid_credit_usd"`
	DailyBudgetUSD       string                `json:"daily_budget_usd"`
	Rewards              []CheckinRewardOption `json:"rewards"`
	DynamicBonusEnabled  bool                  `json:"dynamic_bonus_enabled"`
	MaximumBonusBPS      int64                 `json:"maximum_bonus_bps"`
	RechargeBonusTiers   []CheckinBonusTier    `json:"recharge_bonus_tiers"`
	BalanceBonusTiers    []CheckinBonusTier    `json:"balance_bonus_tiers"`
	RequestsBonusTiers   []CheckinBonusTier    `json:"requests_bonus_tiers"`
	CycleEnabled         bool                  `json:"cycle_enabled"`
	CycleDays            int                   `json:"cycle_days"`
	RequestsThreshold    int64                 `json:"requests_threshold"`
	CycleRewardUSD       string                `json:"cycle_reward_usd"`
}

type CheckinBonus struct {
	Enabled     bool  `json:"enabled"`
	Locked      bool  `json:"locked"`
	RechargeBPS int64 `json:"recharge_bps"`
	BalanceBPS  int64 `json:"balance_bps"`
	RequestsBPS int64 `json:"requests_bps"`
	TotalBPS    int64 `json:"total_bps"`
}

type CheckinRecord struct {
	ID           int64        `json:"id"`
	UserID       int64        `json:"user_id"`
	BusinessDate string       `json:"business_date"`
	RewardType   string       `json:"reward_type"`
	RewardAmount string       `json:"reward_amount"`
	BalanceAfter string       `json:"balance_after"`
	RuleVersion  int64        `json:"rule_version"`
	ClaimedAt    time.Time    `json:"claimed_at"`
	Bonus        CheckinBonus `json:"dynamic_bonus"`
}

type CheckinCycleDay struct {
	Date      string `json:"date"`
	Requests  int64  `json:"requests"`
	CheckedIn bool   `json:"checked_in"`
	Met       bool   `json:"met"`
	Future    bool   `json:"future"`
	Finalized bool   `json:"finalized"`
}

type CheckinCycle struct {
	ID                int64             `json:"id"`
	State             string            `json:"state"`
	StartDate         string            `json:"start_date"`
	EndDate           string            `json:"end_date"`
	RuleVersion       int64             `json:"rule_version"`
	RewardAmount      string            `json:"reward_amount"`
	RequestsThreshold int64             `json:"requests_threshold"`
	QualifiedDays     int               `json:"qualified_days"`
	SettleAt          time.Time         `json:"settle_at"`
	Days              []CheckinCycleDay `json:"days"`
}

type CheckinInputs struct {
	UserID          int64
	Balance         string
	PaidCreditUSD   string
	TodayRequests   int64
	Active          bool
	Record          *CheckinRecord
	Cycle           *CheckinCycle
	BudgetRemaining string
}

type CheckinDraw struct {
	Amount  string
	Bonus   CheckinBonus
	Weights []int64
}

type CheckinStatus struct {
	ServerTime            time.Time             `json:"server_time"`
	BusinessDate          string                `json:"business_date"`
	Timezone              string                `json:"timezone"`
	NextResetAt           time.Time             `json:"next_reset_at"`
	Enabled               bool                  `json:"enabled"`
	Eligible              bool                  `json:"eligible"`
	EligibilityReason     string                `json:"eligibility_reason,omitempty"`
	MinimumPaidCreditUSD  string                `json:"minimum_paid_credit_usd"`
	EligiblePaidCreditUSD string                `json:"eligible_paid_credit_usd"`
	Balance               string                `json:"balance"`
	CheckedIn             bool                  `json:"checked_in"`
	CanClaim              bool                  `json:"can_claim"`
	State                 string                `json:"state"`
	RuleVersion           int64                 `json:"rule_version"`
	TodayValidRequests    int64                 `json:"today_valid_requests"`
	RewardOptions         []CheckinRewardOption `json:"reward_options"`
	DynamicBonus          CheckinBonus          `json:"dynamic_bonus"`
	Record                *CheckinRecord        `json:"record"`
	Cycle                 *CheckinCycle         `json:"cycle"`
	CycleEnabled          bool                  `json:"cycle_enabled"`
	CycleDays             int                   `json:"cycle_days"`
	RequestsThreshold     int64                 `json:"requests_threshold"`
	CycleRewardUSD        string                `json:"cycle_reward_usd"`
}

type CheckinClaimResult struct {
	Created bool           `json:"created"`
	Record  *CheckinRecord `json:"record"`
	Status  *CheckinStatus `json:"status,omitempty"`
}

type CheckinHistory struct {
	Items      []CheckinRecord `json:"items"`
	NextCursor int64           `json:"next_cursor"`
}

type CheckinAdminCycle struct {
	CheckinCycle
	UserID int64 `json:"user_id"`
}

type CheckinCycleHistory struct {
	Items      []CheckinAdminCycle `json:"items"`
	NextCursor int64               `json:"next_cursor"`
}

type CheckinHistoryFilter struct {
	UserID     int64
	Cursor     int64
	Limit      int
	DateFrom   string
	DateTo     string
	RewardType string
}

type CheckinBudget struct {
	BusinessDate string `json:"business_date"`
	LimitUSD     string `json:"limit_usd"`
	SpentUSD     string `json:"spent_usd"`
	ReservedUSD  string `json:"reserved_usd"`
	RemainingUSD string `json:"remaining_usd"`
}

type CheckinBudgets struct {
	Items []CheckinBudget `json:"items"`
}

type CheckinInvalidation struct {
	ID     int64
	UserID int64
}
type CheckinDrawFunc func(CheckinConfig, CheckinInputs) (CheckinDraw, error)

type CheckinRepository interface {
	GetConfig(context.Context, time.Time, bool) (*CheckinConfig, error)
	SaveConfig(context.Context, CheckinConfig, int64, int64, time.Time) (*CheckinConfig, error)
	GetUserState(context.Context, int64, time.Time, CheckinConfig) (*CheckinInputs, error)
	Claim(context.Context, int64, time.Time, CheckinDrawFunc) (*CheckinClaimResult, error)
	History(context.Context, int64, int64, int) (*CheckinHistory, error)
	PendingInvalidations(context.Context, int) ([]CheckinInvalidation, error)
	AckInvalidation(context.Context, int64) error
	ProcessCycles(context.Context, time.Time) error
	ListCycles(context.Context, int64, int64, int) (*CheckinCycleHistory, error)
	ResolveCycle(context.Context, int64, int64, string, string, time.Time) error
	RewardHistory(context.Context, CheckinHistoryFilter) (*CheckinHistory, error)
	Budgets(context.Context, string, string, int) (*CheckinBudgets, error)
}
