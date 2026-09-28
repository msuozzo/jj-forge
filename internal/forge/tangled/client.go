// Package tangled implements the forge.Forge interface for Tangled
// (https://tangled.org), an AT Protocol git forge, via the tg CLI.
//
// Tangled differs from branch-tracking forges in two ways that shape this
// client:
//
//   - Pull requests are atproto records keyed by a record key (a TID such as
//     "3mujbgpmfg422"). The numeric "#N" shown on the web is assigned by the
//     appview and is not exposed by tg, so review IDs here are record keys.
//     Review URLs are resolved to the numbered page through the web UI (see
//     web.go), falling back to the repository's pull request list.
//   - A pull request carries a snapshot of the patch series (a "round") rather
//     than tracking its source branch. Creating or re-submitting one requires
//     tg to run git format-patch against local refs, so the client exports jj
//     bookmarks to git before invoking tg and resolves the base branch through
//     the upstream remote's tracking ref.
package tangled

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// Client implements forge.Forge and forge.ReviewSyncer for Tangled using tg.
type Client struct {
	cli            *CLI
	executor       cmd.Executor
	upstreamRemote string    // Git remote whose tracking refs provide PR base revisions
	jjClient       jj.Client // Exports bookmarks to git refs before tg builds patches (nil skips)

	web *webClient // Resolves numbered PR pages on the web UI

	branchMu      sync.Mutex
	defaultBranch map[string]string // repoURI -> default branch
}

// NewClient creates a Tangled client.
//
// gitDir is exported to tg as GIT_DIR. upstreamRemote is the git remote name
// of the repository reviews target (e.g. "up"). tg resolves the PR base as
// "<upstreamRemote>/<branch>". jjClient, when non-nil, is used to export jj
// bookmarks to git refs so that tg can see push-* branches.
func NewClient(gitDir, upstreamRemote string, jjClient jj.Client, executor cmd.Executor) *Client {
	return &Client{
		cli:            NewCLI(executor).WithGitDir(gitDir),
		executor:       executor,
		upstreamRemote: upstreamRemote,
		jjClient:       jjClient,
		web:            newWebClient("https://" + WebHost),
		defaultBranch:  make(map[string]string),
	}
}

// WithTGCommand configures a custom tg binary (instead of "tg").
func (c *Client) WithTGCommand(name string) *Client {
	c.cli.WithCommand(name)
	return c
}

// WithWebBaseURL points PR page resolution at a different appview
// (e.g. a self-hosted one, or a test server). Defaults to https://tangled.org.
func (c *Client) WithWebBaseURL(baseURL string) *Client {
	if baseURL != "" {
		c.web = newWebClient(baseURL)
	}
	return c
}

// reviewURL resolves the numbered PR page for a record key, falling back to
// the pull request list when the web UI has not surfaced it (yet).
func (c *Client) reviewURL(ctx context.Context, ref *RepoRef, rkey, title string, attempts int) string {
	if url, err := c.web.pullPageURL(ctx, ref, rkey, title, attempts); err == nil {
		return url
	}
	return ref.PullsURL()
}

// repoRef parses the repository remote URL used for a forge call.
func repoRef(repoURI string) (*RepoRef, error) {
	ref, err := ParseURL(repoURI)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URI: %w", err)
	}
	return ref, nil
}

// exportRefs makes jj's local bookmarks visible to git as refs/heads/*.
// Non-colocated jj repos only write these on an explicit export, and tg
// resolves the PR head branch through git.
func (c *Client) exportRefs(ctx context.Context) error {
	if c.jjClient == nil {
		return nil
	}
	if _, err := c.jjClient.Run(ctx, "git", "export"); err != nil {
		return fmt.Errorf("failed to export jj bookmarks to git: %w", err)
	}
	return nil
}

// baseRef returns the git ref tg should diff against for a target branch.
func (c *Client) baseRef(branch string) string {
	if c.upstreamRemote == "" {
		return branch
	}
	return c.upstreamRemote + "/" + branch
}

// pullCreateJSON is tg's `pr create --json` output.
type pullCreateJSON struct {
	URI   string `json:"uri"` // at://<did>/sh.tangled.repo.pull/<rkey>
	Title string `json:"title"`
	Base  string `json:"base"`
	Head  string `json:"head"`
}

// pullItemJSON is one entry of tg's `pr list --json` output.
type pullItemJSON struct {
	Rkey         string `json:"rkey"`
	URI          string `json:"uri"`
	Title        string `json:"title"`
	State        string `json:"state"`
	SourceBranch string `json:"sourceBranch,omitempty"`
	TargetBranch string `json:"targetBranch,omitempty"`
}

type pullListJSON struct {
	Items []pullItemJSON `json:"items"`
}

