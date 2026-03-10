package repoclone

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/ui"
)

// WorkflowType represents the workflow mode for the repository.
type WorkflowType string

const (
	// WorkflowMain is for develop-on-main workflow (personal non-fork repos).
	WorkflowMain WorkflowType = "main"
	// WorkflowPR is for PR-based workflow (forks or external repos).
	WorkflowPR WorkflowType = "pr"
)

// Params holds the parameters for the clone command.
type Params struct {
	URL            string // Repository URL to clone
	Path           string // Clone to this path (empty = default to repo name)
	ForkRemote     string // Name for fork/personal remote (default: "og")
	UpstreamRemote string // Name for upstream remote (default: "up")
	UseHTTPS       bool   // Use HTTPS instead of SSH for remotes
	NoFork         bool   // Don't create fork for external repos (fail instead)
}

// Result contains the outcome of the clone operation.
type Result struct {
	ClonePath    string       // Path where repo was cloned
	Workflow     WorkflowType // Detected workflow type
	ForkRemote   string       // Name of the fork/personal remote
	UpstreamName string       // Name of the upstream remote (empty for main workflow)
}

// Prompter handles user interaction for confirmations and choices.
type Prompter interface {
	cmd.Prompter
	Choose(prompt string, options []string, defaultIndex int) (int, error)
}

// Runner orchestrates the clone operation.
type Runner struct {
	ghClient   *GitHubClient
	jjExecutor cmd.Executor
	prompter   Prompter
	ui         *ui.UI
}

// NewRunner creates a Runner with default implementations.
func NewRunner(u *ui.UI) *Runner {
	return &Runner{
		ghClient:   NewGitHubClient(),
		jjExecutor: cmd.DefaultExecutor,
		prompter:   &cmd.DefaultPrompter{},
		ui:         u,
	}
}

// NewRunnerWithDeps creates a Runner with custom dependencies (for testing).
func NewRunnerWithDeps(ghClient *GitHubClient, jjExecutor cmd.Executor, prompter Prompter, u *ui.UI) *Runner {
	return &Runner{
		ghClient:   ghClient,
		jjExecutor: jjExecutor,
		prompter:   prompter,
		ui:         u,
	}
}

// DetermineWorkflow decides workflow based on ownership and fork status.
func DetermineWorkflow(analysis *RepoAnalysis) WorkflowType {
	if analysis.IsMine && !analysis.IsFork {
		return WorkflowMain // My repo, not a fork → develop on main
	}
	return WorkflowPR // My fork OR external repo → PR-based
}

