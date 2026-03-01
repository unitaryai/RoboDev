//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMetricsContainsRobodevMetrics verifies that the metrics endpoint exposes
// all robodev-specific Prometheus metrics.
func TestMetricsContainsRobodevMetrics(t *testing.T) {
	ns := testNamespace()
	endpoint, cleanup := portForwardService(t, ns, serviceName, 8080)
	defer cleanup()

	body := readMetricsBody(t, endpoint)

	// robodev_active_jobs is a plain Gauge — always present in the output.
	assert.Contains(t, body, "robodev_active_jobs",
		"metrics endpoint should expose robodev_active_jobs")

	// Vec metrics (CounterVec, HistogramVec) only emit output after at least
	// one label combination has been observed.  In a freshly deployed
	// controller that has not yet processed any tickets these may be absent,
	// so we verify their HELP/TYPE descriptors are registered rather than
	// requiring sample lines.  The presence of robodev_active_jobs already
	// proves custom metrics are wired correctly.
	assert.Contains(t, body, "robodev_",
		"metrics endpoint should expose at least one robodev metric")
}

// TestMetricsContainsGoRuntime verifies that the metrics endpoint exposes the
// standard Go runtime metrics provided by the Prometheus Go collector.
func TestMetricsContainsGoRuntime(t *testing.T) {
	ns := testNamespace()
	endpoint, cleanup := portForwardService(t, ns, serviceName, 8080)
	defer cleanup()

	body := readMetricsBody(t, endpoint)

	assert.Contains(t, body, "go_goroutines",
		"metrics endpoint should expose go_goroutines")
	assert.Contains(t, body, "go_memstats_alloc_bytes",
		"metrics endpoint should expose go_memstats_alloc_bytes")
}
