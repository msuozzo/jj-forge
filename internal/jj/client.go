package jj

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Executor defines the function signature for running shell commands.
type Executor func(ctx context.Context, args ...string) (stdout string, err error)

// defaultExecutor implements Executor using os/exec to run "jj".
func defaultExecutor(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "jj", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("command failed: jj %s\nerror: %w\nstderr: %s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

// Rev holds detailed information about a single revision.
type Rev struct {
	ID              string
	IsMutable       bool
	IsConflicted    bool
	IsDivergent     bool
	IsEmpty         bool
	Description     string
	Parents         []string
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
	executor   Executor
}

// NewClient creates a client with the default executor.
func NewClient(repository string) Client {
	return &client{
		repository: repository,
		executor:   defaultExecutor,
	}
}

// NewClientWithExecutor creates a client with a custom executor.
func NewClientWithExecutor(repository string, exec Executor) Client {
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
	return j.executor(ctx, args...)
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
		"conflict",
		"divergent",
		"!immutable",
		"empty",
		`parents.map(|c| c.change_id().short()).join(",")`,
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
		if err := json.Unmarshal([]byte(parts[7]), &description); err != nil {
			return nil, fmt.Errorf("bad json encoding: %w", err)
		}
		revs = append(revs, &Rev{
			ID:              parts[0],
			IsConflicted:    parts[1] == "true",
			IsDivergent:     parts[2] == "true",
			IsMutable:       parts[3] == "true",
			IsEmpty:         parts[4] == "true",
			Parents:         splitNonEmpty(parts[5], ","),
			RemoteBookmarks: splitNonEmpty(parts[6], ","),
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
