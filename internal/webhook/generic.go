package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	svix "github.com/svix/svix-webhooks/go"
	"github.com/unitaryai/osmia/pkg/plugin/ticketing"
)

// GenericAuthMode defines the authentication method for the generic webhook.
type GenericAuthMode string

const (
	// GenericAuthHMAC validates requests using HMAC-SHA256 signature.
	GenericAuthHMAC GenericAuthMode = "hmac"

	// GenericAuthBearer validates requests using a bearer token.
	GenericAuthBearer GenericAuthMode = "bearer"

	// GenericAuthSvix validates requests using Svix-style signing
	// (used by incident.io, Stripe, OpenAI, Linear, and many other SaaS
	// providers built on Svix). Verification is delegated to the official
	// Svix Go library, which enforces a fixed five-minute timestamp
	// tolerance and accepts both "svix-*" and enterprise "webhook-*"
	// header prefixes.
	GenericAuthSvix GenericAuthMode = "svix"
)

// GenericConfig holds the configuration for the generic webhook handler.
//
// Callers should invoke Prepare() at startup (after populating the fields)
// to validate the configuration and pre-construct any auth-mode-specific
// verifiers. Skipping Prepare() leaves Svix mode unusable — every request
// will be rejected with a 500 because the verifier is nil.
type GenericConfig struct {
	// AuthMode is the authentication method: "hmac", "bearer", or "svix".
	AuthMode GenericAuthMode `json:"auth_mode" yaml:"auth_mode"`

	// Secret is the HMAC secret, Svix signing key, or bearer token,
	// depending on AuthMode. For Svix mode, a "whsec_" prefix is
	// recognised and the remainder is base64-decoded before use.
	Secret string `json:"secret" yaml:"secret"`

	// SignatureHeader is the header containing the HMAC signature.
	// Defaults to "X-Webhook-Signature" if empty. Only used in HMAC mode;
	// Svix mode rejects this field at Prepare() time because Svix uses
	// fixed headers (svix-* or webhook-*).
	SignatureHeader string `json:"signature_header" yaml:"signature_header"`

	// FieldMapping maps dot-notation JSON paths to ticket fields.
	// Supported target fields: id, title, description, ticket_type, repo_url, external_url.
	FieldMapping map[string]string `json:"field_mapping" yaml:"field_mapping"`

	// svixWebhook is the pre-constructed Svix verifier, populated by
	// Prepare() when AuthMode is svix. nil for other auth modes.
	svixWebhook *svix.Webhook
}

// Prepare validates the configuration and pre-constructs any runtime
// objects needed for request handling. It is idempotent and may be called
// more than once; each call re-runs validation and rebuilds the Svix
// verifier when applicable.
//
// In production code, callers should invoke Prepare() at controller
// startup so misconfigured secrets surface as a startup failure rather
// than as a per-request 500.
func (g *GenericConfig) Prepare() error {
	if g.Secret == "" {
		return fmt.Errorf("secret is required")
	}
	switch g.AuthMode {
	case GenericAuthHMAC, GenericAuthBearer:
		g.svixWebhook = nil
	case GenericAuthSvix:
		if g.SignatureHeader != "" {
			return fmt.Errorf("signature_header is not used in svix mode (Svix uses fixed svix-* or webhook-* headers)")
		}
		wh, err := svix.NewWebhook(g.Secret)
		if err != nil {
			return fmt.Errorf("invalid svix secret: %w", err)
		}
		g.svixWebhook = wh
	case "":
		return fmt.Errorf("auth_mode is required")
	default:
		return fmt.Errorf("unsupported auth_mode %q", g.AuthMode)
	}
	return nil
}

