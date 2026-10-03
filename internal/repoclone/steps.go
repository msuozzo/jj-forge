package repoclone

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/ui"
)

// cloneSteps describes the jj commands shared by forges without a fork model:
// clone, rename the origin remote, optionally add an upstream remote, set the
// fetch/push remotes, track branches, and point trunk() at the default branch.
type cloneSteps struct {
	CloneURL       string   // URL to clone
	ClonePath      string   // Destination directory
	ForkRemote     string   // Name for the cloned remote (origin is renamed to this)
	UpstreamRemote string   // Name for the upstream remote, or empty for none
	UpstreamURL    string   // URL for the upstream remote
	TrackBranches  []string // Glob patterns to track from the fork remote
	DefaultBranch  string   // Default branch for the trunk() alias
	TrunkRemote    string   // Remote for the trunk() alias
}

// runCloneSteps executes the clone steps with a task tracker, returning the
// absolute clone path.
func runCloneSteps(ctx context.Context, jjExecutor cmd.Executor, u *ui.UI, s cloneSteps) (string, error) {
	needsUpstream := s.UpstreamRemote != "" && s.UpstreamRemote != s.ForkRemote
	needsTrackBranches := len(s.TrackBranches) > 0

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
	fail := func(task int, err error) (string, error) {
		tracker.SetStatus(task, ui.TaskFailed)
		tracker.Finish()
		return "", err
	}

	// Clone the repository
	tracker.SetStatus(taskClone, ui.TaskRunning)
	if _, err := jjExecutor(ctx, cmd.Opts{}, "jj", "git", "clone", s.CloneURL, s.ClonePath); err != nil {
		return fail(taskClone, fmt.Errorf("failed to clone repository: %w", err))
	}
	tracker.SetStatus(taskClone, ui.TaskDone)

	// Configure remotes
	tracker.SetStatus(taskRemotes, ui.TaskRunning)
	absClonePath, err := filepath.Abs(s.ClonePath)
	if err != nil {
		return fail(taskRemotes, fmt.Errorf("failed to get absolute path: %w", err))
	}
	jj := func(args ...string) error {
		_, err := jjExecutor(ctx, cmd.Opts{}, append([]string{"jj", "-R", absClonePath}, args...)...)
		return err
	}
	if s.ForkRemote != "origin" {
		if err := jj("git", "remote", "rename", "origin", s.ForkRemote); err != nil {
			return fail(taskRemotes, fmt.Errorf("failed to rename origin remote: %w", err))
		}
	}
	if needsUpstream {
		if err := jj("git", "remote", "add", s.UpstreamRemote, s.UpstreamURL); err != nil {
			return fail(taskRemotes, fmt.Errorf("failed to add upstream remote: %w", err))
		}
	}
	if needsUpstream {
		err = jj("config", "set", "--repo", "git.fetch", fmt.Sprintf("['%s', '%s']", s.UpstreamRemote, s.ForkRemote))
	} else {
		err = jj("config", "set", "--repo", "git.fetch", s.ForkRemote)
	}
	if err != nil {
		return fail(taskRemotes, fmt.Errorf("failed to set fetch remote(s): %w", err))
	}
	if err := jj("config", "set", "--repo", "git.push", s.ForkRemote); err != nil {
		return fail(taskRemotes, fmt.Errorf("failed to set push remote: %w", err))
	}
	tracker.SetStatus(taskRemotes, ui.TaskDone)

	// Track branches from fork remote
	if needsTrackBranches {
		tracker.SetStatus(taskTrackBranches, ui.TaskRunning)
		trackArgs := append([]string{"bookmark", "track"}, s.TrackBranches...)
		trackArgs = append(trackArgs, "--remote", s.ForkRemote)
		if err := jj(trackArgs...); err != nil {
			return fail(taskTrackBranches, fmt.Errorf("failed to track branches: %w", err))
		}
		tracker.SetStatus(taskTrackBranches, ui.TaskDone)
	}

	// Configure trunk() alias
	tracker.SetStatus(taskTrunk, ui.TaskRunning)
	trunkAlias := fmt.Sprintf("%s@%s", s.DefaultBranch, s.TrunkRemote)
	if err := jj("config", "set", "--repo", "revset-aliases.\"trunk()\"", trunkAlias); err != nil {
		return fail(taskTrunk, fmt.Errorf("failed to configure trunk() alias: %w", err))
	}
	tracker.SetStatus(taskTrunk, ui.TaskDone)
	tracker.Finish()
	return absClonePath, nil
}

// printPRWorkflowSummary prints the closing hint for a PR-based workflow.
func printPRWorkflowSummary(u *ui.UI, forgeLabel string) {
	fmt.Fprintln(u)
	fmt.Fprintf(u, "%s Configured PR-based workflow (%s)\n", u.Styled("task_pass", "✓"), forgeLabel)
	fmt.Fprintln(u)
	fmt.Fprintf(u, "Workflow: PR-based\n")
	fmt.Fprintf(u, "  Use 'jj-forge review open' to create PR (auto-uploads)\n")
	fmt.Fprintf(u, "  Use 'jj-forge review update' to sync content and update PR descriptions\n")
}
