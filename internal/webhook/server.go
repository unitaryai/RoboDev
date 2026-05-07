package webhook

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"

	svix "github.com/svix/svix-webhooks/go"
)

// ApprovalHandler handles approval/rejection callbacks from interactive
// webhook sources (e.g. Slack buttons). The webhook server routes approval
// actions to this handler instead of forwarding them as tickets.
type ApprovalHandler interface {
	HandleApprovalCallback(ctx context.Context, taskRunID string, approved bool, responder string) error
}

// Server is the HTTP webhook receiver. It registers route handlers for each
// supported webhook source and delegates parsed events to an EventHandler.
type Server struct {
	mux     *http.ServeMux
	handler EventHandler
	logger  *slog.Logger
	server  *http.Server

	// secrets holds per-source webhook secrets used for signature validation.
	secrets map[string]string

	// genericConfig holds the configuration for the generic webhook handler.
	genericConfig *GenericConfig

	// incidentConfig holds the configuration for the incident.io webhook
	// handler. It is the typed sibling of genericConfig — incident.io's
	// payload schema is fixed, so no field_mapping is required.
	incidentConfig *IncidentIOConfig

	// incidentHandler, when set, receives parsed incident.io webhook events.
	// Without this handler, the /webhooks/incident-io endpoint responds 500.
	incidentHandler IncidentHandler

	// approvalHandler, when set, receives approval/rejection callbacks from
	// interactive webhook sources (e.g. Slack buttons) instead of forwarding
	// them as tickets.
	approvalHandler ApprovalHandler

	// shortcutTargetStateID, when non-zero, restricts Shortcut webhook
	// processing to story_update events where the workflow state changed to
	// this specific ID. Events that do not represent this transition are
	// acknowledged but not forwarded to the controller, preventing log noise
	// from unrelated story edits.
	shortcutTargetStateID int64

	// githubTriggerLabels, when non-empty, restricts GitHub webhook processing
	// to issues that carry at least one of these labels. This mirrors the
	// label-gating behaviour of the polling backend and prevents any newly
	// opened issue from triggering execution regardless of its labels.
	githubTriggerLabels []string
}

// Option is a functional option for configuring a Server. Options may
// return an error to abort server construction (e.g. when validation of
// a configuration value fails).
type Option func(*Server) error

// WithSecret configures a webhook signing secret for the given source.
// Supported sources: "github", "gitlab", "slack", "shortcut".
func WithSecret(source, secret string) Option {
	return func(s *Server) error {
		s.secrets[source] = secret
		return nil
	}
}

// WithGenericConfig sets the configuration for the generic webhook handler.
// The supplied config is validated and prepared for use (e.g. pre-constructing
// the Svix verifier when AuthMode is svix); construction errors abort
// NewServer rather than failing per-request.
func WithGenericConfig(cfg *GenericConfig) Option {
	return func(s *Server) error {
		if cfg == nil {
			return fmt.Errorf("generic webhook config: config is nil")
		}
		if err := cfg.Prepare(); err != nil {
			return fmt.Errorf("generic webhook config: %w", err)
		}
		s.genericConfig = cfg
		return nil
	}
}

// WithIncidentConfig sets the configuration for the incident.io webhook
// handler. The supplied config is validated and prepared for use (the
// Svix verifier is pre-constructed). Construction errors abort NewServer
// rather than failing per-request.
func WithIncidentConfig(cfg *IncidentIOConfig) Option {
	return func(s *Server) error {
		if cfg == nil {
			return fmt.Errorf("incident.io webhook config: config is nil")
		}
		if err := cfg.Prepare(); err != nil {
			return fmt.Errorf("incident.io webhook config: %w", err)
		}
		s.incidentConfig = cfg
		return nil
	}
}

// WithIncidentHandler sets the handler for parsed incident.io webhook
// events. Mirrors WithApprovalHandler. Without a handler the
// /webhooks/incident-io endpoint responds 500 with "no handler configured".
func WithIncidentHandler(h IncidentHandler) Option {
	return func(s *Server) error {
		s.incidentHandler = h
		return nil
	}
}

