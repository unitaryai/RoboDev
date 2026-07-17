package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allCollectors lists every package-level metric collector exported by this
// package. Kept in one place so registration and cardinality tests iterate
// over the same source of truth as new metrics are added.
var allCollectors = []prometheus.Collector{
	TaskRunsTotal,
	TaskRunDurationSeconds,
	ActiveJobs,
	PluginErrorsTotal,
	PRMStepScores,
	PRMInterventionsTotal,
	PRMTrajectoryPatternsTotal,
	RoutingEngineSelectedTotal,
	RoutingExplorationTotal,
	RoutingFingerprintSamples,
	RoutingSuccessRate,
	TournamentTotal,
	TournamentCandidatesTotal,
	TournamentWinnerEngine,
	TournamentCostTotal,
	TournamentDurationSeconds,
	EstimatorPredictionsTotal,
	EstimatorPredictedCost,
	EstimatorAutoRejectionsTotal,
	EstimatorPredictionAccuracy,
	WatchdogCalibratedThreshold,
	WatchdogCalibrationSamples,
	WatchdogCalibrationOverridesTotal,
	DiagnosisTotal,
	DiagnosisEngineSwitchesTotal,
	DiagnosisRetrySuccessTotal,
	MemoryNodesTotal,
	MemoryQueriesTotal,
	MemoryExtractionsTotal,
	MemoryConfidenceDistribution,
}

// TestMetrics_RegisterOnFreshRegistry verifies that every package-level
// collector can be registered against a brand new prometheus.Registry, even
// though each collector is already registered against the default registerer
// via promauto at package init time. Prometheus collectors are stateless with
// respect to registration, so registering the same instance into a second,
// independent registry must not error.
func TestMetrics_RegisterOnFreshRegistry(t *testing.T) {
	for i, c := range allCollectors {
		reg := prometheus.NewRegistry()
		assert.NoErrorf(t, reg.Register(c), "collector %d failed to register on fresh registry", i)
	}
}

