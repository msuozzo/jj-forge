package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// Client implements the forge.Forge interface for GitHub using the gh CLI.
type Client struct {
	gitDir   string       // Path to .git directory for GIT_DIR env var
	executor cmd.Executor // Function to execute gh commands
}

// NewClient creates a GitHub client with the default executor.
func NewClient(gitDir string) *Client {
	return &Client{
		gitDir:   gitDir,
		executor: cmd.DefaultExecutor,
	}
}

// NewClientWithExecutor creates a GitHub client with a custom executor (for testing).
func NewClientWithExecutor(gitDir string, exec cmd.Executor) *Client {
	return &Client{
		gitDir:   gitDir,
		executor: exec,
	}
}

// run calls the executor with "gh" prepended to args, injecting GIT_DIR if set.
func (c *Client) run(ctx context.Context, opts cmd.Opts, args ...string) (string, error) {
	if c.gitDir != "" {
		opts.Env = append(opts.Env, fmt.Sprintf("GIT_DIR=%s", c.gitDir))
	}
	result, err := c.executor(ctx, opts, append([]string{"gh"}, args...)...)
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
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
	output, err := c.run(ctx, cmd.Opts{}, args...)
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

// MergeReview merges a pull request with squash merge.
func (c *Client) MergeReview(ctx context.Context, repoURI string, reviewNumber int) error {
	// Normalize the repo URI to HTTPS format
	normalizedURI, err := forge.NormalizeRepoURL(repoURI)
	if err != nil {
		return fmt.Errorf("invalid repository URI: %w", err)
	}
	args := []string{
		"pr", "merge",
		fmt.Sprintf("%d", reviewNumber),
		"--repo", normalizedURI,
		"--squash",
	}
	_, err = c.run(ctx, cmd.Opts{}, args...)
	if err != nil {
		return fmt.Errorf("failed to merge PR #%d: %w", reviewNumber, err)
	}
	return nil
}

// CloseReview closes a pull request without merging.
func (c *Client) CloseReview(ctx context.Context, repoURI string, reviewNumber int) error {
	// Normalize the repo URI to HTTPS format
	normalizedURI, err := forge.NormalizeRepoURL(repoURI)
	if err != nil {
		return fmt.Errorf("invalid repository URI: %w", err)
	}
	args := []string{
		"pr", "close",
		fmt.Sprintf("%d", reviewNumber),
		"--repo", normalizedURI,
	}
	_, err = c.run(ctx, cmd.Opts{}, args...)
	if err != nil {
		return fmt.Errorf("failed to close PR #%d: %w", reviewNumber, err)
	}
	return nil
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
	output, err := c.run(ctx, cmd.Opts{}, args...)
	if err != nil {
		return "", fmt.Errorf("failed to get default branch: %w", err)
	}
	branch := strings.TrimSpace(output)
	if branch == "" {
		return "", fmt.Errorf("gh repo view returned empty default branch")
	}
	return branch, nil
}

// FindReview searches for a review by branch name.
func (c *Client) FindReview(ctx context.Context, repoURI, branch string) (*forge.ReviewDetails, error) {
	normalizedURI, err := forge.NormalizeRepoURL(repoURI)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URI: %w", err)
	}
	// Search for PR with specific head branch
	args := []string{
		"pr", "list",
		"--repo", normalizedURI,
		"--head", branch,
		"--state", "all",
		"--json", "number,url,state",
		"--limit", "1",
	}
	output, err := c.run(ctx, cmd.Opts{}, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list PRs: %w", err)
	}

	var reviews []struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal([]byte(output), &reviews); err != nil {
		return nil, fmt.Errorf("failed to parse PR list: %w", err)
	}

	if len(reviews) == 0 {
		return nil, nil // No review found
	}

	return &forge.ReviewDetails{
		Number: reviews[0].Number,
		URL:    reviews[0].URL,
		State:  mapState(reviews[0].State),
	}, nil
}

