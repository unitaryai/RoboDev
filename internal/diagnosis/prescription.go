package diagnosis

import (
	"bytes"
	"fmt"
	"text/template"
)

// prescriptionTemplates maps each failure mode to a safe, template-based
// corrective instruction. Templates use text/template to prevent prompt
// injection — no raw agent output is interpolated directly.
var prescriptionTemplates = map[FailureMode]string{
	WrongApproach: `Focus specifically on the assigned task. Do not modify files outside the expected scope. ` +
		`The previous attempt edited incorrect files. Review the task description carefully before starting.`,

	DependencyMissing: `Before starting the main task, ensure all required dependencies are available. ` +
		`The previous attempt failed due to a missing dependency. ` +
		`Check that all imports resolve correctly and install any missing packages first.`,

	TestMisunderstanding: `Do not modify test files unless the task explicitly requires it. ` +
		`The existing tests define the expected behaviour. ` +
		`Focus on making the implementation pass the existing tests.`,

	ScopeCreep: `Limit changes to the minimum necessary files. ` +
		`The previous attempt modified too many files ({{ .FilesChanged }} changed). ` +
		`The task only requires targeted changes — avoid refactoring unrelated code.`,

	PermissionBlocked: `The previous attempt was blocked by file or network permissions. ` +
		`Try an alternative approach that does not require elevated access. ` +
		`If the task requires specific permissions, note this in the output.`,

	ModelConfusion: `Take a step-by-step approach to avoid repeating previous mistakes. ` +
		`The previous attempt showed signs of confusion with oscillating tool calls. ` +
		`Plan your changes before executing them: first understand, then implement, then verify.`,

	InfraFailure: `The previous attempt failed due to infrastructure issues (e.g. resource limits, network). ` +
		`Ensure the approach is resource-efficient. ` +
		`If the failure persists, the task may need human intervention.`,

	Unknown: `The previous attempt failed for unclear reasons. ` +
		`Take a careful, methodical approach. ` +
		`Review the task requirements and verify each step before proceeding.`,
}

// templateData holds values available to prescription templates.
type templateData struct {
	FilesChanged int
	Evidence     []string
}

// Prescriber generates corrective instructions from a Diagnosis using
// safe, template-based rendering. No raw agent output is interpolated
// to prevent prompt injection.
type Prescriber struct{}

// NewPrescriber creates a new Prescriber.
func NewPrescriber() *Prescriber {
	return &Prescriber{}
}

// Prescribe generates a corrective instruction string from the given
// diagnosis. The output is safe for inclusion in a retry prompt.
func (p *Prescriber) Prescribe(diag *Diagnosis, filesChanged int) (string, error) {
	if diag == nil {
		return "", fmt.Errorf("nil diagnosis")
	}

	tmplStr, ok := prescriptionTemplates[diag.Mode]
	if !ok {
		tmplStr = prescriptionTemplates[Unknown]
	}

	tmpl, err := template.New("prescription").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing prescription template: %w", err)
	}

	data := templateData{
		FilesChanged: filesChanged,
		Evidence:     diag.Evidence,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing prescription template: %w", err)
	}

	return buf.String(), nil
}
