package repoclone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// githubURLRegex matches GitHub URLs in both SSH and HTTPS formats.
var githubURLRegex = regexp.MustCompile(`github\.com[:/]([^/]+)/(.+?)(\.git)?$`)

// Executor defines the function signature for running gh commands.
type Executor func(ctx context.Context, args ...string) (stdout string, err error)

// defaultExecutor creates an executor that runs gh commands.
func defaultExecutor() Executor {
	return func(ctx context.Context, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "gh", args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("gh command failed: %w\nstderr: %s", err, stderr.String())
		}
		return stdout.String(), nil
	}
}

// GitHubClient handles GitHub API operations for repository analysis.
type GitHubClient struct {
	executor Executor
}

// NewGitHubClient creates a GitHubClient with the default executor.
func NewGitHubClient() *GitHubClient {
	return &GitHubClient{
		executor: defaultExecutor(),
	}
}

// NewGitHubClientWithExecutor creates a GitHubClient with a custom executor (for testing).
func NewGitHubClientWithExecutor(exec Executor) *GitHubClient {
	return &GitHubClient{
		executor: exec,
	}
}

// RepoRef holds owner and name for a repository reference.
type RepoRef struct {
	Owner string
	Name  string
}

// RepoAnalysis contains detailed information about a repository.
type RepoAnalysis struct {
	Owner    string   // Repository owner
	Name     string   // Repository name
	Exists   bool     // Whether repo exists on GitHub
	IsMine   bool     // Whether current user owns it
	IsFork   bool     // Whether it's a fork
	Parent   *RepoRef // Parent repo if fork (owner/name)
	SSHURL   string   // SSH clone URL
	HTTPSURL string   // HTTPS clone URL
}

// ParseGitHubURL extracts owner and name from a GitHub URL.
func ParseGitHubURL(url string) (*RepoRef, error) {
	matches := githubURLRegex.FindStringSubmatch(url)
	if matches == nil || len(matches) < 3 {
		return nil, fmt.Errorf("could not parse GitHub URL: %s", url)
	}
	return &RepoRef{
		Owner: matches[1],
		Name:  strings.TrimSuffix(matches[2], ".git"),
	}, nil
}

// GetAuthenticatedUser returns the current GitHub user.
func (c *GitHubClient) GetAuthenticatedUser(ctx context.Context) (string, error) {
	output, err := c.executor(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", fmt.Errorf("failed to get authenticated user: %w", err)
	}
	return strings.TrimSpace(output), nil
}

// AnalyzeRepository checks ownership and fork status of a repository.
func (c *GitHubClient) AnalyzeRepository(ctx context.Context, repoURL string) (*RepoAnalysis, error) {
	// Parse the URL to get owner/name
	ref, err := ParseGitHubURL(repoURL)
	if err != nil {
		return nil, err
	}

	// Get authenticated user
	authUser, err := c.GetAuthenticatedUser(ctx)
	if err != nil {
		return nil, err
	}

	// Query GitHub API for repo info
	apiPath := fmt.Sprintf("repos/%s/%s", ref.Owner, ref.Name)
	output, err := c.executor(ctx, "api", apiPath, "--jq", `{
		fork: .fork,
		parent_owner: .parent.owner.login,
		parent_name: .parent.name,
		ssh_url: .ssh_url,
		clone_url: .clone_url
	}`)

	analysis := &RepoAnalysis{
		Owner: ref.Owner,
		Name:  ref.Name,
	}

	if err != nil {
		// Check if it's a 404 (repo doesn't exist)
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "Not Found") {
			analysis.Exists = false
			analysis.IsMine = ref.Owner == authUser
			return analysis, nil
		}
		return nil, fmt.Errorf("failed to get repo info: %w", err)
	}

	// Parse the JSON response
	var repoInfo struct {
		Fork        bool   `json:"fork"`
		ParentOwner string `json:"parent_owner"`
		ParentName  string `json:"parent_name"`
		SSHURL      string `json:"ssh_url"`
		CloneURL    string `json:"clone_url"`
	}
	if err := json.Unmarshal([]byte(output), &repoInfo); err != nil {
		return nil, fmt.Errorf("failed to parse repo info: %w", err)
	}

	analysis.Exists = true
	analysis.IsMine = ref.Owner == authUser
	analysis.IsFork = repoInfo.Fork
	analysis.SSHURL = repoInfo.SSHURL
	analysis.HTTPSURL = repoInfo.CloneURL

	if repoInfo.Fork && repoInfo.ParentOwner != "" {
		analysis.Parent = &RepoRef{
			Owner: repoInfo.ParentOwner,
			Name:  repoInfo.ParentName,
		}
	}

	return analysis, nil
}

