package controller

import (
	"context"
	"fmt"
	"sort"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/unitaryai/osmia/internal/jobbuilder"
	"github.com/unitaryai/osmia/internal/secretresolver"
	"github.com/unitaryai/osmia/pkg/engine"
)

// taskSecretNamePrefix prefixes the ephemeral Kubernetes Secret that holds
// the resolved values of a TaskRun's task-scoped secrets. The Secret is
// owned by the agent Job, so Kubernetes garbage-collects it when the Job is
// deleted.
const taskSecretNamePrefix = "osmia-task-secrets-"

// taskSecrets is the outcome of resolving a task's secret references: the
// env-name-to-SecretKeyRef mapping to merge into the ExecutionSpec, plus the
// resolved raw values that must first be written to an ephemeral Secret.
type taskSecrets struct {
	// KeyRefs maps environment variable names to the Kubernetes Secret key
	// they should be injected from.
	KeyRefs map[string]engine.SecretKeyRef

	// Values holds resolved plaintext secret values keyed by environment
	// variable name. These come from non-Kubernetes backends (Vault, AWS
	// Secrets Manager) and are written to the ephemeral Secret rather than
	// inlined into the Job manifest.
	Values map[string]string
}

// resolveTaskSecrets parses task-scoped secret references from a task's
// description and labels, resolves them through the configured resolver, and
// returns the injection plan.
//
// It returns a nil plan when no resolver is configured or the task declares
// no secrets. Any policy violation or backend failure is returned as an
// error: secret resolution is fail-closed, so a task that asks for a secret
// it may not have is never launched without it.
func (r *Reconciler) resolveTaskSecrets(ctx context.Context, task engine.Task) (*taskSecrets, error) {
	if r.secretsResolver == nil {
		return nil, nil
	}

	requests, err := taskSecretRequests(task)
	if err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return nil, nil
	}

	resolved, err := r.secretsResolver.Resolve(ctx, requests)
	if err != nil {
		return nil, fmt.Errorf("resolving task-scoped secrets: %w", err)
	}

	plan := &taskSecrets{
		KeyRefs: make(map[string]engine.SecretKeyRef),
		Values:  make(map[string]string),
	}
	for _, rs := range resolved {
		if rs.EnvName == "" {
			return nil, fmt.Errorf("resolved secret has no environment variable name")
		}
		if rs.SecretKeyRef != nil {
			plan.KeyRefs[rs.EnvName] = engine.SecretKeyRef{
				SecretName: rs.SecretKeyRef.SecretName,
				Key:        rs.SecretKeyRef.Key,
			}
			continue
		}
		plan.Values[rs.EnvName] = rs.Value
	}

	return plan, nil
}

// taskSecretRequests collects the secret requests declared by a task, from
// both the osmia:secrets comment block in its description and any
// osmia:secret:ENV=URI labels.
func taskSecretRequests(task engine.Task) ([]secretresolver.SecretRequest, error) {
	fromBody, err := secretresolver.ParseCommentBlock(task.Description)
	if err != nil {
		return nil, fmt.Errorf("parsing secret block from task description: %w", err)
	}

	fromLabels, err := secretresolver.ParseLabels(task.Labels)
	if err != nil {
		return nil, fmt.Errorf("parsing secret labels: %w", err)
	}

	return append(fromBody, fromLabels...), nil
}

// applyTaskSecrets merges a resolved secret plan into an ExecutionSpec.
//
// Values resolved from non-Kubernetes backends are not written into the spec
// directly: the caller is expected to have staged them into an ephemeral
// Secret named secretName, and this function points the corresponding env
// vars at that Secret. Plaintext secrets therefore never appear in the Job
// manifest.
//
// A task may not shadow an environment variable the engine already sets;
// doing so would let a ticket author override the agent's own credentials
// (for example ANTHROPIC_API_KEY). Collisions are rejected rather than
// silently resolved in either direction.
func applyTaskSecrets(spec *engine.ExecutionSpec, plan *taskSecrets, secretName string) error {
	if plan == nil {
		return nil
	}

	for _, envName := range sortedEnvNames(plan) {
		if err := checkEnvCollision(spec, envName); err != nil {
			return err
		}
	}

	if spec.SecretKeyRefs == nil {
		spec.SecretKeyRefs = make(map[string]engine.SecretKeyRef)
	}
	for envName, ref := range plan.KeyRefs {
		spec.SecretKeyRefs[envName] = ref
	}
	for envName := range plan.Values {
		spec.SecretKeyRefs[envName] = engine.SecretKeyRef{
			SecretName: secretName,
			Key:        envName,
		}
	}

	return nil
}

// sortedEnvNames returns every environment variable name in the plan in a
// stable order, so that a task declaring several colliding secrets always
// fails on the same one.
func sortedEnvNames(plan *taskSecrets) []string {
	names := make([]string, 0, len(plan.KeyRefs)+len(plan.Values))
	for envName := range plan.KeyRefs {
		names = append(names, envName)
	}
	for envName := range plan.Values {
		names = append(names, envName)
	}
	sort.Strings(names)
	return names
}

