package forge

import (
	"context"

	"github.com/msuozzo/jj-forge/internal/jj"
)

// ReviewCreateParams contains parameters for creating a code review.
type ReviewCreateParams struct {
	Title      string   // Review title (typically first line of commit message)
	Body       string   // Review body (typically rest of commit message)
	FromBranch string   // Head branch name (e.g., "push-abc123")
	ToBranch   string   // Base branch name (e.g., "main" or "push-xyz789" for stacked reviews)
	Reviewers  []string // List of reviewer usernames
}

// ReviewCreateResult contains the result of creating a code review.
type ReviewCreateResult struct {
	ID  string // Forge-native review identifier (e.g. "123" for GitHub, a record key for Tangled)
	URL string // URL to the review (e.g., https://github.com/owner/repo/pull/123)
}

// ReviewState represents the state of a code review.
type ReviewState string

const (
	ReviewStateOpen   ReviewState = "open"
	ReviewStateClosed ReviewState = "closed"
	ReviewStateMerged ReviewState = "merged"
)

// ReviewDetails contains details about a code review.
type ReviewDetails struct {
	ID    string
	URL   string
	State ReviewState
	Title string
	Body  string
}

// Forge defines the interface for interacting with code forges.
//
// Reviews are identified by a forge-native string ID. For GitHub and SSM this
// is the decimal PR number. For Tangled it is the pull record key. FormatID
// and ParseID convert between the bare ID and the prefixed form stored in the
// jj config (e.g. "pr/123").
type Forge interface {
	// CreateReview creates a new code review.
	CreateReview(ctx context.Context, repoURI string, params ReviewCreateParams) (*ReviewCreateResult, error)

	// MergeReview merges an open code review whose head is commitID, a full
	// commit ID. Forges that can pin a merge to a commit refuse it when the
	// head has moved.
	MergeReview(ctx context.Context, repoURI string, reviewID string, commitID string) error

	// CloseReview closes a code review without merging.
	CloseReview(ctx context.Context, repoURI string, reviewID string) error

	// FindReview searches for a review by branch name.
	FindReview(ctx context.Context, repoURI, branch string) (*ReviewDetails, error)

	// GetReview retrieves details of a specific review.
	GetReview(ctx context.Context, repoURI string, reviewID string) (*ReviewDetails, error)

	// FormatID formats a review ID into the string stored in config (e.g. "pr/123").
	FormatID(reviewID string) string

	// ParseID parses a stored ID (e.g. "pr/123") into the bare review ID.
	ParseID(id string) (string, error)

	// DefaultBranch returns the default branch name of the repository.
	DefaultBranch(ctx context.Context, repoURI string) (string, error)

	// UpdateReview updates the body of an existing code review.
	UpdateReview(ctx context.Context, repoURI string, reviewID string, body string) error

	// SetupRuleset configures a ruleset on the forge to prevent merging commits with forge-parent.
	SetupRuleset(ctx context.Context, repoURI string) error

	// FormatHeadBranch returns the head/source branch reference for creating a review.
	// GitHub: "owner:push-{changeID}" (fork-qualified ref)
	// SSM/Tangled: "push-{changeID}" (bare branch name)
	FormatHeadBranch(ctx context.Context, jjClient jj.Client, forkRemote, changeID string) (string, error)

	// NormalizeRepoURL converts a remote URL to this forge's canonical format.
	NormalizeRepoURL(url string) (string, error)

	// SupportsForks returns whether the forge uses a fork-based workflow.
	SupportsForks() bool
}

// ReviewSyncer is implemented by forges whose reviews capture a snapshot of
// the change content at submission time (e.g. Tangled's patch rounds) rather
// than tracking the head branch. After a change's branch has been pushed,
// SyncReview refreshes the review so that it reflects the pushed content.
//
// Forges that track branches directly (GitHub, SSM) do not implement this.
type ReviewSyncer interface {
	// SyncReview submits the current content of the review's head branch.
	SyncReview(ctx context.Context, repoURI string, reviewID string) error
}
