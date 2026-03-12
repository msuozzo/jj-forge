package repoclone

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge/ssm"
	"github.com/msuozzo/jj-forge/internal/ui"
)

// SSMRunner orchestrates an SSM-specific clone operation.
// SSM repos are always PR-based (no fork model), so the flow is simpler
// than GitHub: clone, rename remote, set trunk() alias, done.
type SSMRunner struct {
	jjExecutor cmd.Executor
	ui         *ui.UI
}

// NewSSMRunner creates an SSMRunner with default implementations.
func NewSSMRunner(u *ui.UI) *SSMRunner {
	return &SSMRunner{
		jjExecutor: cmd.DefaultExecutor,
		ui:         u,
	}
}

// NewSSMRunnerWithDeps creates an SSMRunner with custom dependencies (for testing).
func NewSSMRunnerWithDeps(jjExecutor cmd.Executor, u *ui.UI) *SSMRunner {
	return &SSMRunner{
		jjExecutor: jjExecutor,
		ui:         u,
	}
}

// Run executes the SSM clone operation.
func (r *SSMRunner) Run(ctx context.Context, params Params) (*Result, error) {
	u := r.ui
	fmt.Fprintf(u, "Analyzing SSM repository...\n")

	// Parse and validate URL
	_, _, _, repo, err := ssm.ParseSSMURL(params.URL)
	if err != nil {
		return nil, fmt.Errorf(
			"this Source Manager URL is not in a git-compatible format;\n" +
				"use a URL with a -git or -ssh subdomain, e.g.:\n" +
				"  https://<location>-git.<location>.sourcemanager.dev/<project>/<repo>")
	}

	// Determine clone path
	clonePath := params.Path
	if clonePath == "" {
		clonePath = repo
	}

	// Check if path already exists
	if _, err := os.Stat(clonePath); err == nil {
		return nil, fmt.Errorf("directory already exists: %s", clonePath)
	}

	cloneURL := params.URL
	remoteName := params.ForkRemote
	if remoteName == "" {
		remoteName = "og"
	}
	upstreamRemote := params.UpstreamRemote
	if upstreamRemote == "" {
		upstreamRemote = "up"
	}

	// Build task list
	needsTrackBranches := len(params.TrackBranches) > 0
	taskNames := []string{"Clone", "Configure remotes"}
	if needsTrackBranches {
		taskNames = append(taskNames, "Track branches")
	}
	taskNames = append(taskNames, "Configure trunk()")
	const (
		taskClone   = 0
		taskRemotes = 1
	)
	nextTask := 2
	taskTrackBranches := -1
	if needsTrackBranches {
		taskTrackBranches = nextTask
		nextTask++
	}
	taskTrunk := nextTask

	fmt.Fprintf(u, "Cloning and configuring...\n")
	tracker := ui.NewTaskTracker(u, taskNames)
	tracker.Start()

	// Clone the repository
	tracker.SetStatus(taskClone, ui.TaskRunning)
	_, err = r.jjExecutor(ctx, cmd.Opts{}, "jj", "git", "clone", cloneURL, clonePath)
	if err != nil {
		tracker.SetStatus(taskClone, ui.TaskFailed)
		tracker.Finish()
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}
	tracker.SetStatus(taskClone, ui.TaskDone)

	// Configure remotes
	tracker.SetStatus(taskRemotes, ui.TaskRunning)
	absClonePath, err := filepath.Abs(clonePath)
	if err != nil {
		tracker.SetStatus(taskRemotes, ui.TaskFailed)
		tracker.Finish()
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	if remoteName != "origin" {
		_, err = r.jjExecutor(ctx, cmd.Opts{}, append([]string{"jj", "-R", absClonePath}, "git", "remote", "rename", "origin", remoteName)...)
		if err != nil {
			tracker.SetStatus(taskRemotes, ui.TaskFailed)
			tracker.Finish()
			return nil, fmt.Errorf("failed to rename origin remote: %w", err)
		}
	}
	// Add upstream remote pointing to the same URL (SSM uses no forks)
	if upstreamRemote != remoteName {
		_, err = r.jjExecutor(ctx, cmd.Opts{}, append([]string{"jj", "-R", absClonePath}, "git", "remote", "add", upstreamRemote, cloneURL)...)
		if err != nil {
			tracker.SetStatus(taskRemotes, ui.TaskFailed)
			tracker.Finish()
			return nil, fmt.Errorf("failed to add upstream remote: %w", err)
		}
	}
	// Configure git.fetch and git.push
	if upstreamRemote != remoteName {
		_, err = r.jjExecutor(ctx, cmd.Opts{}, append([]string{"jj", "-R", absClonePath}, "config", "set", "--repo", "git.fetch", fmt.Sprintf("['%s', '%s']", upstreamRemote, remoteName))...)
	} else {
		_, err = r.jjExecutor(ctx, cmd.Opts{}, append([]string{"jj", "-R", absClonePath}, "config", "set", "--repo", "git.fetch", remoteName)...)
	}
	if err != nil {
		tracker.SetStatus(taskRemotes, ui.TaskFailed)
		tracker.Finish()
		return nil, fmt.Errorf("failed to set fetch remote(s): %w", err)
	}
	_, err = r.jjExecutor(ctx, cmd.Opts{}, append([]string{"jj", "-R", absClonePath}, "config", "set", "--repo", "git.push", remoteName)...)
	if err != nil {
		tracker.SetStatus(taskRemotes, ui.TaskFailed)
		tracker.Finish()
		return nil, fmt.Errorf("failed to set push remote: %w", err)
	}
	tracker.SetStatus(taskRemotes, ui.TaskDone)

	// Track branches from fork remote
	if needsTrackBranches {
		tracker.SetStatus(taskTrackBranches, ui.TaskRunning)
		trackArgs := append([]string{"jj", "-R", absClonePath, "bookmark", "track"}, params.TrackBranches...)
		trackArgs = append(trackArgs, "--remote", remoteName)
		_, err = r.jjExecutor(ctx, cmd.Opts{}, trackArgs...)
		if err != nil {
			tracker.SetStatus(taskTrackBranches, ui.TaskFailed)
			tracker.Finish()
			return nil, fmt.Errorf("failed to track branches: %w", err)
		}
		tracker.SetStatus(taskTrackBranches, ui.TaskDone)
	}

	// Configure trunk() alias
	tracker.SetStatus(taskTrunk, ui.TaskRunning)
	defaultBranch := "main"
	trunkAlias := fmt.Sprintf("%s@%s", defaultBranch, upstreamRemote)
	_, err = r.jjExecutor(ctx, cmd.Opts{}, append([]string{"jj", "-R", absClonePath}, "config", "set", "--repo", "revset-aliases.\"trunk()\"", trunkAlias)...)
	if err != nil {
		tracker.SetStatus(taskTrunk, ui.TaskFailed)
		tracker.Finish()
		return nil, fmt.Errorf("failed to configure trunk() alias: %w", err)
	}
	tracker.SetStatus(taskTrunk, ui.TaskDone)
	tracker.Finish()

	// Print workflow summary
	fmt.Fprintln(u)
	fmt.Fprintf(u, "%s Configured PR-based workflow (SSM)\n", u.Styled("task_pass", "✓"))
	fmt.Fprintln(u)
	fmt.Fprintf(u, "Workflow: PR-based\n")
	fmt.Fprintf(u, "  Use 'jj-forge review open' to create PR (auto-uploads)\n")
	fmt.Fprintf(u, "  Use 'jj-forge review update' to sync content and update PR descriptions\n")

	return &Result{
		ClonePath:    clonePath,
		Workflow:     WorkflowPR,
		ForkRemote:   remoteName,
		UpstreamName: upstreamRemote,
	}, nil
}
