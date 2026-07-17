package controller

import (
	"strings"

	"github.com/unitaryai/osmia/internal/taskrun"
	"github.com/unitaryai/osmia/internal/usecase"
)

// incidentTaskRunIDPrefix is the TaskRun ID prefix ProcessIncidentEvent
// uses (see incident.go's trID construction). It is the only signal
// available for inferring the use case of a TaskRun persisted before
// TaskRun.UseCase existed.
const incidentTaskRunIDPrefix = "tr-incident-"

// useCaseFor returns the use-case name for tr: the persisted
// tr.UseCase field when set, or a legacy-inference fallback for TaskRuns
// persisted before that field existed. The fallback treats a TaskRun ID
// starting with "tr-incident-" as incident-triage and everything else as
// ticketing, mirroring ProcessIncidentEvent's and ProcessTicket's
// respective ID formats.
//
// This is a one-release shim, per
// docs/designs/use-case-abstraction.md section 4 and section 8: it
// should be removed once no unresolved TaskRun predating TaskRun.UseCase
// remains, at a documented release boundary. Nothing dispatches through
// this function yet.
func useCaseFor(tr *taskrun.TaskRun) string {
	if tr.UseCase != "" {
		return tr.UseCase
	}
	if strings.HasPrefix(tr.ID, incidentTaskRunIDPrefix) {
		return usecase.NameIncidentTriage
	}
	return usecase.NameTicketing
}
