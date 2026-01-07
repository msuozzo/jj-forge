package github

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/msuozzo/jj-forge/internal/forge"
)

func TestCreateReview_Success(t *testing.T) {
	expectedArgs := []string{
		"pr", "create",
		"--repo", "https://github.com/owner/repo",
		"--title", "Test PR",
		"--body", "Test body",
		"--head", "push-abc123",
		"--base", "main",
		"--reviewer", "reviewer1",
	}

	executor := func(ctx context.Context, args ...string) (string, error) {
		if diff := cmp.Diff(args, expectedArgs); diff != "" {
			t.Errorf("unexpected args:\ngot:  %v\nwant: %v", args, expectedArgs)
		}
		return "https://github.com/owner/repo/pull/42\n", nil
	}

	client := NewClientWithExecutor("/path/to/gh", executor)

	result, err := client.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "Test PR",
		Body:       "Test body",
		FromBranch: "push-abc123",
		ToBranch:   "main",
		Reviewers:  []string{"reviewer1"},
	})

	if err != nil {
		t.Fatalf("CreateReview failed: %v", err)
	}

	if result.Number != 42 {
		t.Errorf("expected PR number 42, got %d", result.Number)
	}

	if result.URL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("expected URL https://github.com/owner/repo/pull/42, got %s", result.URL)
	}
}

func TestCreateReview_MultipleReviewers(t *testing.T) {
	expectedArgs := []string{
		"pr", "create",
		"--repo", "https://github.com/owner/repo",
		"--title", "Title",
		"--body", "Body",
		"--head", "push-abc",
		"--base", "main",
		"--reviewer", "user1",
		"--reviewer", "user2",
	}

	executor := func(ctx context.Context, args ...string) (string, error) {
		if diff := cmp.Diff(args, expectedArgs); diff != "" {
			t.Errorf("unexpected args:\ngot:  %v\nwant: %v", args, expectedArgs)
		}
		return "https://github.com/owner/repo/pull/1", nil
	}

	client := NewClientWithExecutor("/gh", executor)

	_, err := client.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "Title",
		Body:       "Body",
		FromBranch: "push-abc",
		ToBranch:   "main",
		Reviewers:  []string{"user1", "user2"},
	})

	if err != nil {
		t.Fatalf("CreateReview failed: %v", err)
	}
}

func TestCreateReview_NoReviewers(t *testing.T) {
	executor := func(ctx context.Context, args ...string) (string, error) {
		// Verify no --reviewer flags present
		for i, arg := range args {
			if arg == "--reviewer" {
				t.Errorf("unexpected --reviewer at position %d", i)
			}
		}
		return "https://github.com/owner/repo/pull/1", nil
	}

	client := NewClientWithExecutor("/gh", executor)

	_, err := client.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "Title",
		Body:       "Body",
		FromBranch: "push-abc",
		ToBranch:   "main",
		Reviewers:  []string{}, // No reviewers
	})

	if err != nil {
		t.Fatalf("CreateReview failed: %v", err)
	}
}

func TestCreateReview_ExecutorError(t *testing.T) {
	expectedErr := errors.New("gh command failed")
	executor := func(ctx context.Context, args ...string) (string, error) {
		return "", expectedErr
	}

	client := NewClientWithExecutor("/gh", executor)

	_, err := client.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "Title",
		Body:       "Body",
		FromBranch: "push-abc",
		ToBranch:   "main",
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to create PR") {
		t.Errorf("expected 'failed to create PR' in error, got: %v", err)
	}
}

func TestCreateReview_InvalidOutput(t *testing.T) {
	executor := func(ctx context.Context, args ...string) (string, error) {
		return "invalid-url-format", nil
	}

	client := NewClientWithExecutor("/gh", executor)

	_, err := client.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "Title",
		Body:       "Body",
		FromBranch: "push-abc",
		ToBranch:   "main",
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to parse PR number from URL") {
		t.Errorf("expected 'failed to parse PR number from URL' in error, got: %v", err)
	}
}
