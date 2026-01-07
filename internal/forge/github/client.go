package github

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/msuozzo/jj-forge/internal/forge"
)

// Executor defines the function signature for running gh commands.
type Executor func(ctx context.Context, args ...string) (stdout string, err error)

// Client implements the forge.Forge interface for GitHub using the gh CLI.
type Client struct {
	gitDir   string   // Path to .git directory for GIT_DIR env var
	executor Executor // Function to execute gh commands
}

// NewClient creates a GitHub client with the default executor.
func NewClient(gitDir string) *Client {
	return &Client{
		gitDir:   gitDir,
		executor: defaultExecutor(gitDir),
	}
}

// NewClientWithExecutor creates a GitHub client with a custom executor (for testing).
func NewClientWithExecutor(gitDir string, exec Executor) *Client {
	return &Client{
		gitDir:   gitDir,
		executor: exec,
	}
}

// defaultExecutor creates an executor that runs gh commands with proper GIT_DIR.
func defaultExecutor(gitDir string) Executor {
	return func(ctx context.Context, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "gh", args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		// Set GIT_DIR environment variable if provided
		if gitDir != "" {
			cmd.Env = append(os.Environ(), fmt.Sprintf("GIT_DIR=%s", gitDir))
		}
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("gh command failed: %w\nstderr: %s", err, stderr.String())
		}
		return stdout.String(), nil
	}
}

// CreateReview creates a new pull request on GitHub.
func (c *Client) CreateReview(ctx context.Context, repoURI string, params forge.ReviewCreateParams) (*forge.ReviewCreateResult, error) {
	// Normalize the repo URI to HTTPS format
	normalizedURI, err := forge.NormalizeRepoURL(repoURI)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URI: %w", err)
	}
	args := []string{
		"pr", "create",
		"--repo", normalizedURI,
		"--title", params.Title,
		"--body", params.Body,
		"--head", params.FromBranch,
		"--base", params.ToBranch,
	}
	// Add reviewers if provided
	for _, reviewer := range params.Reviewers {
		args = append(args, "--reviewer", reviewer)
	}
	output, err := c.executor(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}
	// Parse output (URL)
	url := strings.TrimSpace(output)
	if url == "" {
		return nil, fmt.Errorf("gh pr create returned empty output")
	}
	// Extract number from URL (e.g. https://github.com/owner/repo/pull/123)
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid PR URL format: %s", url)
	}
	numberStr := parts[len(parts)-1]
	number, err := strconv.Atoi(numberStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PR number from URL %s: %w", url, err)
	}
	return &forge.ReviewCreateResult{
		Number: number,
		URL:    url,
	}, nil
}

// FormatID formats a review number into a string ID (e.g. "pr/123").
func (c *Client) FormatID(number int) string {
	return fmt.Sprintf("pr/%d", number)
}

// ParseID parses a string ID (e.g. "pr/123") into a review number.
func (c *Client) ParseID(id string) (int, error) {
	if strings.HasPrefix(id, "pr/") {
		id = strings.TrimPrefix(id, "pr/")
	}
	return strconv.Atoi(id)
}

// DefaultBranch returns the default branch name of the repository.
func (c *Client) DefaultBranch(ctx context.Context, repoURI string) (string, error) {
	// Normalize the repo URI to HTTPS format
	normalizedURI, err := forge.NormalizeRepoURL(repoURI)
	if err != nil {
		return "", fmt.Errorf("invalid repository URI: %w", err)
	}
	// NOTE: There is a forge-independent solution: git ls-remote --symref <URI> HEAD
	args := []string{
		"repo", "view",
		normalizedURI,
		"--json", "defaultBranchRef",
		"--template", "{{.defaultBranchRef.name}}",
	}
	output, err := c.executor(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("failed to get default branch: %w", err)
	}
	branch := strings.TrimSpace(output)
	if branch == "" {
		return "", fmt.Errorf("gh repo view returned empty default branch")
	}
	return branch, nil
}