// FindMyFork checks if the authenticated user has a fork of the given upstream repo.
func (c *GitHubClient) FindMyFork(ctx context.Context, upstreamOwner, upstreamName string) (*RepoAnalysis, error) {
	// Get authenticated user
	authUser, err := c.GetAuthenticatedUser(ctx)
	if err != nil {
		return nil, err
	}

	// Check if user's fork exists by querying their repo directly
	apiPath := fmt.Sprintf("repos/%s/%s", authUser, upstreamName)
	output, err := c.executor(ctx, "api", apiPath, "--jq", `{
		fork: .fork,
		parent_owner: .parent.owner.login,
		parent_name: .parent.name,
		ssh_url: .ssh_url,
		clone_url: .clone_url
	}`)

	if err != nil {
		// No fork found (404)
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "Not Found") {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to check for fork: %w", err)
	}

	// Parse the response
	var repoInfo struct {
		Fork        bool   `json:"fork"`
		ParentOwner string `json:"parent_owner"`
		ParentName  string `json:"parent_name"`
		SSHURL      string `json:"ssh_url"`
		CloneURL    string `json:"clone_url"`
	}
	if err := json.Unmarshal([]byte(output), &repoInfo); err != nil {
		return nil, fmt.Errorf("failed to parse fork info: %w", err)
	}

	// Verify it's actually a fork of the expected upstream
	if !repoInfo.Fork || repoInfo.ParentOwner != upstreamOwner || repoInfo.ParentName != upstreamName {
		return nil, nil
	}

	return &RepoAnalysis{
		Owner:    authUser,
		Name:     upstreamName,
		Exists:   true,
		IsMine:   true,
		IsFork:   true,
		SSHURL:   repoInfo.SSHURL,
		HTTPSURL: repoInfo.CloneURL,
		Parent: &RepoRef{
			Owner: upstreamOwner,
			Name:  upstreamName,
		},
	}, nil
}

// CreateFork creates a fork of the upstream repo for the authenticated user.
func (c *GitHubClient) CreateFork(ctx context.Context, upstreamOwner, upstreamName string) (*RepoAnalysis, error) {
	// Get authenticated user first
	authUser, err := c.GetAuthenticatedUser(ctx)
	if err != nil {
		return nil, err
	}

	// Create the fork using gh repo fork
	repoPath := fmt.Sprintf("%s/%s", upstreamOwner, upstreamName)
	_, err = c.executor(ctx, "repo", "fork", repoPath, "--clone=false", "--default-branch-only")
	if err != nil {
		return nil, fmt.Errorf("failed to create fork: %w", err)
	}

	// Get the fork info
	apiPath := fmt.Sprintf("repos/%s/%s", authUser, upstreamName)
	output, err := c.executor(ctx, "api", apiPath, "--jq", `{
		ssh_url: .ssh_url,
		clone_url: .clone_url
	}`)
	if err != nil {
		return nil, fmt.Errorf("failed to get fork info after creation: %w", err)
	}

	var repoInfo struct {
		SSHURL   string `json:"ssh_url"`
		CloneURL string `json:"clone_url"`
	}
	if err := json.Unmarshal([]byte(output), &repoInfo); err != nil {
		return nil, fmt.Errorf("failed to parse fork info: %w", err)
	}

	return &RepoAnalysis{
		Owner:    authUser,
		Name:     upstreamName,
		Exists:   true,
		IsMine:   true,
		IsFork:   true,
		SSHURL:   repoInfo.SSHURL,
		HTTPSURL: repoInfo.CloneURL,
		Parent: &RepoRef{
			Owner: upstreamOwner,
			Name:  upstreamName,
		},
	}, nil
}

// CreateRepo creates a new repository for the authenticated user.
func (c *GitHubClient) CreateRepo(ctx context.Context, name string, private bool) (*RepoAnalysis, error) {
	// Get authenticated user first
	authUser, err := c.GetAuthenticatedUser(ctx)
	if err != nil {
		return nil, err
	}

	// Create the repo
	args := []string{"repo", "create", fmt.Sprintf("%s/%s", authUser, name), "--license=mit"}
	if private {
		args = append(args, "--private")
	} else {
		args = append(args, "--public")
	}
	_, err = c.executor(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	// Get the repo info
	apiPath := fmt.Sprintf("repos/%s/%s", authUser, name)
	output, err := c.executor(ctx, "api", apiPath, "--jq", `{
		ssh_url: .ssh_url,
		clone_url: .clone_url
	}`)
	if err != nil {
		return nil, fmt.Errorf("failed to get repo info after creation: %w", err)
	}

	var repoInfo struct {
		SSHURL   string `json:"ssh_url"`
		CloneURL string `json:"clone_url"`
	}
	if err := json.Unmarshal([]byte(output), &repoInfo); err != nil {
		return nil, fmt.Errorf("failed to parse repo info: %w", err)
	}

	return &RepoAnalysis{
		Owner:    authUser,
		Name:     name,
		Exists:   true,
		IsMine:   true,
		IsFork:   false,
		SSHURL:   repoInfo.SSHURL,
		HTTPSURL: repoInfo.CloneURL,
	}, nil
}

// GetUpstreamInfo gets the SSH and HTTPS URLs for an upstream repository.
func (c *GitHubClient) GetUpstreamInfo(ctx context.Context, owner, name string) (sshURL, httpsURL string, err error) {
	apiPath := fmt.Sprintf("repos/%s/%s", owner, name)
	output, err := c.executor(ctx, "api", apiPath, "--jq", `{
		ssh_url: .ssh_url,
		clone_url: .clone_url
	}`)
	if err != nil {
		return "", "", fmt.Errorf("failed to get upstream info: %w", err)
	}

	var repoInfo struct {
		SSHURL   string `json:"ssh_url"`
		CloneURL string `json:"clone_url"`
	}
	if err := json.Unmarshal([]byte(output), &repoInfo); err != nil {
		return "", "", fmt.Errorf("failed to parse upstream info: %w", err)
	}

	return repoInfo.SSHURL, repoInfo.CloneURL, nil
}
