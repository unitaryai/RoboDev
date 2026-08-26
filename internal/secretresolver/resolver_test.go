package secretresolver

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBackend is a test double for secrets.Backend.
type mockBackend struct {
	name    string
	secrets map[string]string
}

func (m *mockBackend) GetSecret(_ context.Context, key string) (string, error) {
	val, ok := m.secrets[key]
	if !ok {
		return "", fmt.Errorf("secret %q not found", key)
	}
	return val, nil
}

func (m *mockBackend) GetSecrets(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		val, ok := m.secrets[key]
		if !ok {
			return nil, fmt.Errorf("secret %q not found", key)
		}
		result[key] = val
	}
	return result, nil
}

func (m *mockBackend) BuildEnvVars(secretRefs map[string]string) ([]corev1.EnvVar, error) {
	envVars := make([]corev1.EnvVar, 0, len(secretRefs))
	for envName, value := range secretRefs {
		envVars = append(envVars, corev1.EnvVar{Name: envName, Value: value})
	}
	return envVars, nil
}

func (m *mockBackend) Name() string          { return m.name }
func (m *mockBackend) InterfaceVersion() int { return 1 }

func TestResolverResolve(t *testing.T) {
	vaultBackend := &mockBackend{
		name: "vault",
		secrets: map[string]string{
			"secret/data/stripe/test-key#api_key": "sk_test_123",
			"secret/data/db#url":                  "postgres://localhost:5432/mydb",
		},
	}

	k8sBackend := &mockBackend{
		name: "k8s",
		secrets: map[string]string{
			"my-secret/token": "ghp_abc123",
		},
	}

	tests := []struct {
		name     string
		requests []SecretRequest
		aliases  map[string]SecretAlias
		policy   Policy
		want     []ResolvedSecret
		wantErr  string
	}{
		{
			name: "resolve vault secret via raw ref",
			requests: []SecretRequest{
				{EnvName: "STRIPE_API_KEY", URI: "vault://secret/data/stripe/test-key#api_key"},
			},
			policy: Policy{AllowRawRefs: true, AllowedSchemes: []string{"vault", "k8s"}},
			want: []ResolvedSecret{
				{EnvName: "STRIPE_API_KEY", Value: "sk_test_123"},
			},
		},
		{
			name: "resolve k8s secret returns SecretKeyRef",
			requests: []SecretRequest{
				{EnvName: "GH_TOKEN", URI: "k8s://my-secret/token"},
			},
			policy: Policy{AllowRawRefs: true, AllowedSchemes: []string{"vault", "k8s"}},
			want: []ResolvedSecret{
				{
					EnvName: "GH_TOKEN",
					SecretKeyRef: &SecretKeyRef{
						SecretName: "my-secret",
						Key:        "token",
					},
				},
			},
		},
		{
			name: "resolve alias",
			requests: []SecretRequest{
				{URI: "alias://stripe-test"},
			},
			aliases: map[string]SecretAlias{
				"stripe-test": {
					Name: "STRIPE_API_KEY",
					URI:  "vault://secret/data/stripe/test-key#api_key",
				},
			},
			policy: Policy{AllowRawRefs: true, AllowedSchemes: []string{"vault", "k8s", "alias"}},
			want: []ResolvedSecret{
				{EnvName: "STRIPE_API_KEY", Value: "sk_test_123"},
			},
		},
		{
			name: "alias with tenant scoping",
			requests: []SecretRequest{
				{URI: "alias://stripe-test"},
			},
			aliases: map[string]SecretAlias{
				"stripe-test": {
					Name:     "STRIPE_API_KEY",
					URI:      "vault://secret/data/stripe/test-key#api_key",
					TenantID: "team-alpha",
				},
			},
			policy: Policy{
				AllowRawRefs:   true,
				AllowedSchemes: []string{"vault", "alias"},
				TenantID:       "team-beta",
			},
			wantErr: "not accessible from tenant",
		},
		{
			name: "policy violation blocks resolution",
			requests: []SecretRequest{
				{EnvName: "PATH", URI: "vault://secret/data/evil#path"},
			},
			policy: Policy{
				AllowRawRefs:       true,
				AllowedSchemes:     []string{"vault"},
				BlockedEnvPatterns: []string{"PATH"},
			},
			wantErr: "policy violation",
		},
		{
			name: "unknown alias",
			requests: []SecretRequest{
				{URI: "alias://nonexistent"},
			},
			policy:  Policy{AllowRawRefs: false, AllowedSchemes: []string{"alias"}},
			wantErr: "unknown secret alias",
		},
		{
			name: "unknown scheme",
			requests: []SecretRequest{
				{EnvName: "MY_SECRET", URI: "aws-sm://my-secret#key"},
			},
			policy:  Policy{AllowRawRefs: true, AllowedSchemes: []string{"vault", "k8s", "aws-sm"}},
			wantErr: "no backend registered for scheme",
		},
		{
			name: "multiple requests resolved together",
			requests: []SecretRequest{
				{EnvName: "STRIPE_API_KEY", URI: "vault://secret/data/stripe/test-key#api_key"},
				{EnvName: "DATABASE_URL", URI: "vault://secret/data/db#url"},
			},
			policy: Policy{AllowRawRefs: true, AllowedSchemes: []string{"vault", "k8s"}},
			want: []ResolvedSecret{
				{EnvName: "STRIPE_API_KEY", Value: "sk_test_123"},
				{EnvName: "DATABASE_URL", Value: "postgres://localhost:5432/mydb"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []Option{
				WithBackend("vault", vaultBackend),
				WithBackend("k8s", k8sBackend),
				WithPolicy(tt.policy),
				WithLogger(slog.Default()),
			}
			if tt.aliases != nil {
				opts = append(opts, WithAliases(tt.aliases))
			}

			resolver := NewResolver(opts...)
			got, err := resolver.Resolve(context.Background(), tt.requests)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestAliasEnvNameResolution covers which environment variable an alias
// reference injects into. An alias exists so the task author names only the
// alias, which means the alias must be able to carry the target variable;
// falling back to the alias's own key would inject something like
// "anthropic-key" and be rejected by allowed_env_patterns.
func TestAliasEnvNameResolution(t *testing.T) {
	tests := []struct {
		name    string
		alias   SecretAlias
		request SecretRequest
		want    string
	}{
		{
			name:    "alias supplies the env name",
			alias:   SecretAlias{Name: "anthropic-key", EnvName: "ANTHROPIC_API_KEY", URI: "k8s://anthropic/api_key"},
			request: SecretRequest{URI: "alias://anthropic-key"},
			want:    "ANTHROPIC_API_KEY",
		},
		{
			name:    "request env name wins over the alias default",
			alias:   SecretAlias{Name: "anthropic-key", EnvName: "ANTHROPIC_API_KEY", URI: "k8s://anthropic/api_key"},
			request: SecretRequest{EnvName: "ANTHROPIC_OVERRIDE", URI: "alias://anthropic-key"},
			want:    "ANTHROPIC_OVERRIDE",
		},
		{
			name:    "alias without an env name falls back to its own key",
			alias:   SecretAlias{Name: "ANTHROPIC_API_KEY", URI: "k8s://anthropic/api_key"},
			request: SecretRequest{URI: "alias://ANTHROPIC_API_KEY"},
			want:    "ANTHROPIC_API_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewResolver(
				WithPolicy(Policy{
					AllowedSchemes:     []string{"alias", "k8s"},
					AllowedEnvPatterns: []string{"ANTHROPIC_*"},
				}),
				WithAliases(map[string]SecretAlias{tt.alias.Name: tt.alias}),
			)

			got, err := resolver.Resolve(context.Background(), []SecretRequest{tt.request})

			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, tt.want, got[0].EnvName)
		})
	}
}

// TestAliasesResolveUnderFailClosedPolicy pins the central contract of the
// recommended configuration: with allow_raw_refs false, an alias resolves and
// a raw reference does not. Validating the raw-ref rule after alias expansion
// rejected both, because an expanded alias is a concrete URI, which left the
// fail-closed policy resolving nothing at all.
func TestAliasesResolveUnderFailClosedPolicy(t *testing.T) {
	newResolver := func() *Resolver {
		return NewResolver(
			WithPolicy(Policy{
				AllowRawRefs:   false,
				AllowedSchemes: []string{"k8s"},
			}),
			WithAliases(map[string]SecretAlias{
				"staging-db": {Name: "staging-db", EnvName: "DATABASE_URL", URI: "k8s://staging-db/url"},
			}),
		)
	}

	t.Run("alias resolves", func(t *testing.T) {
		got, err := newResolver().Resolve(context.Background(), []SecretRequest{
			{URI: "alias://staging-db"},
		})

		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "DATABASE_URL", got[0].EnvName)
		require.NotNil(t, got[0].SecretKeyRef)
		assert.Equal(t, "staging-db", got[0].SecretKeyRef.SecretName)
	})

	t.Run("raw reference is still rejected", func(t *testing.T) {
		_, err := newResolver().Resolve(context.Background(), []SecretRequest{
			{EnvName: "DATABASE_URL", URI: "k8s://staging-db/url"},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "raw secret references are not permitted")
	})

	t.Run("an unknown alias is rejected", func(t *testing.T) {
		_, err := newResolver().Resolve(context.Background(), []SecretRequest{
			{URI: "alias://not-configured"},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown secret alias")
	})
}

// TestResolveK8sRefWithoutRegisteredBackend pins that a k8s:// reference
// resolves to a native secretKeyRef even when no k8s backend is registered.
// The controller never reads the value for these, so requiring a backend
// would fail operators who rely solely on in-cluster Secrets.
func TestResolveK8sRefWithoutRegisteredBackend(t *testing.T) {
	resolver := NewResolver(
		WithPolicy(Policy{AllowRawRefs: true, AllowedSchemes: []string{"k8s"}}),
	)

	got, err := resolver.Resolve(context.Background(), []SecretRequest{
		{EnvName: "DATABASE_URL", URI: "k8s://db-secret/url"},
	})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "DATABASE_URL", got[0].EnvName)
	assert.Empty(t, got[0].Value)
	require.NotNil(t, got[0].SecretKeyRef)
	assert.Equal(t, "db-secret", got[0].SecretKeyRef.SecretName)
	assert.Equal(t, "url", got[0].SecretKeyRef.Key)
}

func TestParseScheme(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"vault://secret/data/db#url", "vault"},
		{"k8s://my-secret/key", "k8s"},
		{"alias://stripe-test", "alias"},
		{"aws-sm://secret-name#key", "aws-sm"},
		{"invalid-uri", ""},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			got := parseScheme(tt.uri)
			assert.Equal(t, tt.want, got)
		})
	}
}
