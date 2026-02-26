package costtracker

import (
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackUsage_And_GetCost(t *testing.T) {
	tests := []struct {
		name         string
		engine       string
		inputTokens  int64
		outputTokens int64
		wantCost     float64
	}{
		{
			name:         "claude-code cost calculation",
			engine:       "claude-code",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			// $3 input + $15 output = $18
			wantCost: 18.0,
		},
		{
			name:         "codex cost calculation",
			engine:       "codex",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			// $2 input + $8 output = $10
			wantCost: 10.0,
		},
		{
			name:         "partial million tokens",
			engine:       "claude-code",
			inputTokens:  500_000,
			outputTokens: 250_000,
			// $1.50 input + $3.75 output = $5.25
			wantCost: 5.25,
		},
		{
			name:         "unknown engine returns zero cost",
			engine:       "unknown-engine",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			wantCost:     0,
		},
		{
			name:         "zero tokens",
			engine:       "claude-code",
			inputTokens:  0,
			outputTokens: 0,
			wantCost:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := New(nil) // uses DefaultRates
			ct.TrackUsage("tr-1", tt.engine, tt.inputTokens, tt.outputTokens)

			cost := ct.GetCost("tr-1")
			assert.InDelta(t, tt.wantCost, cost, 0.001)
		})
	}
}

func TestTrackUsage_Accumulates(t *testing.T) {
	ct := New(nil)

	ct.TrackUsage("tr-1", "claude-code", 500_000, 100_000)
	ct.TrackUsage("tr-1", "claude-code", 500_000, 100_000)

	cost := ct.GetCost("tr-1")
	// 1M input ($3) + 200K output ($3)  = $6
	assert.InDelta(t, 6.0, cost, 0.001)
}

func TestGetCost_UnknownTaskRun(t *testing.T) {
	ct := New(nil)
	cost := ct.GetCost("nonexistent")
	assert.Equal(t, 0.0, cost)
}

func TestGetTotalCost(t *testing.T) {
	ct := New(nil)

	ct.TrackUsage("tr-1", "claude-code", 1_000_000, 0)
	ct.TrackUsage("tr-2", "codex", 1_000_000, 0)

	total := ct.GetTotalCost()
	// tr-1: $3 input, tr-2: $2 input = $5
	assert.InDelta(t, 5.0, total, 0.001)
}

func TestCheckBudget(t *testing.T) {
	tests := []struct {
		name       string
		maxCost    float64
		wantWithin bool
	}{
		{
			name:       "within budget",
			maxCost:    20.0,
			wantWithin: true,
		},
		{
			name:       "over budget",
			maxCost:    1.0,
			wantWithin: false,
		},
		{
			name:       "exactly at budget",
			maxCost:    3.0,
			wantWithin: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := New(nil)
			ct.TrackUsage("tr-budget", "claude-code", 1_000_000, 0) // $3

			within, cost := ct.CheckBudget("tr-budget", tt.maxCost)
			assert.Equal(t, tt.wantWithin, within)
			assert.InDelta(t, 3.0, cost, 0.001)
		})
	}
}

func TestCheckBudget_UnknownTaskRun(t *testing.T) {
	ct := New(nil)
	within, cost := ct.CheckBudget("nonexistent", 10.0)
	assert.True(t, within)
	assert.Equal(t, 0.0, cost)
}

func TestMultipleTaskRuns(t *testing.T) {
	ct := New(nil)

	ct.TrackUsage("tr-1", "claude-code", 1_000_000, 500_000)
	ct.TrackUsage("tr-2", "codex", 2_000_000, 1_000_000)

	cost1 := ct.GetCost("tr-1")
	cost2 := ct.GetCost("tr-2")

	// tr-1: $3 input + $7.50 output = $10.50
	assert.InDelta(t, 10.50, cost1, 0.001)
	// tr-2: $4 input + $8 output = $12
	assert.InDelta(t, 12.0, cost2, 0.001)

	total := ct.GetTotalCost()
	assert.InDelta(t, 22.50, total, 0.001)
}

func TestCustomRates(t *testing.T) {
	rates := map[string]CostRate{
		"custom-engine": {
			InputTokenCostPerMillion:  1.0,
			OutputTokenCostPerMillion: 5.0,
		},
	}

	ct := New(rates)
	ct.TrackUsage("tr-custom", "custom-engine", 1_000_000, 1_000_000)

	cost := ct.GetCost("tr-custom")
	assert.InDelta(t, 6.0, cost, 0.001)
}

func TestConcurrentAccess(t *testing.T) {
	ct := New(nil)

	var wg sync.WaitGroup
	iterations := 1000

	// Multiple goroutines tracking usage concurrently.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ct.TrackUsage("tr-concurrent", "claude-code", 1000, 500)
			}
		}()
	}

	// Concurrent reads while writes are happening.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ct.GetCost("tr-concurrent")
				ct.GetTotalCost()
				ct.CheckBudget("tr-concurrent", 100.0)
			}
		}()
	}

	wg.Wait()

	// Verify final state is consistent.
	cost := ct.GetCost("tr-concurrent")
	require.False(t, math.IsNaN(cost))
	assert.Greater(t, cost, 0.0)

	// 10 goroutines * 1000 iterations * 1000 input = 10M input tokens
	// 10 goroutines * 1000 iterations * 500 output = 5M output tokens
	// $30 input + $75 output = $105
	expectedCost := 10.0*3.0 + 5.0*15.0
	assert.InDelta(t, expectedCost, cost, 0.001)
}
