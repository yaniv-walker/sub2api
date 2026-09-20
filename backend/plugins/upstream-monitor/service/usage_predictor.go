package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/repository"
)

// UsagePredictor predicts future usage and balance lifetime.
type UsagePredictor struct {
	config       *PredictionConfig
	snapshotRepo *repository.BalanceSnapshotRepository
}

// PredictionConfig contains configuration for usage prediction.
type PredictionConfig struct {
	Algorithm     string // "moving_average" or "linear_regression"
	WindowDays    int    // Number of days to look back
	MinDataPoints int    // Minimum data points required for prediction
}

// NewUsagePredictor creates a new usage predictor.
func NewUsagePredictor(config *PredictionConfig, snapshotRepo *repository.BalanceSnapshotRepository) *UsagePredictor {
	return &UsagePredictor{
		config:       config,
		snapshotRepo: snapshotRepo,
	}
}

// PredictionResult contains prediction results.
type PredictionResult struct {
	AccountID           int64     `json:"account_id"`
	CurrentBalance      float64   `json:"current_balance"`
	DailyAverageCost    float64   `json:"daily_average_cost"`
	PredictedDaysLeft   float64   `json:"predicted_days_left"`
	PredictedDepletedAt time.Time `json:"predicted_depleted_at"`
	Confidence          float64   `json:"confidence"` // 0-1
	Algorithm           string    `json:"algorithm"`
	DataPointsUsed      int       `json:"data_points_used"`
}

// BalanceDataPoint represents a balance snapshot point
type BalanceDataPoint struct {
	Date    time.Time
	Balance float64
}

// PredictFromHistory predicts usage based on historical balance snapshots from the database
func (p *UsagePredictor) PredictFromHistory(ctx context.Context, upstreamType string, accountID int64) (*PredictionResult, error) {
	// Get historical snapshots
	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -p.config.WindowDays)

	snapshots, err := p.snapshotRepo.GetByAccountTimeRange(ctx, upstreamType, accountID, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance history: %w", err)
	}

	if len(snapshots) < p.config.MinDataPoints {
		return nil, fmt.Errorf("insufficient data points: need at least %d, got %d", p.config.MinDataPoints, len(snapshots))
	}

	// Convert to data points
	dataPoints := make([]BalanceDataPoint, len(snapshots))
	for i, snapshot := range snapshots {
		dataPoints[i] = BalanceDataPoint{
			Date:    snapshot.SnapshotAt,
			Balance: snapshot.Balance,
		}
	}

	// Get current balance (latest snapshot)
	currentBalance := dataPoints[len(dataPoints)-1].Balance

	result := &PredictionResult{
		AccountID:      accountID,
		CurrentBalance: currentBalance,
		Algorithm:      p.config.Algorithm,
		DataPointsUsed: len(dataPoints),
	}

	switch p.config.Algorithm {
	case "moving_average":
		return p.movingAveragePredict(result, dataPoints)
	case "linear_regression":
		return p.linearRegressionPredict(result, dataPoints)
	default:
		return p.movingAveragePredict(result, dataPoints)
	}
}

// Predict predicts future usage based on provided historical data (legacy method for backward compatibility)
func (p *UsagePredictor) Predict(accountID int64, currentBalance float64, historicalData []BalanceDataPoint) (*PredictionResult, error) {
	if len(historicalData) < p.config.MinDataPoints {
		return nil, fmt.Errorf("insufficient data points: need at least %d, got %d", p.config.MinDataPoints, len(historicalData))
	}

	result := &PredictionResult{
		AccountID:      accountID,
		CurrentBalance: currentBalance,
		Algorithm:      p.config.Algorithm,
		DataPointsUsed: len(historicalData),
	}

	switch p.config.Algorithm {
	case "moving_average":
		return p.movingAveragePredict(result, historicalData)
	case "linear_regression":
		return p.linearRegressionPredict(result, historicalData)
	default:
		return p.movingAveragePredict(result, historicalData)
	}
}

// movingAveragePredict uses moving average for prediction based on balance decline.
func (p *UsagePredictor) movingAveragePredict(result *PredictionResult, data []BalanceDataPoint) (*PredictionResult, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("need at least 2 data points to calculate daily cost")
	}

	// Calculate daily costs based on balance decline between consecutive snapshots
	var dailyCosts []float64
	for i := 1; i < len(data); i++ {
		prevBalance := data[i-1].Balance
		currBalance := data[i].Balance
		timeDiff := data[i].Date.Sub(data[i-1].Date).Hours() / 24 // days

		if timeDiff > 0 {
			balanceDecline := prevBalance - currBalance
			dailyCost := balanceDecline / timeDiff
			if dailyCost > 0 { // Only count positive costs (ignore balance increases)
				dailyCosts = append(dailyCosts, dailyCost)
			}
		}
	}

	if len(dailyCosts) == 0 {
		// No cost data available, balance is stable or increasing
		result.DailyAverageCost = 0
		result.PredictedDaysLeft = -1 // Infinite
		result.PredictedDepletedAt = time.Time{}
		result.Confidence = 0.5
		return result, nil
	}

	// Calculate average daily cost
	totalCost := 0.0
	for _, cost := range dailyCosts {
		totalCost += cost
	}
	result.DailyAverageCost = totalCost / float64(len(dailyCosts))

	// Calculate predicted days left
	if result.DailyAverageCost > 0 {
		result.PredictedDaysLeft = result.CurrentBalance / result.DailyAverageCost
		result.PredictedDepletedAt = time.Now().Add(time.Duration(result.PredictedDaysLeft*24) * time.Hour)
	} else {
		result.PredictedDaysLeft = -1 // Infinite
		result.PredictedDepletedAt = time.Time{}
	}

	// Calculate confidence based on cost variance
	result.Confidence = p.calculateCostConfidence(dailyCosts)

	return result, nil
}

