package tangled

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// Review represents a pull request in the fake Tangled implementation.
type Review struct {
	ID     string // Record key
	Title  string
	Body   string
	Head   string
	Base   string
	Status string // One of open,merged,closed
	URL    string
	Rounds int // Number of patch rounds submitted (1 after creation)
}

// FakeForge implements forge.Forge and forge.ReviewSyncer for testing Tangled flows.
type FakeForge struct {
	mu            sync.Mutex
	reviews       map[string]*Review
	nextNumber    int
	createError   error
	mergeError    error
	closeError    error
	syncError     error
	defaultBranch string
	synced        []string // Review IDs passed to SyncReview, in order
}

// NewFakeForge creates a new fake Tangled forge for testing.
func NewFakeForge() *FakeForge {
	return &FakeForge{
		reviews:       make(map[string]*Review),
		nextNumber:    1,
		defaultBranch: "main",
	}
}

// fakeRkey produces a deterministic record-key-shaped ID.
func fakeRkey(n int) string {
	return fmt.Sprintf("3rkey%08d", n)
}

// CreateReview creates a fake pull request.
func (f *FakeForge) CreateReview(_ context.Context, repoURI string, params forge.ReviewCreateParams) (*forge.ReviewCreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.createError != nil {
		return nil, f.createError
	}
	ref, err := ParseURL(repoURI)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URI: %w", err)
	}
	id := fakeRkey(f.nextNumber)
	f.nextNumber++

	review := &Review{
		ID:     id,
		Title:  params.Title,
		Body:   params.Body,
		Head:   params.FromBranch,
		Base:   params.ToBranch,
		Status: "open",
		URL:    ref.PullsURL(),
		Rounds: 1,
	}
	f.reviews[id] = review

	return &forge.ReviewCreateResult{
		ID:  id,
		URL: review.URL,
	}, nil
}

// MergeReview marks a fake pull request as merged.
func (f *FakeForge) MergeReview(_ context.Context, _ string, reviewID string, _ string) error {
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
func (f *FakeForge) CloseReview(_ context.Context, _ string, reviewID string) error {
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

// FindReview searches for a review by source branch name.
func (f *FakeForge) FindReview(_ context.Context, _ string, branch string) (*forge.ReviewDetails, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, r := range f.reviews {
		if r.Head == branch {
			return &forge.ReviewDetails{
				ID:    r.ID,
				URL:   r.URL,
				State: forge.ReviewState(r.Status),
				Title: r.Title,
			}, nil
		}
	}
	return nil, nil
}

// GetReview retrieves details of a specific review.
func (f *FakeForge) GetReview(_ context.Context, _ string, reviewID string) (*forge.ReviewDetails, error) {
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
func (f *FakeForge) UpdateReview(_ context.Context, _ string, reviewID string, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	r, exists := f.reviews[reviewID]
	if !exists {
		return fmt.Errorf("review #%s not found", reviewID)
	}
	r.Body = body
	return nil
}

// SyncReview records a new patch round for the review.
func (f *FakeForge) SyncReview(_ context.Context, _ string, reviewID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.syncError != nil {
		return f.syncError
	}
	r, exists := f.reviews[reviewID]
	if !exists {
		return fmt.Errorf("review #%s not found", reviewID)
	}
	r.Rounds++
	f.synced = append(f.synced, reviewID)
	return nil
}

// DefaultBranch returns the default branch name.
func (f *FakeForge) DefaultBranch(_ context.Context, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.defaultBranch, nil
}

// SetupRuleset mirrors the real client: unsupported on Tangled.
func (f *FakeForge) SetupRuleset(_ context.Context, _ string) error {
	return fmt.Errorf("Tangled does not support branch rulesets")
}

// FormatID formats a record key into the stored ID form (e.g. "pr/3mujbgpmfg422").
func (f *FakeForge) FormatID(reviewID string) string {
	return "pr/" + reviewID
}

// ParseID parses a stored ID (e.g. "pr/3mujbgpmfg422") into a record key.
func (f *FakeForge) ParseID(id string) (string, error) {
	id = strings.TrimPrefix(id, "pr/")
	if !rkeyRegex.MatchString(id) {
		return "", fmt.Errorf("invalid Tangled pull record key %q", id)
	}
	return id, nil
}

// FormatHeadBranch returns the head branch for Tangled (no owner prefix).
func (f *FakeForge) FormatHeadBranch(_ context.Context, _ jj.Client, _, changeID string) (string, error) {
	return fmt.Sprintf("push-%s", changeID), nil
}

// NormalizeRepoURL normalizes a Tangled URL.
func (f *FakeForge) NormalizeRepoURL(url string) (string, error) {
	return NormalizeURL(url)
}

// SupportsForks returns false because Tangled reviews are branch-based.
func (f *FakeForge) SupportsForks() bool {
	return false
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

// SyncedIDs returns the review IDs that were synced, in call order.
func (f *FakeForge) SyncedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.synced...)
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

// SetSyncError sets an error to be returned from SyncReview.
func (f *FakeForge) SetSyncError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncError = err
}

// ReviewCount returns the number of reviews created.
func (f *FakeForge) ReviewCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reviews)
}
