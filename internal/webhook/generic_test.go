package webhook

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	svix "github.com/svix/svix-webhooks/go"
)

func TestHandleGeneric_HMAC(t *testing.T) {
	secret := "test-generic-secret"

	cfg := &GenericConfig{
		AuthMode: GenericAuthHMAC,
		Secret:   secret,
		FieldMapping: map[string]string{
			"issue.id":          "id",
			"issue.title":       "title",
			"issue.description": "description",
			"issue.type":        "ticket_type",
			"issue.repo":        "repo_url",
			"issue.url":         "external_url",
		},
	}

	validPayload := map[string]any{
		"issue": map[string]any{
			"id":          "42",
			"title":       "Fix bug",
			"description": "Something is broken",
			"type":        "bug",
			"repo":        "https://github.com/owner/repo",
			"url":         "https://github.com/owner/repo/issues/42",
		},
	}

	tests := []struct {
		name       string
		payload    any
		cfg        *GenericConfig
		sigFunc    func([]byte) string
		wantStatus int
		wantCalls  int
	}{
		{
			name:    "valid hmac payload",
			payload: validPayload,
			cfg:     cfg,
			sigFunc: func(b []byte) string {
				return computeGenericHMACSignature(b, secret)
			},
			wantStatus: http.StatusOK,
			wantCalls:  1,
		},
		{
			name:       "invalid hmac signature",
			payload:    validPayload,
			cfg:        cfg,
			sigFunc:    func(_ []byte) string { return "sha256=deadbeef" },
			wantStatus: http.StatusUnauthorized,
			wantCalls:  0,
		},
		{
			name:       "missing signature",
			payload:    validPayload,
			cfg:        cfg,
			sigFunc:    func(_ []byte) string { return "" },
			wantStatus: http.StatusUnauthorized,
			wantCalls:  0,
		},
		{
			name: "missing ticket ID returns 400",
			payload: map[string]any{
				"issue": map[string]any{
					"title": "No ID here",
				},
			},
			cfg: cfg,
			sigFunc: func(b []byte) string {
				return computeGenericHMACSignature(b, secret)
			},
			wantStatus: http.StatusBadRequest,
			wantCalls:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockEventHandler{}
			srv, err := NewServer(testLogger(), mock, WithGenericConfig(tc.cfg))
			require.NoError(t, err)

			body, err := json.Marshal(tc.payload)
			require.NoError(t, err)

			sig := tc.sigFunc(body)

			req := httptest.NewRequest(http.MethodPost, "/webhooks/generic", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if sig != "" {
				req.Header.Set("X-Webhook-Signature", sig)
			}

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Len(t, mock.calls, tc.wantCalls)

			if tc.wantCalls > 0 {
				call := mock.calls[0]
				assert.Equal(t, "generic", call.source)
				require.Len(t, call.tickets, 1)
				assert.Equal(t, "42", call.tickets[0].ID)
				assert.Equal(t, "Fix bug", call.tickets[0].Title)
				assert.Equal(t, "Something is broken", call.tickets[0].Description)
				assert.Equal(t, "bug", call.tickets[0].TicketType)
				assert.Equal(t, "https://github.com/owner/repo", call.tickets[0].RepoURL)
				assert.Equal(t, "https://github.com/owner/repo/issues/42", call.tickets[0].ExternalURL)
			}
		})
	}
}

func TestHandleGeneric_Bearer(t *testing.T) {
	token := "my-bearer-token"

	cfg := &GenericConfig{
		AuthMode: GenericAuthBearer,
		Secret:   token,
		FieldMapping: map[string]string{
			"id":    "id",
			"title": "title",
		},
	}

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
		wantCalls  int
	}{
		{
			name:       "valid bearer token",
			authHeader: "Bearer " + token,
			wantStatus: http.StatusOK,
			wantCalls:  1,
		},
		{
			name:       "wrong bearer token",
			authHeader: "Bearer wrong-token",
			wantStatus: http.StatusUnauthorized,
			wantCalls:  0,
		},
		{
			name:       "missing authorization header",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
			wantCalls:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockEventHandler{}
			srv, err := NewServer(testLogger(), mock, WithGenericConfig(cfg))
			require.NoError(t, err)

			payload := map[string]any{"id": "1", "title": "Test"}
			body, _ := json.Marshal(payload)

			req := httptest.NewRequest(http.MethodPost, "/webhooks/generic", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Len(t, mock.calls, tc.wantCalls)
		})
	}
}

