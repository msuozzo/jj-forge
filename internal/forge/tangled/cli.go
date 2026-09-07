package tangled

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/msuozzo/jj-forge/internal/cmd"
)

// CLI invokes the tg command-line tool (https://tangled.org/aly.codes/tg).
//
// tg invocations use the global --json flag it exposes. tg runs git itself to
// build patches, so GIT_DIR must be injected, similarly to the gh client.
type CLI struct {
	executor cmd.Executor
	tgCmd    string // Custom tg binary (defaults to "tg")
	gitDir   string // Path to the git directory for GIT_DIR (optional)
}

// NewCLI creates a tg runner using the given executor.
func NewCLI(executor cmd.Executor) *CLI {
	return &CLI{executor: executor}
}

// WithCommand configures a custom tg binary name (instead of "tg").
func (c *CLI) WithCommand(name string) *CLI {
	if name != "" {
		c.tgCmd = name
	}
	return c
}

// WithGitDir sets the git directory exported as GIT_DIR to tg.
func (c *CLI) WithGitDir(dir string) *CLI {
	c.gitDir = dir
	return c
}

// Run executes tg with the given arguments and returns stdout.
func (c *CLI) Run(ctx context.Context, opts cmd.Opts, args ...string) (string, error) {
	if c.gitDir != "" {
		opts.Env = append(opts.Env, "GIT_DIR="+c.gitDir)
	}
	bin := "tg"
	if c.tgCmd != "" {
		bin = c.tgCmd
	}
	result, err := c.executor(ctx, opts, append([]string{bin}, args...)...)
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

// RunJSON executes tg with --json appended and decodes stdout into v.
func (c *CLI) RunJSON(ctx context.Context, v any, args ...string) error {
	output, err := c.Run(ctx, cmd.Opts{}, append(args, "--json")...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(output), v); err != nil {
		return fmt.Errorf("failed to parse tg output for %q: %w", strings.Join(args, " "), err)
	}
	return nil
}

// AuthStatus describes the active tg session.
type AuthStatus struct {
	Authenticated bool   `json:"authenticated"`
	Status        string `json:"status,omitempty"` // active, expired, unknown
	DID           string `json:"did,omitempty"`
	Handle        string `json:"handle,omitempty"`
}

// AuthStatus returns the active tg session (tg auth status). A missing
// session is reported with Authenticated=false rather than an error.
func (c *CLI) AuthStatus(ctx context.Context) (*AuthStatus, error) {
	var status AuthStatus
	if err := c.RunJSON(ctx, &status, "auth", "status"); err != nil {
		return nil, fmt.Errorf("failed to check tg auth status: %w", err)
	}
	return &status, nil
}

// RepoInfo describes a Tangled repository record (tg repo view).
type RepoInfo struct {
	Name        string `json:"name"`
	URI         string `json:"uri"` // at://<owner DID>/sh.tangled.repo/<rkey>
	Author      string `json:"author"`
	Knot        string `json:"knot"`
	Description string `json:"description,omitempty"`
	RepoDID     string `json:"repoDid,omitempty"`
}

// OwnerDID returns the owner DID from the record's at:// URI.
func (r *RepoInfo) OwnerDID() string {
	rest := strings.TrimPrefix(r.URI, "at://")
	did, _, _ := strings.Cut(rest, "/")
	return did
}

// ViewRepo fetches the repository record for an "owner/repo" target.
func (c *CLI) ViewRepo(ctx context.Context, target string) (*RepoInfo, error) {
	var info RepoInfo
	if err := c.RunJSON(ctx, &info, "repo", "view", target); err != nil {
		return nil, fmt.Errorf("failed to view repository %s: %w", target, err)
	}
	return &info, nil
}

// DefaultBranch discovers a repository's default branch by asking the git
// remote for its HEAD symref. tg does not report the default branch, and this
// works for any git host without forge-specific API access.
func DefaultBranch(ctx context.Context, executor cmd.Executor, remoteURL string) (string, error) {
	opts := cmd.Opts{Env: []string{"GIT_TERMINAL_PROMPT=0"}}
	result, err := executor(ctx, opts, "git", "ls-remote", "--symref", remoteURL, "HEAD")
	if err != nil {
		return "", fmt.Errorf("failed to query default branch of %s: %w", remoteURL, err)
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		// Format: "ref: refs/heads/<branch>\tHEAD"
		if !strings.HasPrefix(line, "ref: ") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "ref: "))
		if len(fields) >= 2 && fields[1] == "HEAD" {
			if branch, ok := strings.CutPrefix(fields[0], "refs/heads/"); ok && branch != "" {
				return branch, nil
			}
		}
	}
	return "", fmt.Errorf("remote %s did not advertise a default branch", remoteURL)
}
