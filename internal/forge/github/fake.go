package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// Review represents a pull request in the fake implementation.
type Review struct {
	ID        string
	Title     string
	Body      string
	Head      string
	Base      string
	Reviewers []string
	Status    string // One of open,merged,closed
	URL       string
}

// FakeForge implements forge.Forge for testing.
type FakeForge struct {
	mu            sync.Mutex
	reviews       map[string]*Review
	nextNumber    int
	createError   error // Error to return from CreateReview
	mergeError    error // Error to return from MergeReview
	closeError    error // Error to return from CloseReview
	defaultBranch string
}

// NewFakeForge creates a new fake forge for testing.
func NewFakeForge() *FakeForge {
	return &FakeForge{
		reviews:       make(map[string]*Review),
		nextNumber:    1,
		defaultBranch: "main",
	}
}

// CreateReview creates a fake pull request.
func (f *FakeForge) CreateReview(ctx context.Context, repoURI string, params forge.ReviewCreateParams) (*forge.ReviewCreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.createError != nil {
		return nil, f.createError
	}
	// Normalize the repo URI to HTTPS format
	normalizedURI, err := forge.NormalizeRepoURL(repoURI)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URI: %w", err)
	}
	id := strconv.Itoa(f.nextNumber)
	f.nextNumber++

	url := fmt.Sprintf("%s/pull/%s", normalizedURI, id)

	review := &Review{
		ID:        id,
		Title:     params.Title,
		Body:      params.Body,
		Head:      params.FromBranch,
		Base:      params.ToBranch,
		Reviewers: params.Reviewers,
		Status:    "open",
		URL:       url,
	}

	f.reviews[id] = review

	return &forge.ReviewCreateResult{
		ID:  id,
		URL: url,
	}, nil
}

// MergeReview marks a fake pull request as merged.
func (f *FakeForge) MergeReview(ctx context.Context, repoURI string, reviewID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.mergeError != nil {
		return f.mergeError
	}

	review, exists := f.reviews[reviewID]
	if !exists {
		return fmt.Errorf("review #%s not found", reviewID)
	}

	if review.Status != "open" {
		return fmt.Errorf("review #%s is not open (status: %s)", reviewID, review.Status)
	}

	review.Status = "merged"
	return nil
}

// CloseReview marks a fake pull request as closed.
func (f *FakeForge) CloseReview(ctx context.Context, repoURI string, reviewID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closeError != nil {
		return f.closeError
	}

	review, exists := f.reviews[reviewID]
	if !exists {
		return fmt.Errorf("review #%s not found", reviewID)
	}

	review.Status = "closed"
	return nil
}

// FormatID formats a PR number into the stored ID form (e.g. "pr/123").
func (f *FakeForge) FormatID(reviewID string) string {
	return "pr/" + reviewID
}

// ParseID parses a stored ID (e.g. "pr/123") into a PR number string.
func (f *FakeForge) ParseID(id string) (string, error) {
	id = strings.TrimPrefix(id, "pr/")
	number, err := strconv.Atoi(id)
	if err != nil {
		return "", fmt.Errorf("invalid PR number %q: %w", id, err)
	}
	return strconv.Itoa(number), nil
}

// DefaultBranch returns the default branch name.
func (f *FakeForge) DefaultBranch(ctx context.Context, repoURI string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.defaultBranch, nil
}

// SetupRuleset implements forge.Forge.
func (f *FakeForge) SetupRuleset(ctx context.Context, repoURI string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return nil
}

// SetDefaultBranch sets the default branch name.
func (f *FakeForge) SetDefaultBranch(branch string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defaultBranch = branch
}

// GetTestReview returns a review by ID (for testing assertions).
func (f *FakeForge) GetTestReview(reviewID string) (*Review, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	review, exists := f.reviews[reviewID]
	return review, exists
}

// SetCreateError sets an error to be returned from CreateReview.
func (f *FakeForge) SetCreateError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createError = err
}

// SetMergeError sets an error to be returned from MergeReview.
func (f *FakeForge) SetMergeError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mergeError = err
}

// SetCloseError sets an error to be returned from CloseReview.
func (f *FakeForge) SetCloseError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeError = err
}

// ReviewCount returns the number of reviews created (for testing assertions).
func (f *FakeForge) ReviewCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reviews)
}

// FindReview searches for a review by branch name.
func (f *FakeForge) FindReview(ctx context.Context, repoURI, branch string) (*forge.ReviewDetails, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, r := range f.reviews {
		if r.Head == branch {
			return &forge.ReviewDetails{
				ID:    r.ID,
				URL:   r.URL,
				State: forge.ReviewState(r.Status),
			}, nil
		}
	}
	return nil, nil
}

// GetReview retrieves details of a specific review.
func (f *FakeForge) GetReview(ctx context.Context, repoURI string, reviewID string) (*forge.ReviewDetails, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	r, exists := f.reviews[reviewID]
	if !exists {
		return nil, fmt.Errorf("review #%s not found", reviewID)
	}
	return &forge.ReviewDetails{
		ID:    r.ID,
		URL:   r.URL,
		State: forge.ReviewState(r.Status),
		Title: r.Title,
		Body:  r.Body,
	}, nil
}

// UpdateReview updates the body of a review.
func (f *FakeForge) UpdateReview(ctx context.Context, repoURI string, reviewID string, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	r, exists := f.reviews[reviewID]
	if !exists {
		return fmt.Errorf("review #%s not found", reviewID)
	}
	r.Body = body
	return nil
}

// FormatHeadBranch returns the head branch reference for a cross-repo GitHub PR.
func (f *FakeForge) FormatHeadBranch(ctx context.Context, jjClient jj.Client, forkRemote, changeID string) (string, error) {
	repoInfo, err := forge.GetRepoInfo(ctx, jjClient, forkRemote)
	if err != nil {
		return "", fmt.Errorf("failed to get repo info for %s: %w", forkRemote, err)
	}
	return fmt.Sprintf("%s:push-%s", repoInfo.Owner, changeID), nil
}

// NormalizeRepoURL converts a remote URL to GitHub's canonical HTTPS format.
func (f *FakeForge) NormalizeRepoURL(url string) (string, error) {
	return forge.NormalizeRepoURL(url)
}

// SupportsForks returns true because GitHub uses a fork-based workflow.
func (f *FakeForge) SupportsForks() bool {
	return true
}
