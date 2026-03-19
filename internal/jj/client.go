package jj

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/msuozzo/jj-forge/internal/cmd"
)

// Rev holds detailed information about a single revision.
type Rev struct {
	ID              string
	CommitID        string
	IsMutable       bool
	IsConflicted    bool
	IsDivergent     bool
	IsEmpty         bool
	Description     string
	Parents         []string
	Bookmarks       []string // e.g., ["push-abc123", "main"]
	RemoteBookmarks []string // e.g., ["og/push-abc123", "origin/main"]
}

// Client defines the interface for interacting with Jujutsu.
type Client interface {
	Run(context.Context, ...string) (string, error)
	Root(context.Context) (string, error)
	Revs(context.Context, string) ([]*Rev, error)
	Rev(context.Context, string) (*Rev, error)
	RemoteURL(context.Context, string) (string, error)
	GitDir(context.Context) (string, error)
}

type client struct {
	repository string
	executor   cmd.Executor
}

// NewClient creates a client with the default executor.
func NewClient(repository string) Client {
	return &client{
		repository: repository,
		executor:   cmd.DefaultExecutor,
	}
}

// NewClientWithExecutor creates a client with a custom executor.
func NewClientWithExecutor(repository string, exec cmd.Executor) Client {
	return &client{
		repository: repository,
		executor:   exec,
	}
}

// Run executes a jj command and returns its output.
func (j *client) Run(ctx context.Context, args ...string) (string, error) {
	if j.repository != "" {
		args = append([]string{"-R", j.repository}, args...)
	}
	result, err := j.executor(ctx, cmd.Opts{}, append([]string{"jj"}, args...)...)
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

// Root returns the repo root path.
func (j *client) Root(ctx context.Context) (abspath string, err error) {
	rootPath, err := j.Run(ctx, "root")
	if err != nil {
		return "", fmt.Errorf("failed to get root path: %w", err)
	}
	return strings.TrimSpace(rootPath), nil
}

// Revs returns detailed information for all revisions in the specified revset.
func (j *client) Revs(ctx context.Context, revset string) ([]*Rev, error) {
	tplParts := []string{
		"change_id.short()",
		"commit_id.short()",
		"conflict",
		"divergent",
		"!immutable",
		"empty",
		`parents.map(|c| c.change_id().short()).join(",")`,
		`bookmarks.map(|b| b.name()).join(",")`,
		`remote_bookmarks.map(|b| b.remote() ++ "/" ++ b.name()).join(",")`,
		"description.escape_json()",
		`"\n"`,
	}
	template := strings.Join(tplParts, `++" "++`)
	out, err := j.Run(ctx, "log", "--no-graph", "--template", template, "-r", revset)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit info for %s: %w", revset, err)
	}
	var revs []*Rev
	if strings.TrimSpace(out) == "" {
		return revs, nil
	}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, " ", len(tplParts)-1)
		if len(parts) < len(tplParts)-1 {
			return nil, fmt.Errorf("unexpected log entry format: %q", line)
		}
		var description string
		if err := json.Unmarshal([]byte(parts[9]), &description); err != nil {
			return nil, fmt.Errorf("bad json encoding: %w", err)
		}
		revs = append(revs, &Rev{
			ID:              parts[0],
			CommitID:        parts[1],
			IsConflicted:    parts[2] == "true",
			IsDivergent:     parts[3] == "true",
			IsMutable:       parts[4] == "true",
			IsEmpty:         parts[5] == "true",
			Parents:         splitNonEmpty(parts[6], ","),
			Bookmarks:       splitNonEmpty(parts[7], ","),
			RemoteBookmarks: splitNonEmpty(parts[8], ","),
			Description:     description,
		})
	}
	return revs, nil
}

// splitNonEmpty splits a string but returns nil for empty input.
func splitNonEmpty(s, sep string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, sep)
}

// Rev returns detailed information for a single revision.
func (j *client) Rev(ctx context.Context, revset string) (*Rev, error) {
	r, err := j.Revs(ctx, revset)
	if err != nil {
		return nil, err
	}
	if len(r) != 1 {
		return nil, fmt.Errorf("failed to get one revision for revset %s (got %d)", revset, len(r))
	}
	return r[0], nil
}

// RemoteURL returns the URL for a given git remote.
func (j *client) RemoteURL(ctx context.Context, remote string) (string, error) {
	out, err := j.Run(ctx, "git", "remote", "list")
	if err != nil {
		return "", fmt.Errorf("failed to list remotes: %w", err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == remote {
			return parts[1], nil
		}
	}
	return "", fmt.Errorf("remote %q not found", remote)
}

// GitDir returns the absolute path to the backing git directory.
func (j *client) GitDir(ctx context.Context) (string, error) {
	out, err := j.Run(ctx, "git", "root")
	if err != nil {
		return "", fmt.Errorf("failed to get git root: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("git root is empty")
	}
	return out, nil
}