// pullViewJSON is tg's `pr view --json` output.
type pullViewJSON struct {
	Rkey         string `json:"rkey"`
	Title        string `json:"title"`
	State        string `json:"state"`
	Body         string `json:"body,omitempty"`
	SourceBranch string `json:"sourceBranch,omitempty"`
	TargetBranch string `json:"targetBranch,omitempty"`
}

// pullMergeJSON is tg's `pr merge --json` output.
type pullMergeJSON struct {
	Rkey           string   `json:"rkey"`
	Merged         bool     `json:"merged"`
	StatusRecorded bool     `json:"statusRecorded"`
	Warnings       []string `json:"warnings,omitempty"`
}

// CreateReview creates a pull request from the pushed head branch.
//
// tg generates the patch locally (base..head must be a non-empty range with
// base an ancestor of head) and uploads it as the PR's first round.
func (c *Client) CreateReview(ctx context.Context, repoURI string, params forge.ReviewCreateParams) (*forge.ReviewCreateResult, error) {
	// TODO: Assign reviewers once tg can set Tangled's assignee label. Pull
	// records have no reviewer field, but the default "assignee" label
	// definition takes DIDs and applies to pulls.
	if len(params.Reviewers) > 0 {
		return nil, fmt.Errorf("assigning reviewers (%s) is not implemented for Tangled yet, so drop --reviewer or clear forge.default-reviewer for this repo",
			strings.Join(params.Reviewers, ", "))
	}
	ref, err := repoRef(repoURI)
	if err != nil {
		return nil, err
	}
	if err := c.exportRefs(ctx); err != nil {
		return nil, err
	}
	var created pullCreateJSON
	err = c.cli.RunJSON(ctx, &created,
		"pr", "create",
		"--repo", ref.Target(),
		"--title", params.Title,
		"--body", params.Body,
		"--head", params.FromBranch,
		"--base", c.baseRef(params.ToBranch),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}
	rkey := rkeyFromURI(created.URI)
	if rkey == "" {
		return nil, fmt.Errorf("tg pr create returned no record URI")
	}
	// The appview ingests the record from the firehose, so poll briefly for the
	// numbered page. A miss is not fatal: the list URL still works.
	return &forge.ReviewCreateResult{
		ID:  rkey,
		URL: c.reviewURL(ctx, ref, rkey, params.Title, createLookupAttempts),
	}, nil
}

// MergeReview applies the PR's latest round on the knot and records it merged.
func (c *Client) MergeReview(ctx context.Context, repoURI string, reviewID string, _ string) error {
	ref, err := repoRef(repoURI)
	if err != nil {
		return err
	}
	var merged pullMergeJSON
	if err := c.cli.RunJSON(ctx, &merged, "pr", "merge", reviewID, "--repo", ref.Target()); err != nil {
		return fmt.Errorf("failed to merge PR #%s: %w", reviewID, err)
	}
	if !merged.Merged {
		return fmt.Errorf("failed to merge PR #%s: tg reported the merge did not complete (%s)",
			reviewID, strings.Join(merged.Warnings, "; "))
	}
	// A merge that landed but whose status record failed is still merged. The
	// state will be reconciled on the next `review import`.
	return nil
}

// CloseReview closes a pull request without merging.
func (c *Client) CloseReview(ctx context.Context, repoURI string, reviewID string) error {
	ref, err := repoRef(repoURI)
	if err != nil {
		return err
	}
	var state struct {
		State string `json:"state"`
	}
	if err := c.cli.RunJSON(ctx, &state, "pr", "close", reviewID, "--repo", ref.Target()); err != nil {
		return fmt.Errorf("failed to close PR #%s: %w", reviewID, err)
	}
	return nil
}

// FindReview searches for a pull request by source branch name.
// tg cannot filter by branch server-side, so the full list is scanned.
func (c *Client) FindReview(ctx context.Context, repoURI, branch string) (*forge.ReviewDetails, error) {
	ref, err := repoRef(repoURI)
	if err != nil {
		return nil, err
	}
	var list pullListJSON
	if err := c.cli.RunJSON(ctx, &list, "pr", "list", ref.Target()); err != nil {
		return nil, fmt.Errorf("failed to list PRs: %w", err)
	}
	for _, item := range list.Items {
		if item.SourceBranch == branch {
			return &forge.ReviewDetails{
				ID:    item.Rkey,
				URL:   c.reviewURL(ctx, ref, item.Rkey, item.Title, 1),
				State: mapState(item.State),
				Title: item.Title,
			}, nil
		}
	}
	return nil, nil // No review found
}