// calculateCostConfidence calculates prediction confidence based on daily cost variance.
func (p *UsagePredictor) calculateCostConfidence(dailyCosts []float64) float64 {
	if len(dailyCosts) < 2 {
		return 0.5
	}

	// Calculate mean
	sum := 0.0
	for _, cost := range dailyCosts {
		sum += cost
	}
	mean := sum / float64(len(dailyCosts))

	if mean == 0 {
		return 0.5
	}

	// Calculate variance
	variance := 0.0
	for _, cost := range dailyCosts {
		diff := cost - mean
		variance += diff * diff
	}
	variance /= float64(len(dailyCosts))

	// Calculate coefficient of variation
	cv := (variance / (mean * mean))

	// Convert to confidence (lower CV = higher confidence)
	// CV of 0 = confidence 1.0, CV of 1 = confidence 0.5, CV of 2+ = confidence 0.0
	confidence := 1.0 - (cv / 2.0)
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}

	return confidence
}

// linearRegressionPredict uses linear regression for prediction based on balance trend.
func (p *UsagePredictor) linearRegressionPredict(result *PredictionResult, data []BalanceDataPoint) (*PredictionResult, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("need at least 2 data points for linear regression")
	}

	// Convert timestamps to days since first snapshot
	baseTime := data[0].Date
	var xData, yData []float64
	for _, point := range data {
		daysSince := point.Date.Sub(baseTime).Hours() / 24
		xData = append(xData, daysSince)
		yData = append(yData, point.Balance)
	}

	// Calculate linear regression: y = mx + b
	n := float64(len(xData))
	sumX, sumY, sumXY, sumX2 := 0.0, 0.0, 0.0, 0.0
	for i := range xData {
		sumX += xData[i]
		sumY += yData[i]
		sumXY += xData[i] * yData[i]
		sumX2 += xData[i] * xData[i]
	}

	// Calculate slope (m) and intercept (b)
	m := (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)
	b := (sumY - m*sumX) / n

	// Daily cost is the negative of the slope (balance decline per day)
	result.DailyAverageCost = -m

	// Calculate predicted days left
	if m < 0 { // Balance is declining
		// Find when balance reaches zero: 0 = m*x + b, solve for x
		daysUntilZero := -b / m
		currentDays := time.Now().Sub(baseTime).Hours() / 24
		daysLeft := daysUntilZero - currentDays

		if daysLeft > 0 {
			result.PredictedDaysLeft = daysLeft
			result.PredictedDepletedAt = time.Now().Add(time.Duration(daysLeft*24) * time.Hour)
		} else {
			// Already past predicted depletion
			result.PredictedDaysLeft = 0
			result.PredictedDepletedAt = time.Now()
		}
	} else {
		// Balance is stable or increasing
		result.DailyAverageCost = 0
		result.PredictedDaysLeft = -1
		result.PredictedDepletedAt = time.Time{}
	}

	// Calculate R² for confidence
	result.Confidence = p.calculateR2(xData, yData, m, b)

	return result, nil
}

// calculateR2 calculates the coefficient of determination (R²) for regression quality
func (p *UsagePredictor) calculateR2(xData, yData []float64, m, b float64) float64 {
	// Calculate mean of y
	sumY := 0.0
	for _, y := range yData {
		sumY += y
	}
	meanY := sumY / float64(len(yData))

	// Calculate total sum of squares (SS_tot) and residual sum of squares (SS_res)
	ssTot := 0.0
	ssRes := 0.0
	for i := range xData {
		predicted := m*xData[i] + b
		ssTot += (yData[i] - meanY) * (yData[i] - meanY)
		ssRes += (yData[i] - predicted) * (yData[i] - predicted)
	}

	// R² = 1 - (SS_res / SS_tot)
	if ssTot == 0 {
		return 0.5
	}

	r2 := 1 - (ssRes / ssTot)
	if r2 < 0 {
		r2 = 0
	}
	if r2 > 1 {
		r2 = 1
	}

	return r2
}
