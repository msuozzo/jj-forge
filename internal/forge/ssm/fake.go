package ssm

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// Review represents a pull request in the fake SSM implementation.
type Review struct {
	Number int
	Title  string
	Body   string
	Head   string
	Base   string
	Status string // One of open,merged,closed
	URL    string
}

// FakeForge implements forge.Forge for testing SSM flows.
type FakeForge struct {
	mu            sync.Mutex
	reviews       map[int]*Review
	nextNumber    int
	createError   error
	mergeError    error
	closeError    error
	defaultBranch string
}

// NewFakeForge creates a new fake SSM forge for testing.
func NewFakeForge() *FakeForge {
	return &FakeForge{
		reviews:       make(map[int]*Review),
		nextNumber:    1,
		defaultBranch: "main",
	}
}

// CreateReview creates a fake pull request.
func (f *FakeForge) CreateReview(_ context.Context, repoURI string, params forge.ReviewCreateParams) (*forge.ReviewCreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.createError != nil {
		return nil, f.createError
	}
	number := f.nextNumber
	f.nextNumber++

	url := fmt.Sprintf("%s/pulls/%d", repoURI, number)

	review := &Review{
		Number: number,
		Title:  params.Title,
		Body:   params.Body,
		Head:   params.FromBranch,
		Base:   params.ToBranch,
		Status: "open",
		URL:    url,
	}
	f.reviews[number] = review

	return &forge.ReviewCreateResult{
		Number: number,
		URL:    url,
	}, nil
}

// MergeReview marks a fake pull request as merged.
func (f *FakeForge) MergeReview(_ context.Context, _ string, reviewNumber int) error {
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
func (f *FakeForge) CloseReview(_ context.Context, _ string, reviewNumber int) error {
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

// FindReview searches for a review by branch name.
func (f *FakeForge) FindReview(_ context.Context, _ string, branch string) (*forge.ReviewDetails, error) {
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
func (f *FakeForge) GetReview(_ context.Context, _ string, number int) (*forge.ReviewDetails, error) {
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
		Title:  r.Title,
		Body:   r.Body,
	}, nil
}

// UpdateReview updates the body of a review.
func (f *FakeForge) UpdateReview(_ context.Context, _ string, reviewNumber int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	r, exists := f.reviews[reviewNumber]
	if !exists {
		return fmt.Errorf("review #%d not found", reviewNumber)
	}
	r.Body = body
	return nil
}

// DefaultBranch returns the default branch name.
func (f *FakeForge) DefaultBranch(_ context.Context, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.defaultBranch, nil
}

// SetupRuleset implements forge.Forge (no-op for fake).
func (f *FakeForge) SetupRuleset(_ context.Context, _ string) error {
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

// FormatHeadBranch returns the head branch for SSM (no owner prefix).
func (f *FakeForge) FormatHeadBranch(_ context.Context, _ jj.Client, _, changeID string) (string, error) {
	return fmt.Sprintf("push-%s", changeID), nil
}

// NormalizeRepoURL normalizes an SSM URL.
func (f *FakeForge) NormalizeRepoURL(url string) (string, error) {
	return NormalizeSSMURL(url)
}

// SupportsForks returns false because SSM does not use forks.
func (f *FakeForge) SupportsForks() bool {
	return false
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

// ReviewCount returns the number of reviews created.
func (f *FakeForge) ReviewCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reviews)
}
