//go:build e2e

package e2e

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebhookGitHubValidSignature verifies that a well-formed GitHub issue
// event with a valid HMAC-SHA256 signature is accepted with HTTP 200.
func TestWebhookGitHubValidSignature(t *testing.T) {
	ns := testNamespace()
	endpoint, cleanup := portForwardService(t, ns, webhookServiceName(), 8081)
	defer cleanup()

	payload := []byte(`{"action":"opened","issue":{"number":99,"title":"Test webhook","body":"Testing","html_url":"https://github.com/org/repo/issues/99","labels":[]},"repository":{"clone_url":"https://github.com/org/repo.git","html_url":"https://github.com/org/repo"}}`)
	sig := computeGitHubSignature(payload, webhookSecret("github"))

	req, err := http.NewRequest("POST", endpoint+"/webhooks/github", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", sig)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestWebhookGitHubInvalidSignature verifies that a request with a wrong HMAC
// signature is rejected with HTTP 401.
func TestWebhookGitHubInvalidSignature(t *testing.T) {
	ns := testNamespace()
	endpoint, cleanup := portForwardService(t, ns, webhookServiceName(), 8081)
	defer cleanup()

	payload := []byte(`{"action":"opened","issue":{"number":1,"title":"Bad sig","body":"test","html_url":"https://github.com/org/repo/issues/1","labels":[]},"repository":{"clone_url":"https://github.com/org/repo.git","html_url":"https://github.com/org/repo"}}`)

	req, err := http.NewRequest("POST", endpoint+"/webhooks/github", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeefdeadbeefdeadbeefdeadbeef")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestWebhookGitHubMissingSignature verifies that a request with no
// X-Hub-Signature-256 header is rejected with HTTP 401.
func TestWebhookGitHubMissingSignature(t *testing.T) {
	ns := testNamespace()
	endpoint, cleanup := portForwardService(t, ns, webhookServiceName(), 8081)
	defer cleanup()

	payload := []byte(`{"action":"opened","issue":{"number":2,"title":"No sig","body":"test","html_url":"https://github.com/org/repo/issues/2","labels":[]},"repository":{"clone_url":"https://github.com/org/repo.git","html_url":"https://github.com/org/repo"}}`)

	req, err := http.NewRequest("POST", endpoint+"/webhooks/github", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issues")
	// Deliberately omit X-Hub-Signature-256.

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestWebhookGitHubNonIssueEvent verifies that non-issue events (e.g. push)
// are accepted with HTTP 200 and silently ignored.
func TestWebhookGitHubNonIssueEvent(t *testing.T) {
	ns := testNamespace()
	endpoint, cleanup := portForwardService(t, ns, webhookServiceName(), 8081)
	defer cleanup()

	payload := []byte(`{"ref":"refs/heads/main","repository":{"clone_url":"https://github.com/org/repo.git","html_url":"https://github.com/org/repo"}}`)
	sig := computeGitHubSignature(payload, webhookSecret("github"))

	req, err := http.NewRequest("POST", endpoint+"/webhooks/github", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", sig)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestWebhookGitHubClosedAction verifies that a closed-issue event is
// accepted with HTTP 200 (the controller should simply skip it).
func TestWebhookGitHubClosedAction(t *testing.T) {
	ns := testNamespace()
	endpoint, cleanup := portForwardService(t, ns, webhookServiceName(), 8081)
	defer cleanup()

	payload := []byte(`{"action":"closed","issue":{"number":5,"title":"Done","body":"resolved","html_url":"https://github.com/org/repo/issues/5","labels":[]},"repository":{"clone_url":"https://github.com/org/repo.git","html_url":"https://github.com/org/repo"}}`)
	sig := computeGitHubSignature(payload, webhookSecret("github"))

	req, err := http.NewRequest("POST", endpoint+"/webhooks/github", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", sig)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestWebhookHealthz verifies that the webhook server exposes a /healthz
// endpoint that returns HTTP 200.
func TestWebhookHealthz(t *testing.T) {
	ns := testNamespace()
	endpoint, cleanup := portForwardService(t, ns, webhookServiceName(), 8081)
	defer cleanup()

	resp, err := http.Get(endpoint + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
