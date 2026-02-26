package github

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/robodev-inc/robodev/pkg/engine"
	"github.com/robodev-inc/robodev/pkg/plugin/ticketing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestGitHubBackend_Name(t *testing.T) {
	b := NewGitHubBackend("owner", "repo", nil, "tok", testLogger())
	assert.Equal(t, "github", b.Name())
}

func TestGitHubBackend_InterfaceVersion(t *testing.T) {
	b := NewGitHubBackend("owner", "repo", nil, "tok", testLogger())
	assert.Equal(t, ticketing.InterfaceVersion, b.InterfaceVersion())
}

func TestGitHubBackend_PollReadyTickets(t *testing.T) {
	issues := []ghIssue{
		{
			Number:  42,
			Title:   "Fix login bug",
			Body:    "The login page crashes",
			HTMLURL: "https://github.com/owner/repo/issues/42",
			Labels:  []ghLabel{{Name: "robodev"}, {Name: "bug"}},
		},
		{
			Number:  99,
			Title:   "Refactor auth",
			Body:    "Clean up the auth module",
			HTMLURL: "https://github.com/owner/repo/issues/99",
			Labels:  []ghLabel{{Name: "robodev"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/repos/owner/repo/issues")
		assert.Equal(t, "open", r.URL.Query().Get("state"))
		assert.Equal(t, "robodev", r.URL.Query().Get("labels"))
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(issues)
	}))
	defer srv.Close()

	b := NewGitHubBackend("owner", "repo", []string{"robodev"}, "test-token", testLogger(),
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
	)

	tickets, err := b.PollReadyTickets(context.Background())
	require.NoError(t, err)
	require.Len(t, tickets, 2)

	assert.Equal(t, "42", tickets[0].ID)
	assert.Equal(t, "Fix login bug", tickets[0].Title)
	assert.Equal(t, "The login page crashes", tickets[0].Description)
	assert.Equal(t, "issue", tickets[0].TicketType)
	assert.Equal(t, []string{"robodev", "bug"}, tickets[0].Labels)
	assert.Equal(t, "https://github.com/owner/repo", tickets[0].RepoURL)

	assert.Equal(t, "99", tickets[1].ID)
}

func TestGitHubBackend_PollReadyTickets_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]ghIssue{})
	}))
	defer srv.Close()

	b := NewGitHubBackend("o", "r", []string{"robodev"}, "tok", testLogger(), WithBaseURL(srv.URL))
	tickets, err := b.PollReadyTickets(context.Background())
	require.NoError(t, err)
	assert.Empty(t, tickets)
}

func TestGitHubBackend_PollReadyTickets_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	b := NewGitHubBackend("o", "r", []string{"robodev"}, "tok", testLogger(), WithBaseURL(srv.URL))
	_, err := b.PollReadyTickets(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status 500")
}

func TestGitHubBackend_MarkInProgress(t *testing.T) {
	var calls []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)

		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/labels"):
			var payload map[string][]string
			json.NewDecoder(r.Body).Decode(&payload)
			assert.Equal(t, []string{"in-progress"}, payload["labels"])
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/labels/robodev"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	b := NewGitHubBackend("owner", "repo", []string{"robodev"}, "tok", testLogger(), WithBaseURL(srv.URL))
	err := b.MarkInProgress(context.Background(), "42")
	require.NoError(t, err)

	assert.Contains(t, calls, "POST /repos/owner/repo/issues/42/labels")
	assert.Contains(t, calls, "DELETE /repos/owner/repo/issues/42/labels/robodev")
}

func TestGitHubBackend_MarkComplete(t *testing.T) {
	var commentBody string
	var patchState string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments"):
			var payload map[string]string
			json.NewDecoder(r.Body).Decode(&payload)
			commentBody = payload["body"]
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPatch:
			var payload map[string]string
			json.NewDecoder(r.Body).Decode(&payload)
			patchState = payload["state"]
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	b := NewGitHubBackend("owner", "repo", nil, "tok", testLogger(), WithBaseURL(srv.URL))

	result := engine.TaskResult{
		Success:         true,
		Summary:         "Fixed the bug",
		MergeRequestURL: "https://github.com/owner/repo/pull/10",
	}
	err := b.MarkComplete(context.Background(), "42", result)
	require.NoError(t, err)

	assert.Contains(t, commentBody, "Fixed the bug")
	assert.Contains(t, commentBody, "https://github.com/owner/repo/pull/10")
	assert.Equal(t, "closed", patchState)
}

func TestGitHubBackend_MarkFailed(t *testing.T) {
	var labelPayload map[string][]string
	var commentPayload map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/labels"):
			json.NewDecoder(r.Body).Decode(&labelPayload)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments"):
			json.NewDecoder(r.Body).Decode(&commentPayload)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	b := NewGitHubBackend("owner", "repo", nil, "tok", testLogger(), WithBaseURL(srv.URL))
	err := b.MarkFailed(context.Background(), "7", "timeout exceeded")
	require.NoError(t, err)

	assert.Equal(t, []string{"robodev-failed"}, labelPayload["labels"])
	assert.Contains(t, commentPayload["body"], "timeout exceeded")
}

func TestGitHubBackend_AddComment(t *testing.T) {
	var receivedBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/owner/repo/issues/5/comments", r.URL.Path)
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	b := NewGitHubBackend("owner", "repo", nil, "tok", testLogger(), WithBaseURL(srv.URL))
	err := b.AddComment(context.Background(), "5", "progress update")
	require.NoError(t, err)
	assert.Equal(t, "progress update", receivedBody["body"])
}

func TestGitHubBackend_WithBaseURL(t *testing.T) {
	b := NewGitHubBackend("o", "r", nil, "tok", testLogger(), WithBaseURL("https://ghe.example.com/api/v3/"))
	assert.Equal(t, "https://ghe.example.com/api/v3", b.baseURL)
}
