package ssm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// httpDoer abstracts *http.Client for test mock injection.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client implements the forge.Forge interface for Google Cloud Secure Source Manager.
type Client struct {
	baseURL    string       // https://securesourcemanager.googleapis.com/v1
	repoName   string       // projects/P/locations/L/repositories/R
	htmlURL    string       // https://{instance}-git.{location}.sourcemanager.dev/{project}/{repo}
	httpClient httpDoer     // interface for testability
	executor   cmd.Executor // for `gcloud auth print-access-token`

	tokenOnce sync.Once
	token     string
	tokenErr  error
}

// NewClientFromURL creates a new SSM Client from a remote URL.
// It uses `gcloud auth print-access-token` for authentication via the provided executor.
func NewClientFromURL(ctx context.Context, url string, executor cmd.Executor) (*Client, error) {
	instance, location, project, repo, err := ParseSSMURL(url)
	if err != nil {
		return nil, fmt.Errorf("invalid SSM URL: %w", err)
	}
	repoName := ResourceName(project, location, repo)
	htmlURL := fmt.Sprintf("https://%s-git.%s.sourcemanager.dev/%s/%s", instance, location, project, repo)

	return &Client{
		baseURL:    "https://securesourcemanager.googleapis.com/v1",
		repoName:   repoName,
		htmlURL:    htmlURL,
		httpClient: http.DefaultClient,
		executor:   executor,
	}, nil
}

// newClientForTest creates a Client with a mock HTTP doer (for testing).
func newClientForTest(doer httpDoer, repoName, htmlURL string) *Client {
	return &Client{
		baseURL:    "https://test.example.com/v1",
		repoName:   repoName,
		htmlURL:    htmlURL,
		httpClient: doer,
		executor: func(ctx context.Context, opts cmd.Opts, args ...string) (string, error) {
			return "fake-token", nil
		},
	}
}

// prName builds a pull request resource name from the repo name and PR number.
func (c *Client) prName(number int) string {
	return fmt.Sprintf("%s/pullRequests/%d", c.repoName, number)
}

// prURL builds the web UI URL for a pull request.
func (c *Client) prURL(number int) string {
	return fmt.Sprintf("%s/-/pull/%d", c.htmlURL, number)
}

// parsePRNumber extracts the PR number from a resource name.
// Format: projects/P/locations/L/repositories/R/pullRequests/N
func parsePRNumber(name string) (int, error) {
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid PR resource name: %s", name)
	}
	return strconv.Atoi(parts[len(parts)-1])
}

// getToken retrieves an access token, caching it for the process lifetime.
// TODO: Add disk-based token cache to avoid the ~2s gcloud penalty on every CLI invocation.
func (c *Client) getToken(ctx context.Context) (string, error) {
	c.tokenOnce.Do(func() {
		out, err := c.executor(ctx, cmd.Opts{}, "gcloud", "auth", "print-access-token")
		c.token = strings.TrimSpace(out)
		c.tokenErr = err
	})
	return c.token, c.tokenErr
}

// doRequest builds and executes an authenticated HTTP request against the SSM REST API.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := fmt.Sprintf("%s/%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get auth token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(req)
}

// apiError represents a structured error response from the SSM REST API.
type apiError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// doJSON executes an authenticated request and decodes the JSON response into result.
func (c *Client) doJSON(ctx context.Context, method, path string, body io.Reader, result interface{}) error {
	resp, err := c.doRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return fmt.Errorf("SSM API error (%s, HTTP %d): %s", apiErr.Error.Status, apiErr.Error.Code, apiErr.Error.Message)
		}
		return fmt.Errorf("SSM API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

// lroResponse represents a Long-Running Operation from the SSM API.
type lroResponse struct {
	Name     string          `json:"name"`
	Done     bool            `json:"done"`
	Response json.RawMessage `json:"response"`
	Error    *lroError       `json:"error"`
}

type lroError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// doLRO executes a request that returns a Long-Running Operation, polls until done,
// and returns the response field.
func (c *Client) doLRO(ctx context.Context, method, path string, body io.Reader) (json.RawMessage, error) {
	var op lroResponse
	if err := c.doJSON(ctx, method, path, body, &op); err != nil {
		return nil, err
	}
	if op.Done {
		if op.Error != nil {
			return nil, fmt.Errorf("LRO error: %s", op.Error.Message)
		}
		return op.Response, nil
	}
	return c.pollOperation(ctx, op.Name)
}

// pollOperation polls an LRO by name until done, with exponential backoff.
func (c *Client) pollOperation(ctx context.Context, opName string) (json.RawMessage, error) {
	delay := 500 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}

		var op lroResponse
		if err := c.doJSON(ctx, http.MethodGet, opName, nil, &op); err != nil {
			return nil, fmt.Errorf("failed to poll operation %s: %w", opName, err)
		}
		if op.Done {
			if op.Error != nil {
				return nil, fmt.Errorf("LRO error: %s", op.Error.Message)
			}
			return op.Response, nil
		}

		// Exponential backoff, capped at 5s
		delay = time.Duration(float64(delay) * 1.5)
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
	}
}

// pullRequestJSON is the JSON representation of an SSM pull request.
type pullRequestJSON struct {
	Name  string    `json:"name"`
	Title string    `json:"title"`
	Body  string    `json:"body"`
	Head  branchRef `json:"head"`
	Base  branchRef `json:"base"`
	State string    `json:"state"`
}

type branchRef struct {
	Ref string `json:"ref"`
}

