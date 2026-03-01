package prm

// TrajectoryPattern describes the detected pattern in a sequence of scores.
type TrajectoryPattern string

const (
	// PatternNone indicates no significant pattern has been detected.
	PatternNone TrajectoryPattern = "none"
	// PatternSustainedDecline indicates 3 or more consecutive score drops.
	PatternSustainedDecline TrajectoryPattern = "sustained_decline"
	// PatternPlateau indicates 5 or more consecutive identical scores.
	PatternPlateau TrajectoryPattern = "plateau"
	// PatternOscillation indicates 4 or more alternating up/down movements.
	PatternOscillation TrajectoryPattern = "oscillation"
	// PatternRecovery indicates 3 or more consecutive score increases.
	PatternRecovery TrajectoryPattern = "recovery"
)

// Trend describes the immediate direction of the score trajectory.
type Trend string

const (
	// TrendStable indicates the score has not changed significantly.
	TrendStable Trend = "stable"
	// TrendDeclining indicates the score is going down.
	TrendDeclining Trend = "declining"
	// TrendImproving indicates the score is going up.
	TrendImproving Trend = "improving"
)

// Trajectory tracks a history of StepScores and detects patterns in the
// score progression over time.
type Trajectory struct {
	scores    []StepScore
	maxLength int
}

// NewTrajectory creates a Trajectory that retains up to maxLength scores.
// Older scores are discarded when the limit is exceeded.
func NewTrajectory(maxLength int) *Trajectory {
	if maxLength <= 0 {
		maxLength = 50
	}
	return &Trajectory{
		maxLength: maxLength,
	}
}

// AddScore appends a new score to the trajectory history.
func (t *Trajectory) AddScore(score StepScore) {
	t.scores = append(t.scores, score)
	if len(t.scores) > t.maxLength {
		t.scores = t.scores[len(t.scores)-t.maxLength:]
	}
}

// Len returns the number of scores currently tracked.
func (t *Trajectory) Len() int {
	return len(t.scores)
}

// Latest returns the most recent StepScore, or nil if the trajectory is empty.
func (t *Trajectory) Latest() *StepScore {
	if len(t.scores) == 0 {
		return nil
	}
	s := t.scores[len(t.scores)-1]
	return &s
}

// Pattern detects the most significant pattern in the recent score history.
// It checks patterns in priority order: sustained decline (most urgent),
// oscillation, plateau, recovery, and finally none.
func (t *Trajectory) Pattern() TrajectoryPattern {
	if len(t.scores) < 3 {
		return PatternNone
	}

	// Check sustained decline: 3+ consecutive drops.
	if t.hasConsecutiveDrops(3) {
		return PatternSustainedDecline
	}

	// Check oscillation: 4+ alternating up/down.
	if t.hasOscillation(4) {
		return PatternOscillation
	}

	// Check plateau: 5+ identical scores.
	if t.hasPlateau(5) {
		return PatternPlateau
	}

	// Check recovery: 3+ consecutive increases.
	if t.hasConsecutiveIncreases(3) {
		return PatternRecovery
	}

	return PatternNone
}

// CurrentTrend returns the immediate trend based on the last two scores.
func (t *Trajectory) CurrentTrend() Trend {
	if len(t.scores) < 2 {
		return TrendStable
	}
	prev := t.scores[len(t.scores)-2].Score
	curr := t.scores[len(t.scores)-1].Score

	if curr < prev {
		return TrendDeclining
	}
	if curr > prev {
		return TrendImproving
	}
	return TrendStable
}

// hasConsecutiveDrops returns true if the last n scores form a strictly
// decreasing sequence.
func (t *Trajectory) hasConsecutiveDrops(n int) bool {
	if len(t.scores) < n+1 {
		return false
	}
	tail := t.scores[len(t.scores)-(n+1):]
	for i := 1; i < len(tail); i++ {
		if tail[i].Score >= tail[i-1].Score {
			return false
		}
	}
	return true
}

// hasConsecutiveIncreases returns true if the last n scores form a strictly
// increasing sequence.
func (t *Trajectory) hasConsecutiveIncreases(n int) bool {
	if len(t.scores) < n+1 {
		return false
	}
	tail := t.scores[len(t.scores)-(n+1):]
	for i := 1; i < len(tail); i++ {
		if tail[i].Score <= tail[i-1].Score {
			return false
		}
	}
	return true
}

// hasPlateau returns true if the last n scores are all identical.
func (t *Trajectory) hasPlateau(n int) bool {
	if len(t.scores) < n {
		return false
	}
	tail := t.scores[len(t.scores)-n:]
	first := tail[0].Score
	for _, s := range tail[1:] {
		if s.Score != first {
			return false
		}
	}
	return true
}

// hasOscillation returns true if the last n score changes alternate between
// positive and negative (up/down/up/down or down/up/down/up).
func (t *Trajectory) hasOscillation(n int) bool {
	// We need n+1 scores to observe n direction changes.
	if len(t.scores) < n+1 {
		return false
	}
	tail := t.scores[len(t.scores)-(n+1):]

	alternations := 0
	var lastDir int // -1 = down, +1 = up, 0 = same
	for i := 1; i < len(tail); i++ {
		diff := tail[i].Score - tail[i-1].Score
		var dir int
		if diff > 0 {
			dir = 1
		} else if diff < 0 {
			dir = -1
		} else {
			// Equal scores break the oscillation pattern.
			return false
		}

		if lastDir != 0 && dir != lastDir {
			alternations++
		}
		lastDir = dir
	}

	// We need at least n-1 direction changes among n transitions.
	return alternations >= n-1
}
