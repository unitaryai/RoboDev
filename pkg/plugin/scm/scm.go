// Package scm defines the SCMBackend interface for interacting with
// source code management platforms (GitHub, GitLab, etc). The SCM backend
// handles branch creation, pull/merge request management, and repository
// operations.
package scm

import (
	"context"
)

// InterfaceVersion is the current version of the SCMBackend interface.
const InterfaceVersion = 1

// PullRequest represents a pull request or merge request created by an agent.
type PullRequest struct {
	ID          string `json:"id"`
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	BranchName  string `json:"branch_name"`
	BaseBranch  string `json:"base_branch"`
	State       string `json:"state"` // "open", "closed", "merged"
}

// CreatePullRequestInput contains the parameters for creating a new
// pull request or merge request.
type CreatePullRequestInput struct {
	RepoURL     string `json:"repo_url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	BranchName  string `json:"branch_name"`
	BaseBranch  string `json:"base_branch"`
}

// Backend is the interface that SCM backends must implement.
// It provides operations for branch and pull request management.
type Backend interface {
	// CreateBranch creates a new branch in the repository from the
	// specified base branch or default branch if base is empty.
	CreateBranch(ctx context.Context, repoURL string, branchName string, baseBranch string) error

	// CreatePullRequest creates a new pull/merge request and returns
	// the created PR details including its URL.
	CreatePullRequest(ctx context.Context, input CreatePullRequestInput) (*PullRequest, error)

	// GetPullRequestStatus retrieves the current status of a pull request
	// by its URL. This is used by the controller to check CI status and
	// review state.
	GetPullRequestStatus(ctx context.Context, prURL string) (*PullRequest, error)

	// Name returns the unique identifier for this backend (e.g. "github", "gitlab").
	Name() string

	// InterfaceVersion returns the version of the SCMBackend interface
	// that this backend implements.
	InterfaceVersion() int
}
