// Package secrets defines the SecretsBackend interface for retrieving
// sensitive values (API keys, tokens, credentials) from external secret
// stores. Built-in support for Kubernetes Secrets is provided; third-party
// backends (Vault, AWS Secrets Manager, 1Password) can be added via gRPC
// plugins.
package secrets

import (
	"context"

	corev1 "k8s.io/api/core/v1"
)

// InterfaceVersion is the current version of the SecretsBackend interface.
const InterfaceVersion = 1

// Backend is the interface that secrets backends must implement.
// It provides operations for retrieving secrets and building Kubernetes
// environment variable references.
type Backend interface {
	// GetSecret retrieves a single secret value by key.
	GetSecret(ctx context.Context, key string) (string, error)

	// GetSecrets retrieves multiple secret values by key. The returned map
	// is keyed by the requested key names.
	GetSecrets(ctx context.Context, keys []string) (map[string]string, error)

	// BuildEnvVars translates secret references into Kubernetes EnvVar
	// definitions suitable for inclusion in a pod spec. The secretRefs map
	// is keyed by environment variable name, with values being the secret
	// key to look up.
	BuildEnvVars(secretRefs map[string]string) ([]corev1.EnvVar, error)

	// Name returns the unique identifier for this backend (e.g. "k8s", "vault").
	Name() string

	// InterfaceVersion returns the version of the SecretsBackend interface
	// that this backend implements.
	InterfaceVersion() int
}
