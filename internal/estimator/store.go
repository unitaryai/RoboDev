package estimator

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// PredictionOutcome records the actual cost and duration of a completed task,
// used for learning and improving future predictions.
type PredictionOutcome struct {
	ComplexityScore ComplexityScore `json:"complexity_score"`
	Engine          string          `json:"engine"`
	ActualCost      float64         `json:"actual_cost"`
	ActualDuration  time.Duration   `json:"actual_duration"`
	Success         bool            `json:"success"`
	TaskRunID       string          `json:"task_run_id"`
	RecordedAt      time.Time       `json:"recorded_at"`
}

// EstimatorStore persists and queries historical task outcomes for the
// estimator's kNN-based predictions.
type EstimatorStore interface {
	// SaveOutcome persists a completed task outcome.
	SaveOutcome(ctx context.Context, outcome PredictionOutcome) error

	// QuerySimilar returns the k most similar historical outcomes to the
	// given complexity score, optionally filtered by engine.
	QuerySimilar(ctx context.Context, score ComplexityScore, engine string, k int) ([]PredictionOutcome, error)
}

// MemoryEstimatorStore is a thread-safe in-memory implementation of
// EstimatorStore, suitable for development and testing.
type MemoryEstimatorStore struct {
	mu       sync.RWMutex
	outcomes []PredictionOutcome
}

// NewMemoryEstimatorStore creates an empty in-memory estimator store.
func NewMemoryEstimatorStore() *MemoryEstimatorStore {
	return &MemoryEstimatorStore{
		outcomes: make([]PredictionOutcome, 0),
	}
}

// SaveOutcome appends an outcome to the in-memory store.
func (m *MemoryEstimatorStore) SaveOutcome(_ context.Context, outcome PredictionOutcome) error {
	if outcome.TaskRunID == "" {
		return fmt.Errorf("outcome must have a task run ID")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcomes = append(m.outcomes, outcome)
	return nil
}

// QuerySimilar returns the k most similar historical outcomes based on
// Euclidean distance between complexity dimension vectors. If engine is
// non-empty only outcomes from that engine are considered.
func (m *MemoryEstimatorStore) QuerySimilar(_ context.Context, score ComplexityScore, engine string, k int) ([]PredictionOutcome, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	type scored struct {
		outcome  PredictionOutcome
		distance float64
	}

	var candidates []scored
	for _, o := range m.outcomes {
		if engine != "" && o.Engine != engine {
			continue
		}
		d := euclideanDistance(score, o.ComplexityScore)
		candidates = append(candidates, scored{outcome: o, distance: d})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].distance < candidates[j].distance
	})

	if k > len(candidates) {
		k = len(candidates)
	}

	result := make([]PredictionOutcome, k)
	for i := 0; i < k; i++ {
		result[i] = candidates[i].outcome
	}
	return result, nil
}

// euclideanDistance computes the distance between two complexity score
// vectors across their shared dimensions.
func euclideanDistance(a, b ComplexityScore) float64 {
	sumSq := 0.0

	// Iterate over all dimensions present in either score.
	allDims := make(map[string]bool)
	for d := range a.Dimensions {
		allDims[d] = true
	}
	for d := range b.Dimensions {
		allDims[d] = true
	}

	for d := range allDims {
		va := a.Dimensions[d]
		vb := b.Dimensions[d]
		diff := va - vb
		sumSq += diff * diff
	}

	return math.Sqrt(sumSq)
}