// GetReview retrieves details of a pull request. The URL is the pull request
// list rather than the numbered page: callers use GetReview for state and body
// (often once per PR in a stack), and the numbered URL is persisted from
// CreateReview/FindReview instead.
func (c *Client) GetReview(ctx context.Context, repoURI string, reviewID string) (*forge.ReviewDetails, error) {
	ref, err := repoRef(repoURI)
	if err != nil {
		return nil, err
	}
	view, err := c.viewPull(ctx, ref, reviewID)
	if err != nil {
		return nil, err
	}
	return &forge.ReviewDetails{
		ID:    reviewID,
		URL:   ref.PullsURL(),
		State: mapState(view.State),
		Title: view.Title,
		Body:  view.Body,
	}, nil
}

func (c *Client) viewPull(ctx context.Context, ref *RepoRef, reviewID string) (*pullViewJSON, error) {
	var view pullViewJSON
	if err := c.cli.RunJSON(ctx, &view, "pr", "view", reviewID, "--repo", ref.Target()); err != nil {
		return nil, fmt.Errorf("failed to get PR #%s: %w", reviewID, err)
	}
	return &view, nil
}

// UpdateReview replaces the body of a pull request.
// tg edits the record in the authenticated user's repository, so only the
// PR author can update it.
func (c *Client) UpdateReview(ctx context.Context, _ string, reviewID string, body string) error {
	if _, err := c.cli.Run(ctx, cmd.Opts{}, "pr", "edit", reviewID, "--body", body); err != nil {
		return fmt.Errorf("failed to update PR #%s: %w", reviewID, err)
	}
	return nil
}

// SyncReview submits a new round for the pull request from its current
// source branch, so the PR reflects content pushed since the last round.
func (c *Client) SyncReview(ctx context.Context, repoURI string, reviewID string) error {
	ref, err := repoRef(repoURI)
	if err != nil {
		return err
	}
	view, err := c.viewPull(ctx, ref, reviewID)
	if err != nil {
		return err
	}
	if view.TargetBranch == "" {
		return fmt.Errorf("PR #%s has no target branch, so no new round can be submitted", reviewID)
	}
	if err := c.exportRefs(ctx); err != nil {
		return err
	}
	if _, err := c.cli.Run(ctx, cmd.Opts{}, "pr", "update", reviewID, "--base", c.baseRef(view.TargetBranch)); err != nil {
		return fmt.Errorf("failed to submit new round for PR #%s: %w", reviewID, err)
	}
	return nil
}

// DefaultBranch returns the repository's default branch, discovered from the
// git remote's HEAD and cached per repository for the client's lifetime.
func (c *Client) DefaultBranch(ctx context.Context, repoURI string) (string, error) {
	c.branchMu.Lock()
	defer c.branchMu.Unlock()
	if branch, ok := c.defaultBranch[repoURI]; ok {
		return branch, nil
	}
	branch, err := DefaultBranch(ctx, c.executor, repoURI)
	if err != nil {
		return "", err
	}
	c.defaultBranch[repoURI] = branch
	return branch, nil
}

// SetupRuleset is unsupported: Tangled has no server-side commit message rules.
func (c *Client) SetupRuleset(_ context.Context, _ string) error {
	return fmt.Errorf("Tangled does not support branch rulesets, so forge-parent trailers are only checked locally")
}

// FormatID formats a pull record key into the stored ID form (e.g. "pr/3mujbgpmfg422").
func (c *Client) FormatID(reviewID string) string {
	return "pr/" + reviewID
}

// rkeyRegex matches a valid atproto record key.
var rkeyRegex = regexp.MustCompile(`^[A-Za-z0-9._:~-]{1,512}$`)

// ParseID parses a stored ID (e.g. "pr/3mujbgpmfg422") into a pull record key.
func (c *Client) ParseID(id string) (string, error) {
	id = strings.TrimPrefix(id, "pr/")
	if !rkeyRegex.MatchString(id) {
		return "", fmt.Errorf("invalid Tangled pull record key %q", id)
	}
	return id, nil
}

// FormatHeadBranch returns the head branch for a Tangled PR. Reviews are
// branch-based within the target repository, so no owner prefix is needed.
func (c *Client) FormatHeadBranch(_ context.Context, _ jj.Client, _, changeID string) (string, error) {
	return fmt.Sprintf("push-%s", changeID), nil
}

// NormalizeRepoURL converts a remote URL to Tangled's canonical HTTPS format.
func (c *Client) NormalizeRepoURL(url string) (string, error) {
	return NormalizeURL(url)
}

// SupportsForks returns false: reviews are opened as branches in the target
// repository, which requires push access. Fork-based PRs are not yet supported.
func (c *Client) SupportsForks() bool {
	return false
}

// rkeyFromURI extracts the record key from an at:// URI.
func rkeyFromURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if i := strings.LastIndex(uri, "/"); i >= 0 {
		return uri[i+1:]
	}
	return uri
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
		return forge.ReviewStateClosed // Unknown states (e.g. abandoned) are treated as closed
	}
}