// handleGeneric processes incoming generic webhook deliveries. It supports
// configurable authentication (HMAC or bearer token) and configurable JSON
// field mapping for extracting ticket data from arbitrary payloads.
func (s *Server) handleGeneric(w http.ResponseWriter, r *http.Request) {
	if s.genericConfig == nil {
		s.logger.Error("generic webhook not configured")
		http.Error(w, "generic webhook not configured", http.StatusInternalServerError)
		return
	}

	cfg := s.genericConfig

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("failed to read request body", slog.String("error", err.Error()))
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	// Validate authentication.
	switch cfg.AuthMode {
	case GenericAuthHMAC:
		sigHeader := cfg.SignatureHeader
		if sigHeader == "" {
			sigHeader = "X-Webhook-Signature"
		}
		sig := r.Header.Get(sigHeader)
		if !validateGenericHMACSignature(body, sig, cfg.Secret) {
			s.logger.Warn("invalid generic webhook hmac signature")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	case GenericAuthBearer:
		auth := r.Header.Get("Authorization")
		expected := "Bearer " + cfg.Secret
		if auth != expected {
			s.logger.Warn("invalid generic webhook bearer token")
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
	case GenericAuthSvix:
		if cfg.svixWebhook == nil {
			s.logger.Error("svix webhook not prepared; call GenericConfig.Prepare() at startup")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := cfg.svixWebhook.Verify(body, r.Header); err != nil {
			s.logger.Warn("invalid generic webhook svix signature", slog.String("error", err.Error()))
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	default:
		s.logger.Error("unknown generic webhook auth mode", slog.String("mode", string(cfg.AuthMode)))
		http.Error(w, "invalid auth configuration", http.StatusInternalServerError)
		return
	}

	// Parse JSON payload.
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		s.logger.Error("failed to parse generic webhook payload", slog.String("error", err.Error()))
		http.Error(w, "invalid json payload", http.StatusBadRequest)
		return
	}

	// Extract ticket fields using configured field mapping.
	ticket := ticketing.Ticket{}
	if cfg.FieldMapping != nil {
		for jsonPath, field := range cfg.FieldMapping {
			val := extractJSONPath(raw, jsonPath)
			if val == "" {
				continue
			}
			switch field {
			case "id":
				ticket.ID = val
			case "title":
				ticket.Title = val
			case "description":
				ticket.Description = val
			case "ticket_type":
				ticket.TicketType = val
			case "repo_url":
				ticket.RepoURL = val
			case "external_url":
				ticket.ExternalURL = val
			}
		}
	}

	if ticket.ID == "" {
		s.logger.Warn("generic webhook payload did not produce a ticket ID")
		http.Error(w, "missing ticket id in payload", http.StatusBadRequest)
		return
	}

	if err := s.handler.HandleWebhookEvent(r.Context(), "generic", []ticketing.Ticket{ticket}); err != nil {
		s.logger.Error("failed to handle generic webhook event", slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("processed generic webhook",
		slog.String("ticket_id", ticket.ID),
		slog.String("title", ticket.Title),
	)
	w.WriteHeader(http.StatusOK)
}

// extractJSONPath extracts a value from a nested map using simple dot-notation
// paths (e.g. "issue.title"). Path segments containing literal dots can be
// addressed by escaping the dot with a backslash. For example, given a payload
// with a top-level key "public_alert.alert_created_v1" (an event-namespaced
// wrapper key, as used by incident.io), the path
// `public_alert\.alert_created_v1.id` resolves the "id" field inside that
// wrapper. A literal backslash in a segment is written as `\\`.
//
// This is intentionally simple — it does not support array indexing or
// complex JSONPath expressions.
func extractJSONPath(data map[string]any, path string) string {
	parts := splitEscapedPath(path)
	var current any = data

	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[part]
	}

	if current == nil {
		return ""
	}

	switch v := current.(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case bool:
		return fmt.Sprintf("%t", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// splitEscapedPath splits a dot-notation path on unescaped dots. A backslash
// escapes the following character, allowing path segments to contain literal
// dots (`\.`) or backslashes (`\\`). A trailing backslash with nothing to
// escape is treated as a literal backslash.
func splitEscapedPath(path string) []string {
	var parts []string
	var current strings.Builder
	escaped := false
	for _, r := range path {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '.':
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if escaped {
		current.WriteRune('\\')
	}
	parts = append(parts, current.String())
	return parts
}

// validateGenericHMACSignature checks the signature header against the
// HMAC-SHA256 of the request body. The signature may be hex-encoded with
// or without a "sha256=" prefix.
func validateGenericHMACSignature(body []byte, sigHeader, secret string) bool {
	if sigHeader == "" {
		return false
	}

	sigHex := strings.TrimPrefix(sigHeader, "sha256=")

	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	return hmac.Equal(sig, expected)
}

// computeGenericHMACSignature computes the HMAC-SHA256 signature for testing.
func computeGenericHMACSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return fmt.Sprintf("sha256=%s", hex.EncodeToString(mac.Sum(nil)))
}
