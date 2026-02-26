// Package costtracker provides thread-safe token and cost tracking per engine
// and per TaskRun, enabling budget enforcement for agent executions.
package costtracker

import (
	"sync"
)

// CostRate defines the per-token cost in USD for an engine.
type CostRate struct {
	InputTokenCostPerMillion  float64
	OutputTokenCostPerMillion float64
}

// DefaultRates provides approximate cost rates for known engines.
var DefaultRates = map[string]CostRate{
	"claude-code": {InputTokenCostPerMillion: 3.0, OutputTokenCostPerMillion: 15.0},
	"codex":       {InputTokenCostPerMillion: 2.0, OutputTokenCostPerMillion: 8.0},
}

// usage tracks accumulated token consumption for a single TaskRun.
type usage struct {
	inputTokens  int64
	outputTokens int64
	engine       string
}

// CostTracker tracks token consumption and costs across TaskRuns.
// It is safe for concurrent use.
type CostTracker struct {
	mu    sync.RWMutex
	rates map[string]CostRate
	usage map[string]*usage // keyed by TaskRun ID
}

// New creates a new CostTracker with the given per-engine cost rates.
// If rates is nil, DefaultRates are used.
func New(rates map[string]CostRate) *CostTracker {
	if rates == nil {
		rates = DefaultRates
	}
	return &CostTracker{
		rates: rates,
		usage: make(map[string]*usage),
	}
}

// TrackUsage records token consumption for a TaskRun. Tokens are
// accumulated across multiple calls for the same TaskRun.
func (ct *CostTracker) TrackUsage(taskRunID, engineName string, inputTokens, outputTokens int64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	u, ok := ct.usage[taskRunID]
	if !ok {
		u = &usage{engine: engineName}
		ct.usage[taskRunID] = u
	}
	u.inputTokens += inputTokens
	u.outputTokens += outputTokens
}

// GetCost returns the total estimated cost in USD for a given TaskRun.
func (ct *CostTracker) GetCost(taskRunID string) float64 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	u, ok := ct.usage[taskRunID]
	if !ok {
		return 0
	}

	return ct.calculateCost(u)
}

// GetTotalCost returns the total estimated cost in USD across all TaskRuns.
func (ct *CostTracker) GetTotalCost() float64 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	var total float64
	for _, u := range ct.usage {
		total += ct.calculateCost(u)
	}
	return total
}

// CheckBudget returns whether the given TaskRun is within the specified budget
// and its current cost. Returns (true, cost) if within budget, (false, cost) otherwise.
func (ct *CostTracker) CheckBudget(taskRunID string, maxCostUSD float64) (bool, float64) {
	cost := ct.GetCost(taskRunID)
	return cost <= maxCostUSD, cost
}

// calculateCost computes the USD cost for a usage record based on configured rates.
func (ct *CostTracker) calculateCost(u *usage) float64 {
	rate, ok := ct.rates[u.engine]
	if !ok {
		return 0
	}

	inputCost := float64(u.inputTokens) / 1_000_000 * rate.InputTokenCostPerMillion
	outputCost := float64(u.outputTokens) / 1_000_000 * rate.OutputTokenCostPerMillion
	return inputCost + outputCost
}