// checkEnvCollision reports an error when envName is already supplied by the
// engine's own execution spec.
func checkEnvCollision(spec *engine.ExecutionSpec, envName string) error {
	if _, ok := spec.Env[envName]; ok {
		return fmt.Errorf("task-scoped secret %q collides with an environment variable set by the engine", envName)
	}
	if _, ok := spec.SecretKeyRefs[envName]; ok {
		return fmt.Errorf("task-scoped secret %q collides with a secret reference set by the engine", envName)
	}
	if _, ok := spec.SecretEnv[envName]; ok {
		return fmt.Errorf("task-scoped secret %q collides with a secret mount set by the engine", envName)
	}
	return nil
}

// taskSecretName returns the name of the ephemeral Secret holding a
// TaskRun's resolved secret values.
func taskSecretName(taskRunID string) string {
	name := taskSecretNamePrefix + taskRunID
	if len(name) > 253 {
		name = name[:253]
	}
	return name
}

// createTaskSecret writes the plan's resolved values to an ephemeral
// Kubernetes Secret. It is a no-op when the plan holds no raw values, since
// Kubernetes-backed references are injected from their existing Secrets.
//
// The Secret is created before the Job so the kubelet never has to retry a
// missing reference; adoptTaskSecret then makes the Job its owner.
func (r *Reconciler) createTaskSecret(ctx context.Context, taskRunID, engineName string, plan *taskSecrets) error {
	if r.k8sClient == nil || plan == nil || len(plan.Values) == 0 {
		return nil
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      taskSecretName(taskRunID),
			Namespace: r.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/component":  "agent",
				"app.kubernetes.io/managed-by": "osmia",
				"osmia.io/engine":              engineName,
				jobbuilder.LabelTaskRunID:      taskRunID,
			},
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: plan.Values,
	}

	_, err := r.k8sClient.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating task secret for task run %s: %w", taskRunID, err)
	}
	return nil
}

// taskSecretCleanupTimeout bounds the detached context used for the two
// secret operations that must still run when the launch context is being
// torn down.
const taskSecretCleanupTimeout = 15 * time.Second

// detachedCleanupContext returns a bounded context that survives ctx's
// cancellation, keeping ctx's values (trace and log correlation) but not its
// deadline. Both remaining secret operations are finishing-up work: by the
// time they run, an ephemeral Secret holding plaintext already exists in the
// cluster, and inheriting a cancelled context would leave it there.
func detachedCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), taskSecretCleanupTimeout)
}

// adoptTaskSecret sets job as the owner of the TaskRun's ephemeral Secret so
// that deleting the Job garbage-collects the Secret with it. A failure here
// leaves an orphaned Secret rather than a broken run, so it is logged by the
// caller and not treated as fatal.
//
// It runs on a detached context: the Job already exists and will run whatever
// happens to the caller's context, so abandoning adoption on cancellation
// would orphan the Secret permanently.
func (r *Reconciler) adoptTaskSecret(ctx context.Context, taskRunID string, job *batchv1.Job, plan *taskSecrets) error {
	if r.k8sClient == nil || plan == nil || len(plan.Values) == 0 {
		return nil
	}

	ctx, cancel := detachedCleanupContext(ctx)
	defer cancel()

	name := taskSecretName(taskRunID)
	secrets := r.k8sClient.CoreV1().Secrets(r.namespace)

	secret, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("fetching task secret %s: %w", name, err)
	}

	secret.OwnerReferences = append(secret.OwnerReferences, metav1.OwnerReference{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       job.Name,
		UID:        job.UID,
	})

	if _, err := secrets.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("setting owner on task secret %s: %w", name, err)
	}
	return nil
}

// discardTaskSecret removes an ephemeral Secret that was written for a
// launch which then failed before the Job existed to own it. The launch is
// already being aborted, so a cleanup failure is logged rather than
// returned; it would otherwise mask the error that caused the abort.
func (r *Reconciler) discardTaskSecret(ctx context.Context, taskRunID string, plan *taskSecrets) {
	if err := r.deleteTaskSecret(ctx, taskRunID, plan); err != nil {
		r.logger.WarnContext(ctx, "failed to clean up task secret after aborted launch",
			"task_run_id", taskRunID,
			"error", err,
		)
	}
}

// deleteTaskSecret removes the TaskRun's ephemeral Secret. It is used to
// clean up when Job creation fails after the Secret was already written, at
// which point no Job exists to own it.
//
// It runs on a detached context. A launch aborted by cancellation is exactly
// when this matters most: inheriting the cancelled context would fail the
// delete and leave a Secret full of plaintext credentials with no owner to
// garbage-collect it.
func (r *Reconciler) deleteTaskSecret(ctx context.Context, taskRunID string, plan *taskSecrets) error {
	if r.k8sClient == nil || plan == nil || len(plan.Values) == 0 {
		return nil
	}

	ctx, cancel := detachedCleanupContext(ctx)
	defer cancel()

	name := taskSecretName(taskRunID)
	if err := r.k8sClient.CoreV1().Secrets(r.namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("deleting task secret %s: %w", name, err)
	}
	return nil
}