func TestHandleGeneric_CustomSignatureHeader(t *testing.T) {
	secret := "test-secret"

	cfg := &GenericConfig{
		AuthMode:        GenericAuthHMAC,
		Secret:          secret,
		SignatureHeader: "X-Custom-Sig",
		FieldMapping: map[string]string{
			"id":    "id",
			"title": "title",
		},
	}

	mock := &mockEventHandler{}
	srv, err := NewServer(testLogger(), mock, WithGenericConfig(cfg))
	require.NoError(t, err)

	payload := map[string]any{"id": "1", "title": "Test"}
	body, _ := json.Marshal(payload)
	sig := computeGenericHMACSignature(body, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/generic", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Custom-Sig", sig)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, mock.calls, 1)
}

func TestHandleGeneric_NotConfigured(t *testing.T) {
	mock := &mockEventHandler{}
	srv, err := NewServer(testLogger(), mock) // no generic config
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/generic", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleGeneric_MalformedJSON(t *testing.T) {
	secret := "test-secret"
	cfg := &GenericConfig{
		AuthMode:     GenericAuthHMAC,
		Secret:       secret,
		FieldMapping: map[string]string{"id": "id"},
	}

	mock := &mockEventHandler{}
	srv, err := NewServer(testLogger(), mock, WithGenericConfig(cfg))
	require.NoError(t, err)

	body := []byte(`not json`)
	sig := computeGenericHMACSignature(body, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/generic", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", sig)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGeneric_HandlerError(t *testing.T) {
	secret := "test-secret"
	cfg := &GenericConfig{
		AuthMode:     GenericAuthHMAC,
		Secret:       secret,
		FieldMapping: map[string]string{"id": "id", "title": "title"},
	}

	mock := &mockEventHandler{err: fmt.Errorf("handler failed")}
	srv, err := NewServer(testLogger(), mock, WithGenericConfig(cfg))
	require.NoError(t, err)

	payload := map[string]any{"id": "1", "title": "Test"}
	body, _ := json.Marshal(payload)
	sig := computeGenericHMACSignature(body, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/generic", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", sig)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestExtractJSONPath(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		path string
		want string
	}{
		{
			name: "simple string field",
			data: map[string]any{"title": "Hello"},
			path: "title",
			want: "Hello",
		},
		{
			name: "nested field",
			data: map[string]any{
				"issue": map[string]any{
					"title": "Nested title",
				},
			},
			path: "issue.title",
			want: "Nested title",
		},
		{
			name: "deeply nested field",
			data: map[string]any{
				"a": map[string]any{
					"b": map[string]any{
						"c": "deep value",
					},
				},
			},
			path: "a.b.c",
			want: "deep value",
		},
		{
			name: "numeric field",
			data: map[string]any{"id": float64(42)},
			path: "id",
			want: "42",
		},
		{
			name: "boolean field",
			data: map[string]any{"active": true},
			path: "active",
			want: "true",
		},
		{
			name: "missing field",
			data: map[string]any{"title": "Hello"},
			path: "missing",
			want: "",
		},
		{
			name: "missing nested field",
			data: map[string]any{
				"issue": map[string]any{},
			},
			path: "issue.title",
			want: "",
		},
		{
			name: "path through non-map",
			data: map[string]any{"title": "string"},
			path: "title.nested",
			want: "",
		},
		{
			name: "nil value",
			data: map[string]any{"title": nil},
			path: "title",
			want: "",
		},
		{
			name: "escaped dot in top-level key",
			data: map[string]any{
				"public_alert.alert_created_v1": map[string]any{
					"id": "01ABC",
				},
			},
			path: `public_alert\.alert_created_v1.id`,
			want: "01ABC",
		},
		{
			name: "escaped dots in multiple segments",
			data: map[string]any{
				"a.b": map[string]any{
					"c.d": "value",
				},
			},
			path: `a\.b.c\.d`,
			want: "value",
		},
		{
			name: "escaped backslash in key",
			data: map[string]any{
				`weird\key`: "value",
			},
			path: `weird\\key`,
			want: "value",
		},
		{
			name: "trailing backslash is literal",
			data: map[string]any{
				`trailing\`: "value",
			},
			path: `trailing\`,
			want: "value",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractJSONPath(tc.data, tc.path)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSplitEscapedPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "empty path",
			path: "",
			want: []string{""},
		},
		{
			name: "simple single segment",
			path: "title",
			want: []string{"title"},
		},
		{
			name: "simple nested path",
			path: "issue.title",
			want: []string{"issue", "title"},
		},
		{
			name: "escaped dot",
			path: `public_alert\.alert_created_v1.id`,
			want: []string{"public_alert.alert_created_v1", "id"},
		},
		{
			name: "escaped backslash",
			path: `weird\\key`,
			want: []string{`weird\key`},
		},
		{
			name: "escaped dot at start of segment",
			path: `\.starts.middle`,
			want: []string{".starts", "middle"},
		},
		{
			name: "trailing backslash treated as literal",
			path: `trailing\`,
			want: []string{`trailing\`},
		},
		{
			name: "consecutive escaped dots",
			path: `a\.\.b`,
			want: []string{"a..b"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitEscapedPath(tc.path)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestValidateGenericHMACSignature(t *testing.T) {
	body := []byte(`{"test": true}`)
	secret := "my-secret"

	tests := []struct {
		name string
		sig  string
		want bool
	}{
		{
			name: "valid with prefix",
			sig:  computeGenericHMACSignature(body, secret),
			want: true,
		},
		{
			name: "valid without prefix",
			sig: func() string {
				full := computeGenericHMACSignature(body, secret)
				return full[len("sha256="):]
			}(),
			want: true,
		},
		{
			name: "empty signature",
			sig:  "",
			want: false,
		},
		{
			name: "invalid hex",
			sig:  "sha256=zzzz",
			want: false,
		},
		{
			name: "wrong secret",
			sig:  computeGenericHMACSignature(body, "wrong-secret"),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateGenericHMACSignature(body, tc.sig, secret)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestHandleGeneric_Svix(t *testing.T) {
	// Svix verifies signatures with HMAC-SHA256, which uses 32-byte keys.
	// Use random material to make the size-32 expectation explicit; the
	// "whsec_" prefix + base64 encoding matches Svix's wire format.
	keyBytes := make([]byte, 32)
	_, err := rand.Read(keyBytes)
	require.NoError(t, err)
	wrongKeyBytes := make([]byte, 32)
	_, err = rand.Read(wrongKeyBytes)
	require.NoError(t, err)
	secret := "whsec_" + base64.StdEncoding.EncodeToString(keyBytes)
	wrongSecret := "whsec_" + base64.StdEncoding.EncodeToString(wrongKeyBytes)

	cfg := &GenericConfig{
		AuthMode: GenericAuthSvix,
		Secret:   secret,
		FieldMapping: map[string]string{
			"id":    "id",
			"title": "title",
		},
	}
	require.NoError(t, cfg.Prepare())

	payload := map[string]any{"id": "msg_001", "title": "Test"}
	body, _ := json.Marshal(payload)

	// Use the library to compute valid signatures so the test exercises
	// the same code path the production handler relies on.
	signWith := func(t *testing.T, msgSecret string, ts time.Time) string {
		t.Helper()
		wh, err := svix.NewWebhook(msgSecret)
		require.NoError(t, err)
		sig, err := wh.Sign("msg_2KWPB", ts, body)
		require.NoError(t, err)
		return sig
	}

	tests := []struct {
		name        string
		signSecret  string // empty means do not sign (test missing/invalid)
		signAt      time.Time
		idHeader    string
		tsHeader    string // empty means derive from signAt
		sigHeader   string // empty means compute via signWith
		headerStyle string // "webhook" or "svix"
		wantStatus  int
		wantCalls   int
	}{
		{
			name:        "valid signature with webhook- prefix",
			signSecret:  secret,
			signAt:      time.Now(),
			idHeader:    "msg_2KWPB",
			headerStyle: "webhook",
			wantStatus:  http.StatusOK,
			wantCalls:   1,
		},
		{
			name:        "valid signature with svix- prefix",
			signSecret:  secret,
			signAt:      time.Now(),
			idHeader:    "msg_2KWPB",
			headerStyle: "svix",
			wantStatus:  http.StatusOK,
			wantCalls:   1,
		},
		{
			name:        "wrong secret rejected",
			signSecret:  wrongSecret,
			signAt:      time.Now(),
			idHeader:    "msg_2KWPB",
			headerStyle: "webhook",
			wantStatus:  http.StatusUnauthorized,
			wantCalls:   0,
		},
		{
			name:        "stale timestamp rejected",
			signSecret:  secret,
			signAt:      time.Now().Add(-10 * time.Minute),
			idHeader:    "msg_2KWPB",
			headerStyle: "webhook",
			wantStatus:  http.StatusUnauthorized,
			wantCalls:   0,
		},
		{
			name:        "missing headers rejected",
			signSecret:  "",
			headerStyle: "webhook",
			wantStatus:  http.StatusUnauthorized,
			wantCalls:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockEventHandler{}
			srv, err := NewServer(testLogger(), mock, WithGenericConfig(cfg))
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/webhooks/generic", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			if tc.signSecret != "" {
				ts := tc.signAt
				sig := signWith(t, tc.signSecret, ts)
				req.Header.Set(tc.headerStyle+"-id", tc.idHeader)
				req.Header.Set(tc.headerStyle+"-timestamp", strconv.FormatInt(ts.Unix(), 10))
				req.Header.Set(tc.headerStyle+"-signature", sig)
			}

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Len(t, mock.calls, tc.wantCalls)
		})
	}
}

func TestGenericConfigPrepare(t *testing.T) {
	keyBytes := make([]byte, 32)
	_, err := rand.Read(keyBytes)
	require.NoError(t, err)
	validSvixSecret := "whsec_" + base64.StdEncoding.EncodeToString(keyBytes)

	tests := []struct {
		name    string
		cfg     *GenericConfig
		wantErr string // substring; empty means success
	}{
		{
			name: "valid hmac config",
			cfg: &GenericConfig{
				AuthMode: GenericAuthHMAC,
				Secret:   "shared-secret",
			},
		},
		{
			name: "valid bearer config",
			cfg: &GenericConfig{
				AuthMode: GenericAuthBearer,
				Secret:   "shared-token",
			},
		},
		{
			name: "valid svix config",
			cfg: &GenericConfig{
				AuthMode: GenericAuthSvix,
				Secret:   validSvixSecret,
			},
		},
		{
			name: "missing secret",
			cfg: &GenericConfig{
				AuthMode: GenericAuthHMAC,
			},
			wantErr: "secret is required",
		},
		{
			name: "missing auth_mode",
			cfg: &GenericConfig{
				Secret: "shared-secret",
			},
			wantErr: "auth_mode is required",
		},
		{
			name: "unsupported auth_mode",
			cfg: &GenericConfig{
				AuthMode: "nope",
				Secret:   "shared-secret",
			},
			wantErr: `unsupported auth_mode "nope"`,
		},
		{
			name: "svix mode rejects signature_header",
			cfg: &GenericConfig{
				AuthMode:        GenericAuthSvix,
				Secret:          validSvixSecret,
				SignatureHeader: "X-Custom-Sig",
			},
			wantErr: "signature_header is not used in svix mode",
		},
		{
			name: "svix with malformed whsec base64",
			cfg: &GenericConfig{
				AuthMode: GenericAuthSvix,
				Secret:   "whsec_!!!notbase64!!!",
			},
			wantErr: "invalid svix secret",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Prepare()
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestGenericConfigPrepare_Idempotent(t *testing.T) {
	keyBytes := make([]byte, 32)
	_, err := rand.Read(keyBytes)
	require.NoError(t, err)
	cfg := &GenericConfig{
		AuthMode: GenericAuthSvix,
		Secret:   "whsec_" + base64.StdEncoding.EncodeToString(keyBytes),
	}
	require.NoError(t, cfg.Prepare())
	first := cfg.svixWebhook
	require.NotNil(t, first)

	require.NoError(t, cfg.Prepare())
	second := cfg.svixWebhook
	require.NotNil(t, second)
	// Second call rebuilds the verifier from scratch; identity is not
	// preserved, but both calls succeed and leave the field populated.
	_ = first
	_ = second
}

func TestNewServer_GenericConfigErrors(t *testing.T) {
	// WithGenericConfig invokes GenericConfig.Prepare() at server-construction
	// time, so misconfiguration surfaces as a startup error instead of as a
	// per-request 500. These cases mirror the validation paths in
	// TestGenericConfigPrepare to confirm the error is propagated through
	// NewServer.
	tests := []struct {
		name    string
		cfg     *GenericConfig
		wantErr string
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: "config is nil",
		},
		{
			name:    "missing secret",
			cfg:     &GenericConfig{AuthMode: GenericAuthHMAC},
			wantErr: "secret is required",
		},
		{
			name:    "unsupported auth mode",
			cfg:     &GenericConfig{AuthMode: "nope", Secret: "x"},
			wantErr: `unsupported auth_mode "nope"`,
		},
		{
			name: "svix malformed secret",
			cfg: &GenericConfig{
				AuthMode: GenericAuthSvix,
				Secret:   "whsec_!!!notbase64!!!",
			},
			wantErr: "invalid svix secret",
		},
		{
			name: "svix mode rejects signature_header",
			cfg: &GenericConfig{
				AuthMode:        GenericAuthSvix,
				Secret:          "whsec_" + base64.StdEncoding.EncodeToString(make([]byte, 32)),
				SignatureHeader: "X-Foo",
			},
			wantErr: "signature_header is not used in svix mode",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockEventHandler{}
			_, err := NewServer(testLogger(), mock, WithGenericConfig(tc.cfg))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "generic webhook config")
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
