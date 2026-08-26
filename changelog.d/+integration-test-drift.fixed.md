**Integration test drift in `tests/integration`**: repaired a compile
break in `webhook_reconciler_test.go` left by `internal/webhook.NewServer`
returning `(*Server, error)`, then aligned three tests with the current,
deliberate production contract rather than stale expectations:
`TestJobBuilderSecurityHardening` now expects `RunAsUser: 10000` (matching
`fsGroup` for EBS volume ownership, not the old `1000`);
`TestAllEnginesProduceValidSpecs` now accepts either `SecretEnv` or
`SecretKeyRefs` (claude-code injects secrets exclusively via the latter);
`TestReconcilerJobFailureAndRetry` now polls for the terminal `Running`
state reached after a successful retry launch instead of racing to catch
the transient `Retrying` state, which `handleJobFailed` supersedes
synchronously within the same reconcile tick.
