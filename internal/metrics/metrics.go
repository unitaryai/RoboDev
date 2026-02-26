// Package metrics defines Prometheus metrics for the RoboDev controller.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "robodev"

// Core controller metrics.
var (
	// TaskRunsTotal counts the total number of task runs by final state.
	TaskRunsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "taskruns_total",
			Help:      "Total number of task runs by state.",
		},
		[]string{"state"},
	)

	// TaskRunDurationSeconds tracks the duration of task runs in seconds.
	TaskRunDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "taskrun_duration_seconds",
			Help:      "Duration of task runs in seconds.",
			Buckets:   prometheus.ExponentialBuckets(60, 2, 8), // 1m to ~4h
		},
		[]string{"engine"},
	)

	// ActiveJobs tracks the number of currently active jobs.
	ActiveJobs = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "active_jobs",
			Help:      "Number of currently active jobs.",
		},
	)

	// PluginErrorsTotal counts plugin errors by plugin name.
	PluginErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "plugin_errors_total",
			Help:      "Total number of plugin errors by plugin.",
		},
		[]string{"plugin"},
	)
)
