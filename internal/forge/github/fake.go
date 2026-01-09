package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/msuozzo/jj-forge/internal/forge"
)

// Review represents a pull request in the fake implementation.
type Review struct {
	Number    int
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
	reviews       map[int]*Review
	nextNumber    int
	createError   error // Error to return from CreateReview
	mergeError    error // Error to return from MergeReview
	closeError    error // Error to return from CloseReview
	defaultBranch string
}

// NewFakeForge creates a new fake forge for testing.
func NewFakeForge() *FakeForge {
	return &FakeForge{
		reviews:       make(map[int]*Review),
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
	number := f.nextNumber
	f.nextNumber++

	url := fmt.Sprintf("%s/pull/%d", normalizedURI, number)

	review := &Review{
		Number:    number,
		Title:     params.Title,
		Body:      params.Body,
		Head:      params.FromBranch,
		Base:      params.ToBranch,
		Reviewers: params.Reviewers,
		Status:    "open",
		URL:       url,
	}

	f.reviews[number] = review

	return &forge.ReviewCreateResult{
		Number: number,
		URL:    url,
	}, nil
}

// MergeReview marks a fake pull request as merged.
func (f *FakeForge) MergeReview(ctx context.Context, repoURI string, reviewNumber int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.mergeError != nil {
		return f.mergeError
	}

	review, exists := f.reviews[reviewNumber]
	if !exists {
		return fmt.Errorf("review #%d not found", reviewNumber)
	}

	if review.Status != "open" {
		return fmt.Errorf("review #%d is not open (status: %s)", reviewNumber, review.Status)
	}

	review.Status = "merged"
	return nil
}

// CloseReview marks a fake pull request as closed.
func (f *FakeForge) CloseReview(ctx context.Context, repoURI string, reviewNumber int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closeError != nil {
		return f.closeError
	}

	review, exists := f.reviews[reviewNumber]
	if !exists {
		return fmt.Errorf("review #%d not found", reviewNumber)
	}

	review.Status = "closed"
	return nil
}

// FormatID formats a review number into a string ID (e.g. "pr/123").
func (f *FakeForge) FormatID(number int) string {
	return fmt.Sprintf("pr/%d", number)
}

// ParseID parses a string ID (e.g. "pr/123") into a review number.
func (f *FakeForge) ParseID(id string) (int, error) {
	if strings.HasPrefix(id, "pr/") {
		id = strings.TrimPrefix(id, "pr/")
	}
	return strconv.Atoi(id)
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

// GetTestReview returns a review by number (for testing assertions).
func (f *FakeForge) GetTestReview(number int) (*Review, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	review, exists := f.reviews[number]
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
				Number: r.Number,
				URL:    r.URL,
				State:  forge.ReviewState(r.Status),
			}, nil
		}
	}
	return nil, nil
}

// GetReview retrieves details of a specific review.
func (f *FakeForge) GetReview(ctx context.Context, repoURI string, number int) (*forge.ReviewDetails, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	r, exists := f.reviews[number]
	if !exists {
		return nil, fmt.Errorf("review #%d not found", number)
	}
	return &forge.ReviewDetails{
		Number: r.Number,
		URL:    r.URL,
		State:  forge.ReviewState(r.Status),
	}, nil
}

