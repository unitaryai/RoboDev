package secretresolver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/unitaryai/osmia/pkg/plugin/secrets"
)

// Resolver performs multi-backend secret resolution. It validates requests
// against a policy, expands aliases, and dispatches to the appropriate
// secrets backend based on URI scheme.
type Resolver struct {
	backends map[string]secrets.Backend
	aliases  map[string]SecretAlias
	policy   Policy
	logger   *slog.Logger
	audit    *AuditLogger
}

// Option is a functional option for configuring a Resolver.
type Option func(*Resolver)

// WithBackend registers a secrets backend for a given URI scheme.
func WithBackend(scheme string, backend secrets.Backend) Option {
	return func(r *Resolver) {
		r.backends[scheme] = backend
	}
}

// WithAliases sets the alias map for the resolver.
func WithAliases(aliases map[string]SecretAlias) Option {
	return func(r *Resolver) {
		r.aliases = aliases
	}
}

// WithPolicy sets the security policy for the resolver.
func WithPolicy(policy Policy) Option {
	return func(r *Resolver) {
		r.policy = policy
	}
}

// WithLogger sets the structured logger for the resolver.
func WithLogger(logger *slog.Logger) Option {
	return func(r *Resolver) {
		r.logger = logger
		r.audit = NewAuditLogger(logger)
	}
}

// NewResolver creates a new Resolver with the given options.
func NewResolver(opts ...Option) *Resolver {
	r := &Resolver{
		backends: make(map[string]secrets.Backend),
		aliases:  make(map[string]SecretAlias),
		logger:   slog.Default(),
	}
	for _, opt := range opts {
		opt(r)
	}
	if r.audit == nil {
		r.audit = NewAuditLogger(r.logger)
	}
	return r
}

// Resolve validates, expands, and resolves a list of secret requests.
// It returns the resolved secrets ready for injection into an execution
// environment.
func (r *Resolver) Resolve(ctx context.Context, requests []SecretRequest) ([]ResolvedSecret, error) {
	// Each request costs at least one call to a secrets backend, and the
	// list originates in ticket or incident text. Bound it here, at the one
	// point every caller passes through, rather than in each parser.
	if len(requests) > MaxSecretRequests {
		return nil, fmt.Errorf("task declares %d secrets, over the limit of %d",
			len(requests), MaxSecretRequests)
	}

	// Whether a task may name a secret directly is a property of what the
	// task asked for, so it is checked before expansion. Checking it after
	// would reject every alias whenever AllowRawRefs is false, because an
	// alias expands into exactly the concrete URI that setting forbids,
	// which would leave the recommended fail-closed policy resolving
	// nothing at all.
	for _, req := range requests {
		if err := ValidateRawRef(r.policy, req); err != nil {
			return nil, fmt.Errorf("policy violation for %q: %w", requestLabel(req), err)
		}
	}

	expanded, err := r.expandAliases(requests)
	if err != nil {
		return nil, fmt.Errorf("expanding aliases: %w", err)
	}

	// Scheme and environment-variable rules apply to the concrete URI an
	// alias resolves to, so they are checked after expansion.
	for _, req := range expanded {
		if err := ValidateResolved(r.policy, req); err != nil {
			return nil, fmt.Errorf("policy violation for %q: %w", req.EnvName, err)
		}
	}

	// Resolve each request via the appropriate backend.
	resolved := make([]ResolvedSecret, 0, len(expanded))
	for _, req := range expanded {
		rs, err := r.resolveOne(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("resolving secret for %q: %w", req.EnvName, err)
		}
		resolved = append(resolved, rs)
	}

	return resolved, nil
}

// requestLabel names a request in an error message. An alias-only request
// has no environment variable name yet, so its URI identifies it.
func requestLabel(req SecretRequest) string {
	if req.EnvName != "" {
		return req.EnvName
	}
	return req.URI
}

// expandAliases replaces alias:// URIs with concrete URIs from the alias map.
func (r *Resolver) expandAliases(requests []SecretRequest) ([]SecretRequest, error) {
	var expanded []SecretRequest
	for _, req := range requests {
		scheme := parseScheme(req.URI)
		if scheme != "alias" {
			expanded = append(expanded, req)
			continue
		}

		// Extract alias name from URI (alias://name).
		aliasName := strings.TrimPrefix(req.URI, "alias://")
		alias, ok := r.aliases[aliasName]
		if !ok {
			return nil, fmt.Errorf("unknown secret alias %q", aliasName)
		}

		// Validate tenant scoping.
		if err := ValidateAliasTenant(r.policy, alias); err != nil {
			return nil, err
		}

		// A task referencing an alias may name the target environment
		// variable itself; otherwise the alias supplies it. Falling back to
		// the alias's own name is a last resort for aliases that predate
		// EnvName and happen to be named like an environment variable: an
		// alias called "anthropic-key" would otherwise inject a variable of
		// that name and be rejected by the env-name policy.
		envName := req.EnvName
		if envName == "" {
			envName = alias.EnvName
		}
		if envName == "" {
			envName = alias.Name
		}

		expanded = append(expanded, SecretRequest{
			EnvName: envName,
			URI:     alias.URI,
		})
	}
	return expanded, nil
}

// resolveOne resolves a single secret request by dispatching to the
// appropriate backend based on the URI scheme.
func (r *Resolver) resolveOne(ctx context.Context, req SecretRequest) (ResolvedSecret, error) {
	scheme := parseScheme(req.URI)
	if scheme == "" {
		return ResolvedSecret{}, fmt.Errorf("invalid URI %q: missing scheme", req.URI)
	}

	// Parse the key from the URI. The key is everything after "scheme://".
	key := strings.TrimPrefix(req.URI, scheme+"://")

	// K8s references are injected natively as a secretKeyRef, so the
	// controller never reads the value and no backend call is made. This is
	// deliberately handled before the backend lookup: requiring a
	// registered k8s backend to emit a reference that never uses it would
	// make k8s:// refs fail for operators who configure no backends at all.
	if scheme == "k8s" {
		return ResolvedSecret{
			EnvName: req.EnvName,
			SecretKeyRef: &SecretKeyRef{
				SecretName: parseK8sSecretName(key),
				Key:        parseK8sSecretKey(key),
			},
		}, nil
	}

	backend, ok := r.backends[scheme]
	if !ok {
		return ResolvedSecret{}, fmt.Errorf("no backend registered for scheme %q", scheme)
	}

	// For other backends, fetch the value.
	value, err := backend.GetSecret(ctx, key)
	if err != nil {
		return ResolvedSecret{}, fmt.Errorf("backend %q: %w", backend.Name(), err)
	}

	return ResolvedSecret{
		EnvName: req.EnvName,
		Value:   value,
	}, nil
}

// parseK8sSecretName extracts the secret name from a K8s URI key.
// Key format: "secretName/dataKey".
func parseK8sSecretName(key string) string {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) < 1 {
		return ""
	}
	return parts[0]
}

// parseK8sSecretKey extracts the data key from a K8s URI key.
// Key format: "secretName/dataKey".
func parseK8sSecretKey(key string) string {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}
