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

// Forge defines the interface for interacting with code forges.
type Forge interface {
	// CreateReview creates a new code review.
	CreateReview(ctx context.Context, repoURI string, params ReviewCreateParams) (*ReviewCreateResult, error)

	// FormatID formats a review number into a string ID (e.g. "pr/123").
	FormatID(number int) string

	// ParseID parses a string ID (e.g. "pr/123") into a review number.
	ParseID(id string) (int, error)

	// DefaultBranch returns the default branch name of the repository.
	DefaultBranch(ctx context.Context, repoURI string) (string, error)
}