// TestMetrics_LabelCardinality registers a representative subset of
// collectors spanning 0, 1, 3, and 4 label dimensions against a fresh
// registry, touches each label combination with distinctive dummy values,
// and asserts the resulting label name/value pairs via Gather(). This
// verifies label names and cardinality without relying on brittle
// Desc.String() parsing.
func TestMetrics_LabelCardinality(t *testing.T) {
	tests := []struct {
		name       string
		metricName string
		touch      func(reg *prometheus.Registry)
		wantLabels map[string]string
	}{
		{
			name:       "0 labels: ActiveJobs",
			metricName: "osmia_active_jobs",
			touch: func(reg *prometheus.Registry) {
				require.NoError(t, reg.Register(ActiveJobs))
				ActiveJobs.Set(1)
			},
			wantLabels: map[string]string{},
		},
		{
			name:       "0 labels: RoutingExplorationTotal",
			metricName: "osmia_routing_exploration_total",
			touch: func(reg *prometheus.Registry) {
				require.NoError(t, reg.Register(RoutingExplorationTotal))
				RoutingExplorationTotal.Inc()
			},
			wantLabels: map[string]string{},
		},
		{
			name:       "1 label: TaskRunsTotal",
			metricName: "osmia_taskruns_total",
			touch: func(reg *prometheus.Registry) {
				require.NoError(t, reg.Register(TaskRunsTotal))
				TaskRunsTotal.WithLabelValues("succeeded").Inc()
			},
			wantLabels: map[string]string{"state": "succeeded"},
		},
		{
			name:       "1 label: TaskRunDurationSeconds",
			metricName: "osmia_taskrun_duration_seconds",
			touch: func(reg *prometheus.Registry) {
				require.NoError(t, reg.Register(TaskRunDurationSeconds))
				TaskRunDurationSeconds.WithLabelValues("claude-code").Observe(120)
			},
			wantLabels: map[string]string{"engine": "claude-code"},
		},
		{
			name:       "1 label: PluginErrorsTotal",
			metricName: "osmia_plugin_errors_total",
			touch: func(reg *prometheus.Registry) {
				require.NoError(t, reg.Register(PluginErrorsTotal))
				PluginErrorsTotal.WithLabelValues("scm").Inc()
			},
			wantLabels: map[string]string{"plugin": "scm"},
		},
		{
			name:       "1 label: MemoryNodesTotal",
			metricName: "osmia_memory_nodes_total",
			touch: func(reg *prometheus.Registry) {
				require.NoError(t, reg.Register(MemoryNodesTotal))
				MemoryNodesTotal.WithLabelValues("fact").Set(3)
			},
			wantLabels: map[string]string{"type": "fact"},
		},
		{
			name:       "1 label: MemoryExtractionsTotal",
			metricName: "osmia_memory_extractions_total",
			touch: func(reg *prometheus.Registry) {
				require.NoError(t, reg.Register(MemoryExtractionsTotal))
				MemoryExtractionsTotal.WithLabelValues("success").Inc()
			},
			wantLabels: map[string]string{"outcome": "success"},
		},
		{
			name:       "3 labels: WatchdogCalibrationSamples",
			metricName: "osmia_watchdog_calibration_samples",
			touch: func(reg *prometheus.Registry) {
				require.NoError(t, reg.Register(WatchdogCalibrationSamples))
				WatchdogCalibrationSamples.WithLabelValues("go-monorepo", "claude-code", "bugfix").Set(7)
			},
			wantLabels: map[string]string{"repo_pattern": "go-monorepo", "engine": "claude-code", "task_type": "bugfix"},
		},
		{
			name:       "3 labels: RoutingSuccessRate",
			metricName: "osmia_routing_success_rate",
			touch: func(reg *prometheus.Registry) {
				require.NoError(t, reg.Register(RoutingSuccessRate))
				RoutingSuccessRate.WithLabelValues("codex", "language", "python").Set(0.9)
			},
			wantLabels: map[string]string{"engine": "codex", "dimension": "language", "value": "python"},
		},
		{
			name:       "4 labels: WatchdogCalibratedThreshold",
			metricName: "osmia_watchdog_calibrated_threshold",
			touch: func(reg *prometheus.Registry) {
				require.NoError(t, reg.Register(WatchdogCalibratedThreshold))
				WatchdogCalibratedThreshold.WithLabelValues("cpu", "go-monorepo", "claude-code", "bugfix").Set(0.5)
			},
			wantLabels: map[string]string{"signal": "cpu", "repo_pattern": "go-monorepo", "engine": "claude-code", "task_type": "bugfix"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			tt.touch(reg)

			families, err := reg.Gather()
			require.NoError(t, err)
			require.Len(t, families, 1, "expected exactly one metric family for %s", tt.metricName)
			require.Equal(t, tt.metricName, families[0].GetName())
			require.Len(t, families[0].GetMetric(), 1, "expected exactly one label combination for %s", tt.metricName)

			gotLabels := map[string]string{}
			for _, lp := range families[0].GetMetric()[0].GetLabel() {
				gotLabels[lp.GetName()] = lp.GetValue()
			}
			assert.Equal(t, tt.wantLabels, gotLabels)
		})
	}
}

// TestMetrics_ActiveJobsIncDecSymmetry verifies Inc/Dec symmetry on a plain
// Gauge. Because ActiveJobs is a shared package-level singleton (also touched
// by the default registerer and potentially other tests), the assertion is
// delta-based rather than checking an absolute value.
func TestMetrics_ActiveJobsIncDecSymmetry(t *testing.T) {
	before := testutil.ToFloat64(ActiveJobs)

	ActiveJobs.Inc()
	ActiveJobs.Inc()
	ActiveJobs.Inc()
	ActiveJobs.Dec()
	ActiveJobs.Dec()

	after := testutil.ToFloat64(ActiveJobs)
	assert.Equal(t, float64(1), after-before)
}

// TestMetrics_GaugeVecIncDecSymmetry verifies Inc/Dec symmetry on a GaugeVec
// label combination, mirroring TestMetrics_ActiveJobsIncDecSymmetry but for a
// vector metric.
func TestMetrics_GaugeVecIncDecSymmetry(t *testing.T) {
	gauge := RoutingFingerprintSamples.WithLabelValues("engine-under-test")

	before := testutil.ToFloat64(gauge)

	gauge.Inc()
	gauge.Inc()
	gauge.Dec()

	after := testutil.ToFloat64(gauge)
	assert.Equal(t, float64(1), after-before)
}

// TestMetrics_CollectorsGatherExpectedDTO is a light sanity check that
// Gather() on a fresh registry returns the expected metric family for a
// simple counter.
func TestMetrics_CollectorsGatherExpectedDTO(t *testing.T) {
	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(TournamentTotal))
	TournamentTotal.Inc()

	families, err := reg.Gather()
	require.NoError(t, err)
	require.Len(t, families, 1)

	mf := families[0]
	assert.Equal(t, "osmia_tournament_total", mf.GetName())
}
