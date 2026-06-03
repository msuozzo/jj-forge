package github

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/msuozzo/jj-forge/internal/cmd"
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

	executor := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		args = args[1:] // strip binary name
		if diff := cmp.Diff(args, expectedArgs); diff != "" {
			t.Errorf("unexpected args:\ngot:  %v\nwant: %v", args, expectedArgs)
		}
		return &cmd.Result{Stdout: "https://github.com/owner/repo/pull/42\n"}, nil
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

	executor := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		args = args[1:] // strip binary name
		if diff := cmp.Diff(args, expectedArgs); diff != "" {
			t.Errorf("unexpected args:\ngot:  %v\nwant: %v", args, expectedArgs)
		}
		return &cmd.Result{Stdout: "https://github.com/owner/repo/pull/1"}, nil
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
	executor := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		args = args[1:] // strip binary name
		// Verify no --reviewer flags present
		for i, arg := range args {
			if arg == "--reviewer" {
				t.Errorf("unexpected --reviewer at position %d", i)
			}
		}
		return &cmd.Result{Stdout: "https://github.com/owner/repo/pull/1"}, nil
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
	executor := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		return nil, expectedErr
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
	executor := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		return &cmd.Result{Stdout: "invalid-url-format"}, nil
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

func TestMergeReview_Success(t *testing.T) {
	expectedArgs := []string{
		"pr", "merge",
		"42",
		"--repo", "https://github.com/owner/repo",
		"--squash",
	}

	executor := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		args = args[1:] // strip binary name
		if diff := cmp.Diff(args, expectedArgs); diff != "" {
			t.Errorf("unexpected args:\ngot:  %v\nwant: %v", args, expectedArgs)
		}
		return &cmd.Result{}, nil
	}

	client := NewClientWithExecutor("/git", executor)

	err := client.MergeReview(context.Background(), "github.com/owner/repo", 42)
	if err != nil {
		t.Fatalf("MergeReview failed: %v", err)
	}
}

func TestMergeReview_Error(t *testing.T) {
	expectedErr := errors.New("merge failed")
	executor := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		return nil, expectedErr
	}

	client := NewClientWithExecutor("/git", executor)

	err := client.MergeReview(context.Background(), "github.com/owner/repo", 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to merge PR #42") {
		t.Errorf("expected 'failed to merge PR #42' in error, got: %v", err)
	}
}

func TestCloseReview_Success(t *testing.T) {
	expectedArgs := []string{
		"pr", "close",
		"123",
		"--repo", "https://github.com/owner/repo",
	}

	executor := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		args = args[1:] // strip binary name
		if diff := cmp.Diff(args, expectedArgs); diff != "" {
			t.Errorf("unexpected args:\ngot:  %v\nwant: %v", args, expectedArgs)
		}
		return &cmd.Result{}, nil
	}

	client := NewClientWithExecutor("/git", executor)

	err := client.CloseReview(context.Background(), "github.com/owner/repo", 123)
	if err != nil {
		t.Fatalf("CloseReview failed: %v", err)
	}
}

func TestCloseReview_Error(t *testing.T) {
	expectedErr := errors.New("close failed")
	executor := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		return nil, expectedErr
	}

	client := NewClientWithExecutor("/git", executor)

	err := client.CloseReview(context.Background(), "github.com/owner/repo", 123)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to close PR #123") {
		t.Errorf("expected 'failed to close PR #123' in error, got: %v", err)
	}
}

func TestSetupRuleset_Success(t *testing.T) {
	var gotListArgs []string
	var gotCreateArgs []string
	var gotStdin []byte

	executor := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		args = args[1:] // strip binary name
		// First call: list existing rulesets (GET)
		if len(gotListArgs) == 0 && opts.Stdin == nil {
			gotListArgs = args
			return &cmd.Result{Stdout: "[]"}, nil // No existing rulesets
		}
		// Second call: create ruleset (POST)
		gotCreateArgs = args
		if opts.Stdin != nil {
			gotStdin, _ = io.ReadAll(opts.Stdin)
		}
		return &cmd.Result{Stdout: "{}"}, nil
	}

	client := NewClientWithExecutor("/git", executor)

	err := client.SetupRuleset(context.Background(), "github.com/owner/repo")
	if err != nil {
		t.Fatalf("SetupRuleset failed: %v", err)
	}

	// Verify list call
	if len(gotListArgs) == 0 {
		t.Fatal("expected list rulesets call")
	}
	expectedListPath := "/repos/owner/repo/rulesets"
	if gotListArgs[len(gotListArgs)-1] != expectedListPath {
		t.Errorf("expected list API path %s, got %s", expectedListPath, gotListArgs[len(gotListArgs)-1])
	}

	// Verify create call
	if len(gotCreateArgs) == 0 {
		t.Fatal("expected create ruleset call")
	}
	createArgsStr := strings.Join(gotCreateArgs, " ")
	if !strings.Contains(createArgsStr, "--method POST") {
		t.Errorf("expected POST method in create args, got: %v", gotCreateArgs)
	}
	expectedCreatePath := "/repos/owner/repo/rulesets"
	if !strings.Contains(createArgsStr, expectedCreatePath) {
		t.Errorf("expected create API path %s in args, got: %v", expectedCreatePath, gotCreateArgs)
	}

	// Verify stdin contains the ruleset JSON with correct name and pattern
	stdinStr := string(gotStdin)
	if !strings.Contains(stdinStr, `"name": "reject-forge-parent-trailer"`) {
		t.Errorf("expected ruleset name 'reject-forge-parent-trailer' in stdin, got: %s", stdinStr)
	}
	if !strings.Contains(stdinStr, `"pattern": "forge-parent:"`) {
		t.Errorf("expected pattern 'forge-parent:' in stdin, got: %s", stdinStr)
	}
}

func TestSetupRuleset_AlreadyExists(t *testing.T) {
	callCount := 0
	executor := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		_ = args[0] // binary name
		callCount++
		if callCount == 1 {
			// Return existing ruleset with matching name
			return &cmd.Result{Stdout: `[{"name": "reject-forge-parent-trailer"}]`}, nil
		}
		t.Fatal("should not make a second call when ruleset already exists")
		return nil, nil
	}

	client := NewClientWithExecutor("/git", executor)

	err := client.SetupRuleset(context.Background(), "github.com/owner/repo")
	if err != nil {
		t.Fatalf("SetupRuleset failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 API call (list only), got %d", callCount)
	}
}

func TestGitHubClient_WithGHCommand(t *testing.T) {
	var gotBin string
	executor := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		if len(args) > 0 {
			gotBin = args[0]
		}
		return &cmd.Result{Stdout: "main"}, nil
	}

	client := NewClientWithExecutor("/git", executor).WithGHCommand("gh-custom")
	_, err := client.DefaultBranch(context.Background(), "github.com/owner/repo")
	if err != nil {
		t.Fatalf("DefaultBranch() failed = %v", err)
	}

	if gotBin != "gh-custom" {
		t.Errorf("expected bin name 'gh-custom', got %q", gotBin)
	}
}