// WithShortcutTargetStateID restricts Shortcut webhook handling to story
// updates where the workflow state transitioned to id. Set this to the same
// workflow state ID configured on the Shortcut ticketing backend so that only
// relevant state transitions trigger task processing.
func WithShortcutTargetStateID(id int64) Option {
	return func(s *Server) error {
		s.shortcutTargetStateID = id
		return nil
	}
}

// WithApprovalHandler sets the handler for approval/rejection callbacks.
// When set, approval actions from Slack (osmia_approval_*) are routed to
// this handler instead of being logged and discarded.
func WithApprovalHandler(h ApprovalHandler) Option {
	return func(s *Server) error {
		s.approvalHandler = h
		return nil
	}
}

// WithGitHubTriggerLabels restricts GitHub webhook handling to issues that
// carry at least one of the given labels. When not set (or empty), all
// opened/labelled issues are forwarded, which bypasses the trigger-label
// contract enforced by the polling backend.
func WithGitHubTriggerLabels(labels []string) Option {
	return func(s *Server) error {
		s.githubTriggerLabels = labels
		return nil
	}
}

// NewServer creates a new webhook Server with routes registered for each
// supported source. The handler receives parsed webhook events. Use
// functional options to configure per-source secrets. Returns an error
// when any option fails its validation (e.g. a malformed Svix secret).
func NewServer(logger *slog.Logger, handler EventHandler, opts ...Option) (*Server, error) {
	s := &Server{
		mux:     http.NewServeMux(),
		handler: handler,
		logger:  logger,
		secrets: make(map[string]string),
	}

	for _, opt := range opts {
		if err := opt(s); err != nil {
			return nil, err
		}
	}

	// Register routes.
	s.mux.HandleFunc("POST /webhooks/github", s.handleGitHub)
	s.mux.HandleFunc("POST /webhooks/gitlab", s.handleGitLab)
	s.mux.HandleFunc("POST /webhooks/slack", s.handleSlack)
	s.mux.HandleFunc("POST /webhooks/shortcut", s.handleShortcut)
	s.mux.HandleFunc("POST /webhooks/generic", s.handleGeneric)
	s.mux.HandleFunc("POST /webhooks/incident-io", s.handleIncidentIO)
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)

	return s, nil
}

// RegisterRoute adds a custom route to the server's mux. This can be used
// to extend the server with additional webhook sources beyond the built-in
// handlers.
func (s *Server) RegisterRoute(pattern string, handler http.HandlerFunc) {
	s.mux.HandleFunc(pattern, handler)
}

// ListenAndServe starts the HTTP server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}
	s.logger.Info("webhook server starting", slog.String("addr", addr))
	return s.server.ListenAndServe()
}

// Serve accepts connections on the given listener.
func (s *Server) Serve(ln net.Listener) error {
	s.server = &http.Server{
		Handler: s.mux,
	}
	s.logger.Info("webhook server starting", slog.String("addr", ln.Addr().String()))
	return s.server.Serve(ln)
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	s.logger.Info("webhook server shutting down")
	return s.server.Shutdown(ctx)
}

// ServeHTTP implements http.Handler, allowing the Server to be used directly
// in tests or composed into a larger mux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// handleHealthz responds with 200 OK for liveness/readiness probes.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// readRequestBody reads the entire request body and writes a 400
// response on failure. Used by webhook handlers that need to validate
// the body before parsing it. The bool return is false on failure (and
// in that case body is nil and the response has already been written).
func (s *Server) readRequestBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("failed to read request body", slog.String("error", err.Error()))
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return nil, false
	}
	return body, true
}

// verifySvixSignature validates a Svix-signed webhook request using the
// supplied verifier. On failure it writes the appropriate HTTP error
// response (401 for invalid signature, 500 if the verifier is nil) and
// returns false. The source argument is added as a structured log field
// so log streams can distinguish svix failures from different webhook
// endpoints.
func (s *Server) verifySvixSignature(w http.ResponseWriter, r *http.Request, body []byte, verifier *svix.Webhook, source string) bool {
	if verifier == nil {
		s.logger.Error("svix webhook not prepared; call Prepare() at startup",
			slog.String("source", source))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return false
	}
	if err := verifier.Verify(body, r.Header); err != nil {
		s.logger.Warn("invalid svix signature",
			slog.String("source", source),
			slog.String("error", err.Error()))
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return false
	}
	return true
}
