package controller

// ProcessIncidentEvent is a reconciler entry point for incident.io webhook
// events that runs in parallel to ProcessTicket rather than sharing code
// with it. The full dispatch path (engine selection, TaskRun creation,
// K8s Job launch) is intentionally absent from this entry point: it
// arrives in a follow-up change once the operator has authored a
// classifier skill and configured the engine profile to invoke it.
// Until then, the webhook still receives, validates, and acknowledges
// every incident.io delivery — running an agent without a classifier
// skill would produce unstructured output and noise on the dispatch
// surface (e.g. Slack), so the entry point is a structured-log no-op.
//
// A future change will lift both ProcessTicket and ProcessIncidentEvent
// behind a common use-case interface.

import (
	"context"

	"github.com/unitaryai/osmia/internal/webhook"
)

// ProcessIncidentEvent acknowledges an incident.io webhook event. The
// webhook server (`internal/webhook`) handles signature verification
// and parsing; this method is the dispatch hand-off, currently a
// structured-log no-op until the engine-side classifier wiring is in
// place. Returning a non-nil error causes the webhook server to
// respond with 5xx, which would prompt incident.io to retry — so this
// path always returns nil.
func (r *Reconciler) ProcessIncidentEvent(ctx context.Context, evt webhook.IncidentEvent) error {
	r.logger.InfoContext(ctx, "incident webhook received; dispatch disabled",
		"incident_id", evt.Incident.ID,
		"event_type", evt.EventType,
	)
	return nil
}
