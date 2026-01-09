package forge

import "context"

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
	Number int    // Review number (e.g., PR number for GitHub)
	URL    string // URL to the review (e.g., https://github.com/owner/repo/pull/123)
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
	Number int
	URL    string
	State  ReviewState
}
// Forge defines the interface for interacting with code forges.
type Forge interface {
	// CreateReview creates a new code review.
	CreateReview(ctx context.Context, repoURI string, params ReviewCreateParams) (*ReviewCreateResult, error)

	// MergeReview merges an open code review.
	MergeReview(ctx context.Context, repoURI string, reviewNumber int) error

	// CloseReview closes a code review without merging.
	CloseReview(ctx context.Context, repoURI string, reviewNumber int) error

	// FindReview searches for a review by branch name.
	FindReview(ctx context.Context, repoURI, branch string) (*ReviewDetails, error)

	// GetReview retrieves details of a specific review.
	GetReview(ctx context.Context, repoURI string, number int) (*ReviewDetails, error)

	// FormatID formats a review number into a string ID (e.g. "pr/123").
	FormatID(number int) string

	// ParseID parses a string ID (e.g. "pr/123") into a review number.
	ParseID(id string) (int, error)

	// DefaultBranch returns the default branch name of the repository.
	DefaultBranch(ctx context.Context, repoURI string) (string, error)

	// SetupRuleset configures a ruleset on the forge to prevent merging commits with forge-parent.
	SetupRuleset(ctx context.Context, repoURI string) error
}