// Run executes the clone operation.
func (r *Runner) Run(ctx context.Context, params Params) (*Result, error) {
	u := r.ui
	fmt.Fprintf(u, "Analyzing repository...\n")

	// Parse and validate URL
	ref, err := forge.ParseGitURL(params.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	// Analyze repository
	analysis, err := r.ghClient.AnalyzeRepository(ctx, params.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze repository: %w", err)
	}

	// Track what we'll clone and the upstream info
	var cloneAnalysis *RepoAnalysis
	var upstreamOwner, upstreamName string
	var upstreamSSH, upstreamHTTPS, upstreamDefaultBranch string

	// Handle different scenarios
	if !analysis.Exists {
		// Repository doesn't exist
		if !analysis.IsMine {
			return nil, fmt.Errorf("repository %s/%s doesn't exist and isn't owned by you", ref.Owner, ref.Name)
		}

		// Offer to create new personal repo
		fmt.Fprintf(u, "Repository doesn't exist\n")
		create, err := r.prompter.Confirm(fmt.Sprintf("Create new repository '%s/%s'?", ref.Owner, ref.Name), true)
		if err != nil {
			return nil, err
		}
		if !create {
			return nil, fmt.Errorf("repository creation cancelled")
		}

		// Ask for visibility
		visIdx, err := r.prompter.Choose("Visibility:", []string{"Private", "Public"}, 0)
		if err != nil {
			return nil, err
		}
		private := visIdx == 0

		fmt.Fprintf(u, "Creating repository...\n")
		cloneAnalysis, err = r.ghClient.CreateRepo(ctx, ref.Name, private)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(u, "%s Created repository: github.com/%s/%s\n", u.Styled("task_pass", "✓"), cloneAnalysis.Owner, cloneAnalysis.Name)

	} else if analysis.IsMine {
		// Repository exists and is owned by user
		if analysis.IsFork {
			fmt.Fprintf(u, "%s Repository owned by you\n", u.Styled("task_pass", "✓"))
			fmt.Fprintf(u, "%s Is a fork of %s/%s\n", u.Styled("task_pass", "✓"), analysis.Parent.Owner, analysis.Parent.Name)
			upstreamOwner = analysis.Parent.Owner
			upstreamName = analysis.Parent.Name
		} else {
			fmt.Fprintf(u, "%s Repository owned by you\n", u.Styled("task_pass", "✓"))
			fmt.Fprintf(u, "%s Not a fork\n", u.Styled("task_pass", "✓"))
		}
		cloneAnalysis = analysis

	} else {
		// External repository - need to fork
		fmt.Fprintf(u, "%s Repository owned by %s\n", u.Styled("task_pass", "✓"), analysis.Owner)

		if params.NoFork {
			return nil, fmt.Errorf("external repository requires fork (run without --no-fork)")
		}

		// Check if user already has a fork
		fmt.Fprintf(u, "Checking for existing fork...\n")
		existingFork, err := r.ghClient.FindMyFork(ctx, analysis.Owner, analysis.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to check for existing fork: %w", err)
		}

		if existingFork != nil {
			fmt.Fprintf(u, "%s Found existing fork: github.com/%s/%s\n", u.Styled("task_pass", "✓"), existingFork.Owner, existingFork.Name)
			cloneAnalysis = existingFork
		} else {
			// Offer to create fork
			create, err := r.prompter.Confirm("You don't have a fork. Create one?", true)
			if err != nil {
				return nil, err
			}
			if !create {
				return nil, fmt.Errorf("fork creation cancelled")
			}

			fmt.Fprintf(u, "Forking repository...\n")
			cloneAnalysis, err = r.ghClient.CreateFork(ctx, analysis.Owner, analysis.Name)
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(u, "%s Created fork: github.com/%s/%s\n", u.Styled("task_pass", "✓"), cloneAnalysis.Owner, cloneAnalysis.Name)
		}

		upstreamOwner = analysis.Owner
		upstreamName = analysis.Name
	}

	// Determine clone path
	clonePath := params.Path
	if clonePath == "" {
		clonePath = cloneAnalysis.Name
	}

	// Check if path already exists
	if _, err := os.Stat(clonePath); err == nil {
		return nil, fmt.Errorf("directory already exists: %s", clonePath)
	}

	// Select URL based on preference
	cloneURL := cloneAnalysis.SSHURL
	if params.UseHTTPS {
		cloneURL = cloneAnalysis.HTTPSURL
	}

	// Determine workflow
	workflow := DetermineWorkflow(cloneAnalysis)
	needsUpstream := workflow == WorkflowPR && upstreamOwner != ""

	// Build task list for the work phase
	taskNames := []string{"Clone", "Configure remotes"}
	if needsUpstream {
		taskNames = append(taskNames, "Add upstream")
	}
	taskNames = append(taskNames, "Configure trunk()")

	const (
		taskClone   = 0
		taskRemotes = 1
	)
	taskUpstream := -1
	taskTrunk := 2
	if needsUpstream {
		taskUpstream = 2
		taskTrunk = 3
	}

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

	// Rename origin to fork-remote name
	if params.ForkRemote != "origin" {
		_, err = r.jjExecutor(ctx, cmd.Opts{}, "jj", "-R", absClonePath, "git", "remote", "rename", "origin", params.ForkRemote)
		if err != nil {
			tracker.SetStatus(taskRemotes, ui.TaskFailed)
			tracker.Finish()
			return nil, fmt.Errorf("failed to rename origin remote: %w", err)
		}
		_, err = r.jjExecutor(ctx, cmd.Opts{}, "jj", "-R", absClonePath, "config", "set", "--repo", "git.push", params.ForkRemote)
		if err != nil {
			tracker.SetStatus(taskRemotes, ui.TaskFailed)
			tracker.Finish()
			return nil, fmt.Errorf("failed to set push remote: %w", err)
		}
	}
	tracker.SetStatus(taskRemotes, ui.TaskDone)

	// For PR workflow, add upstream remote
	result := &Result{
		ClonePath:  clonePath,
		Workflow:   workflow,
		ForkRemote: params.ForkRemote,
	}

	if needsUpstream {
		tracker.SetStatus(taskUpstream, ui.TaskRunning)
		// Get upstream URLs if we don't have them
		if upstreamSSH == "" {
			upstreamSSH, upstreamHTTPS, upstreamDefaultBranch, err = r.ghClient.GetUpstreamInfo(ctx, upstreamOwner, upstreamName)
			if err != nil {
				tracker.SetStatus(taskUpstream, ui.TaskFailed)
				tracker.Finish()
				return nil, fmt.Errorf("failed to get upstream info: %w", err)
			}
		}

		upstreamURL := upstreamSSH
		if params.UseHTTPS {
			upstreamURL = upstreamHTTPS
		}

		_, err = r.jjExecutor(ctx, cmd.Opts{}, "jj", "-R", absClonePath, "git", "remote", "add", params.UpstreamRemote, upstreamURL)
		if err != nil {
			tracker.SetStatus(taskUpstream, ui.TaskFailed)
			tracker.Finish()
			return nil, fmt.Errorf("failed to add upstream remote: %w", err)
		}
		_, err = r.jjExecutor(ctx, cmd.Opts{}, "jj", "-R", absClonePath, "git", "fetch", "--remote", params.UpstreamRemote)
		if err != nil {
			tracker.SetStatus(taskUpstream, ui.TaskFailed)
			tracker.Finish()
			return nil, fmt.Errorf("failed to fetch from upstream: %w", err)
		}
		result.UpstreamName = params.UpstreamRemote
		tracker.SetStatus(taskUpstream, ui.TaskDone)
	}

	// Configure fetch remotes
	tracker.SetStatus(taskTrunk, ui.TaskRunning)
	if result.UpstreamName != "" {
		_, err = r.jjExecutor(ctx, cmd.Opts{}, "jj", "-R", absClonePath, "config", "set", "--repo", "git.fetch", fmt.Sprintf("['%s', '%s']", result.UpstreamName, result.ForkRemote))
	} else {
		_, err = r.jjExecutor(ctx, cmd.Opts{}, "jj", "-R", absClonePath, "config", "set", "--repo", "git.fetch", result.ForkRemote)
	}
	if err != nil {
		tracker.SetStatus(taskTrunk, ui.TaskFailed)
		tracker.Finish()
		return nil, fmt.Errorf("failed to set fetch remote(s): %w", err)
	}

	// Configure trunk() revset alias to point to the correct remote
	var trunkRemote, defaultBranch string
	if workflow == WorkflowMain {
		trunkRemote = params.ForkRemote
		defaultBranch = cloneAnalysis.DefaultBranch
	} else {
		trunkRemote = params.UpstreamRemote
		defaultBranch = upstreamDefaultBranch
	}

	trunkAlias := fmt.Sprintf("%s@%s", defaultBranch, trunkRemote)
	_, err = r.jjExecutor(ctx, cmd.Opts{}, "jj", "-R", absClonePath, "config", "set", "--repo", "revset-aliases.\"trunk()\"", trunkAlias)
	if err != nil {
		tracker.SetStatus(taskTrunk, ui.TaskFailed)
		tracker.Finish()
		return nil, fmt.Errorf("failed to configure trunk() alias: %w", err)
	}
	tracker.SetStatus(taskTrunk, ui.TaskDone)
	tracker.Finish()

	// Print workflow summary
	fmt.Fprintln(u)
	if workflow == WorkflowMain {
		fmt.Fprintf(u, "%s Configured develop-on-main workflow\n", u.Styled("task_pass", "✓"))
		fmt.Fprintln(u)
		fmt.Fprintf(u, "Workflow: Develop on main\n")
		fmt.Fprintf(u, "  Use 'jj' to create changes and 'jj-forge change submit' to land them\n")
	} else {
		fmt.Fprintf(u, "%s Configured PR-based workflow\n", u.Styled("task_pass", "✓"))
		fmt.Fprintln(u)
		fmt.Fprintf(u, "Workflow: PR-based\n")
		fmt.Fprintf(u, "  Use 'jj-forge review open' to create PR (auto-uploads)\n")
		fmt.Fprintf(u, "  Use 'jj-forge review update' to sync content and update PR descriptions\n")
	}

	return result, nil
}

// Run is a convenience function that creates a default Runner and executes the clone.
func Run(ctx context.Context, params Params, u *ui.UI) (*Result, error) {
	runner := NewRunner(u)
	return runner.Run(ctx, params)
}