// CreateReview creates a new pull request on SSM.
func (c *Client) CreateReview(ctx context.Context, _ string, params forge.ReviewCreateParams) (*forge.ReviewCreateResult, error) {
	reqBody := pullRequestJSON{
		Title: params.Title,
		Body:  params.Body,
		Head:  branchRef{Ref: params.FromBranch},
		Base:  branchRef{Ref: params.ToBranch},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PR request: %w", err)
	}

	raw, err := c.doLRO(ctx, http.MethodPost, c.repoName+"/pullRequests", strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	var pr pullRequestJSON
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, fmt.Errorf("failed to decode PR response: %w", err)
	}

	number, err := parsePRNumber(pr.Name)
	if err != nil {
		return nil, err
	}
	return &forge.ReviewCreateResult{
		Number: number,
		URL:    c.prURL(number),
	}, nil
}

// MergeReview merges an open pull request on SSM.
func (c *Client) MergeReview(ctx context.Context, _ string, reviewNumber int) error {
	path := fmt.Sprintf("%s/pullRequests/%d:merge", c.repoName, reviewNumber)
	_, err := c.doLRO(ctx, http.MethodPost, path, strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("failed to merge PR #%d: %w", reviewNumber, err)
	}
	return nil
}

// CloseReview closes a pull request without merging on SSM.
func (c *Client) CloseReview(ctx context.Context, _ string, reviewNumber int) error {
	path := fmt.Sprintf("%s/pullRequests/%d:close", c.repoName, reviewNumber)
	_, err := c.doLRO(ctx, http.MethodPost, path, strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("failed to close PR #%d: %w", reviewNumber, err)
	}
	return nil
}

// listPullRequestsResponse is the JSON response for listing pull requests.
type listPullRequestsResponse struct {
	PullRequests  []pullRequestJSON `json:"pullRequests"`
	NextPageToken string            `json:"nextPageToken"`
}

// FindReview searches for a pull request by source branch name.
// SSM does not support server-side filtering, so we iterate all PRs.
func (c *Client) FindReview(ctx context.Context, _ string, branch string) (*forge.ReviewDetails, error) {
	path := c.repoName + "/pullRequests"
	for {
		var resp listPullRequestsResponse
		if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
			return nil, fmt.Errorf("failed to list PRs: %w", err)
		}
		for _, pr := range resp.PullRequests {
			if pr.Head.Ref == branch {
				number, err := parsePRNumber(pr.Name)
				if err != nil {
					return nil, err
				}
				return &forge.ReviewDetails{
					Number: number,
					URL:    c.prURL(number),
					State:  mapSSMState(pr.State),
					Title:  pr.Title,
					Body:   pr.Body,
				}, nil
			}
		}
		if resp.NextPageToken == "" {
			break
		}
		path = c.repoName + "/pullRequests?pageToken=" + resp.NextPageToken
	}
	return nil, nil // No review found
}

// GetReview retrieves details of a specific pull request.
func (c *Client) GetReview(ctx context.Context, _ string, number int) (*forge.ReviewDetails, error) {
	var pr pullRequestJSON
	if err := c.doJSON(ctx, http.MethodGet, c.prName(number), nil, &pr); err != nil {
		return nil, fmt.Errorf("failed to get PR #%d: %w", number, err)
	}
	return &forge.ReviewDetails{
		Number: number,
		URL:    c.prURL(number),
		State:  mapSSMState(pr.State),
		Title:  pr.Title,
		Body:   pr.Body,
	}, nil
}

// UpdateReview updates the body/description of an existing pull request.
func (c *Client) UpdateReview(ctx context.Context, _ string, reviewNumber int, body string) error {
	reqBody := struct {
		Body string `json:"body"`
	}{Body: body}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal update request: %w", err)
	}

	path := fmt.Sprintf("%s/pullRequests/%d?updateMask=body", c.repoName, reviewNumber)
	_, err = c.doLRO(ctx, http.MethodPatch, path, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("failed to update PR #%d: %w", reviewNumber, err)
	}
	return nil
}

// repositoryJSON is the JSON representation of an SSM repository.
type repositoryJSON struct {
	InitialConfig struct {
		DefaultBranch string `json:"defaultBranch"`
	} `json:"initialConfig"`
}

// DefaultBranch returns the default branch name of the repository.
func (c *Client) DefaultBranch(ctx context.Context, _ string) (string, error) {
	var repo repositoryJSON
	if err := c.doJSON(ctx, http.MethodGet, c.repoName, nil, &repo); err != nil {
		return "", fmt.Errorf("failed to get repository: %w", err)
	}
	branch := repo.InitialConfig.DefaultBranch
	if branch == "" {
		return "main", nil // Default fallback
	}
	return branch, nil
}

// SetupRuleset is a no-op for SSM. SSM doesn't support commit-message-pattern
// rules like GitHub's `forge-parent` trailer check.
func (c *Client) SetupRuleset(_ context.Context, _ string) error {
	return nil
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

// FormatHeadBranch returns the head branch reference for SSM PRs.
// SSM PRs are within the same repo, so no owner prefix is needed.
func (c *Client) FormatHeadBranch(_ context.Context, _ jj.Client, _, changeID string) (string, error) {
	return fmt.Sprintf("push-%s", changeID), nil
}

// NormalizeRepoURL converts a remote URL to SSM's canonical HTTPS format.
func (c *Client) NormalizeRepoURL(url string) (string, error) {
	return NormalizeSSMURL(url)
}

// SupportsForks returns false because SSM does not use a fork-based workflow.
func (c *Client) SupportsForks() bool {
	return false
}

func mapSSMState(state string) forge.ReviewState {
	switch state {
	case "OPEN":
		return forge.ReviewStateOpen
	case "MERGED":
		return forge.ReviewStateMerged
	case "CLOSED":
		return forge.ReviewStateClosed
	default:
		return forge.ReviewStateClosed
	}
}
