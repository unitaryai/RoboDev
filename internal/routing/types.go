// Package routing provides intelligent engine selection based on historical
// performance data. It tracks per-engine success rates across multiple
// dimensions (task type, language, repository size, complexity) and uses
// Laplace-smoothed scores to route tasks to the most suitable engine.
package routing

import "time"

// TaskOutcome records the result of a completed task execution, used to
// update engine fingerprints.
type TaskOutcome struct {
	EngineName   string        `json:"engine_name"`
	TaskType     string        `json:"task_type"`
	RepoLanguage string        `json:"repo_language"`
	RepoSize     int           `json:"repo_size"`
	Complexity   string        `json:"complexity"`
	Success      bool          `json:"success"`
	Duration     time.Duration `json:"duration"`
	Cost         float64       `json:"cost"`
}

// RoutingQuery describes the characteristics of a task for which
// an engine must be selected.
type RoutingQuery struct {
	TaskType     string `json:"task_type"`
	RepoLanguage string `json:"repo_language"`
	RepoSize     int    `json:"repo_size"`
	Complexity   string `json:"complexity"`
}

// repoSizeBucket converts a file count into a categorical bucket label.
func repoSizeBucket(fileCount int) string {
	switch {
	case fileCount <= 100:
		return "small"
	case fileCount <= 1000:
		return "medium"
	default:
		return "large"
	}
}
