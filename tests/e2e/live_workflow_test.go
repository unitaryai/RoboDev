//go:build live

package e2e

// Live E2E workflow tests exercise the full RoboDev pipeline against real
// external services: Shortcut ticketing, GitLab SCM, and the Claude Code
// engine. They require the live controller to be running in the kind cluster
// with valid secrets.
//
// # Prerequisites
//
//   - kind cluster running with `make live-up` or equivalent
//   - robodev-shortcut-token, robodev-scm-token, and robodev-anthropic-key
//     K8s Secrets present in the live namespace
//   - kubeconfig pointing at the kind cluster (kubectl context kind-robodev)
//
// # Running
//
//	make e2e-live-test
//
// or directly:
//
//	go test -tags=live -v -timeout=1200s ./tests/e2e/ -run TestLive
//
// # Environment variables
//
//	ROBODEV_LIVE_NAMESPACE      K8s namespace where the controller runs (default: "robodev")
//	SHORTCUT_TEST_REPO_URL      GitLab repo URL for agent tasks (default: customer1-common test repo)
//	SHORTCUT_READY_STATE        Trigger workflow state name (default: "Ready for Development")
//	SHORTCUT_OWNER_MENTION      Shortcut mention name to assign stories to (default: "robodev")

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestLiveHappyPath validates the complete end-to-end pipeline against real
// external services:
//
//  1. Creates a Shortcut story in "Ready for Development" state, assigned to
//     @robodev, with the test GitLab repo as an external link.
//  2. Waits for the live controller to poll and pick up the story (up to 30s).
//  3. Waits for the Claude Code agent K8s Job to complete successfully.
//  4. Asserts that Shortcut transitions the story to a done-type state and
//     posts a "Task completed successfully" comment.
//
// The task description is deliberately minimal so any non-empty repo will
// satisfy it: append a single comment line to README.md.
func TestLiveHappyPath(t *testing.T) {
	repoURL := liveTestRepoURL()
	sc := newShortcutTestClient(t)

	title := fmt.Sprintf("RoboDev E2E: append comment to README — %d", time.Now().Unix())
	description := `Append exactly one line to the bottom of README.md:

` + "```" + `
<!-- robodev-e2e-test -->
` + "```" + `

Do not modify any other files. Open a merge request with the change.`

	storyID := sc.createStory(t, title, description, repoURL)
	// Always delete the test story on exit regardless of outcome.
	// If a K8s Job is still running when cleanup fires, the controller will
	// call MarkComplete on the now-deleted story and log a 404 — this is
	// harmless.
	t.Cleanup(func() { sc.deleteStory(t, storyID) })

	// The controller polls every 30 s; allow 15 minutes for the full cycle:
	// poll pickup → K8s Job scheduling → git clone → Claude Code run → MR →
	// MarkComplete → Shortcut state transition.
	sc.waitForStoryDone(t, storyID, 15*time.Minute)

	comments := sc.storyComments(t, storyID)
	assert.True(t,
		hasCommentContaining(comments, "Task completed successfully"),
		"expected a completion comment on story #%s; got: %v", storyID, comments,
	)
}

// TestLiveFailureDiagnosis validates the failure path: a story whose repo URL
// is inaccessible causes the agent's git clone to fail, exhausting all retries
// and triggering MarkFailed on the ticketing backend.
//
// Asserts:
//   - Story is labelled "robodev-failed" within the timeout.
//   - At least one comment contains a failure reason.
//
// No SHORTCUT_TEST_REPO_URL is required — the invalid URL is intentional.
func TestLiveFailureDiagnosis(t *testing.T) {
	sc := newShortcutTestClient(t)

	title := fmt.Sprintf("RoboDev E2E: failure/diagnosis test — %d", time.Now().Unix())
	description := "Fix the authentication bug in the login module."

	// This repo does not exist; the git clone inside the agent container will
	// fail immediately, triggering the retry → failure → diagnosis path.
	invalidRepoURL := "https://gitlab.com/robodev-e2e-nonexistent/repo-does-not-exist"

	storyID := sc.createStory(t, title, description, invalidRepoURL)
	t.Cleanup(func() { sc.deleteStory(t, storyID) })

	// Clone failure + one retry takes ~2 minutes; allow 10 to be safe.
	sc.waitForStoryFailed(t, storyID, 10*time.Minute)

	comments := sc.storyComments(t, storyID)
	assert.True(t,
		hasCommentContaining(comments, "failed") || hasCommentContaining(comments, "error"),
		"expected a failure reason comment on story #%s; got: %v", storyID, comments,
	)
}