// GetReview retrieves details of a specific review.
func (c *Client) GetReview(ctx context.Context, repoURI string, number int) (*forge.ReviewDetails, error) {
	normalizedURI, err := forge.NormalizeRepoURL(repoURI)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URI: %w", err)
	}

	args := []string{
		"pr", "view",
		fmt.Sprintf("%d", number),
		"--repo", normalizedURI,
		"--json", "number,url,state,title,body",
	}
	output, err := c.run(ctx, cmd.Opts{}, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR #%d: %w", number, err)
	}

	var review struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
		State  string `json:"state"`
		Title  string `json:"title"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(output), &review); err != nil {
		return nil, fmt.Errorf("failed to parse PR details: %w", err)
	}

	return &forge.ReviewDetails{
		Number: review.Number,
		URL:    review.URL,
		State:  mapState(review.State),
		Title:  review.Title,
		Body:   review.Body,
	}, nil
}

// UpdateReview updates the body of a pull request.
func (c *Client) UpdateReview(ctx context.Context, repoURI string, reviewNumber int, body string) error {
	normalizedURI, err := forge.NormalizeRepoURL(repoURI)
	if err != nil {
		return fmt.Errorf("invalid repository URI: %w", err)
	}
	args := []string{
		"pr", "edit",
		fmt.Sprintf("%d", reviewNumber),
		"--repo", normalizedURI,
		"--body", body,
	}
	_, err = c.run(ctx, cmd.Opts{}, args...)
	if err != nil {
		return fmt.Errorf("failed to update PR #%d: %w", reviewNumber, err)
	}
	return nil
}

func mapState(state string) forge.ReviewState {
	switch strings.ToLower(state) {
	case "open":
		return forge.ReviewStateOpen
	case "merged":
		return forge.ReviewStateMerged
	case "closed":
		return forge.ReviewStateClosed
	default:
		return forge.ReviewStateClosed // Default to closed for unknown states
	}
}

const rulesetName = "reject-forge-parent-trailer"

// SetupRuleset configures a ruleset on GitHub to prevent merging commits with forge-parent.
// It is idempotent: if a ruleset with the expected name already exists, it is left as-is.
func (c *Client) SetupRuleset(ctx context.Context, repoURI string) error {
	normalizedURI, err := forge.NormalizeRepoURL(repoURI)
	if err != nil {
		return fmt.Errorf("invalid repository URI: %w", err)
	}
	// Extract path from normalizedURI generically (works for github.com and GHES)
	u, err := url.Parse(normalizedURI)
	if err != nil {
		return fmt.Errorf("invalid repository URI: %w", err)
	}
	apiPath := fmt.Sprintf("/repos%s/rulesets", u.Path)
	// Check for existing ruleset with the same name.
	listArgs := []string{
		"api",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2022-11-28",
		apiPath,
	}
	listOutput, err := c.run(ctx, cmd.Opts{}, listArgs...)
	if err != nil {
		return fmt.Errorf("failed to list rulesets: %w", err)
	}
	var existingRulesets []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(listOutput), &existingRulesets); err != nil {
		return fmt.Errorf("failed to parse rulesets: %w", err)
	}
	for _, rs := range existingRulesets {
		if rs.Name == rulesetName {
			return nil // Already exists
		}
	}
	rulesetJSON := `{
  "name": "` + rulesetName + `",
  "target": "branch",
  "enforcement": "active",
  "conditions": {
    "ref_name": {
      "exclude": [],
      "include": [
        "~DEFAULT_BRANCH"
      ]
    }
  },
  "rules": [
    {
      "type": "commit_message_pattern",
      "parameters": {
        "operator": "contains",
        "pattern": "forge-parent:",
        "negate": true,
        "name": ""
      }
    }
  ],
  "bypass_actors": []
}`
	createArgs := []string{
		"api",
		"--method", "POST",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2022-11-28",
		apiPath,
		"--input", "-",
	}
	_, err = c.run(ctx, cmd.Opts{Stdin: bytes.NewReader([]byte(rulesetJSON))}, createArgs...)
	if err != nil {
		return fmt.Errorf("failed to setup ruleset: %w", err)
	}
	return nil
}

// FormatHeadBranch returns the head branch reference for a cross-repo GitHub PR.
// Format: "owner:push-{changeID}"
func (c *Client) FormatHeadBranch(ctx context.Context, jjClient jj.Client, forkRemote, changeID string) (string, error) {
	repoInfo, err := forge.GetRepoInfo(ctx, jjClient, forkRemote)
	if err != nil {
		return "", fmt.Errorf("failed to get repo info for %s: %w", forkRemote, err)
	}
	return fmt.Sprintf("%s:push-%s", repoInfo.Owner, changeID), nil
}

// NormalizeRepoURL converts a remote URL to GitHub's canonical HTTPS format.
func (c *Client) NormalizeRepoURL(url string) (string, error) {
	return forge.NormalizeRepoURL(url)
}

// SupportsForks returns true because GitHub uses a fork-based workflow.
func (c *Client) SupportsForks() bool {
	return true
}
