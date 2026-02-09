package repoclone

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/msuozzo/jj-forge/internal/cmd"
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

// Printer handles output messages.
type Printer interface {
	Info(msg string)
	Success(msg string)
	Error(msg string)
	Step(msg string)
}

// DefaultPrinter implements Printer using stdout/stderr.
type DefaultPrinter struct{}

func (p *DefaultPrinter) Info(msg string)    { fmt.Println(msg) }
func (p *DefaultPrinter) Success(msg string) { fmt.Printf("✓ %s\n", msg) }
func (p *DefaultPrinter) Error(msg string)   { fmt.Fprintf(os.Stderr, "✗ %s\n", msg) }
func (p *DefaultPrinter) Step(msg string)    { fmt.Printf("⟳ %s\n", msg) }

// Runner orchestrates the clone operation.
type Runner struct {
	ghClient   *GitHubClient
	jjExecutor cmd.Executor
	prompter   Prompter
	printer    Printer
}

// NewRunner creates a Runner with default implementations.
func NewRunner() *Runner {
	return &Runner{
		ghClient:   NewGitHubClient(),
		jjExecutor: cmd.DefaultExecutor,
		prompter:   &cmd.DefaultPrompter{},
		printer:    &DefaultPrinter{},
	}
}

// NewRunnerWithDeps creates a Runner with custom dependencies (for testing).
func NewRunnerWithDeps(ghClient *GitHubClient, jjExecutor cmd.Executor, prompter Prompter, printer Printer) *Runner {
	return &Runner{
		ghClient:   ghClient,
		jjExecutor: jjExecutor,
		prompter:   prompter,
		printer:    printer,
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
	r.printer.Info("Analyzing repository...")

	// Parse and validate URL
	ref, err := ParseGitHubURL(params.URL)
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
		r.printer.Info("Repository doesn't exist")
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

		r.printer.Step("Creating repository...")
		cloneAnalysis, err = r.ghClient.CreateRepo(ctx, ref.Name, private)
		if err != nil {
			return nil, err
		}
		r.printer.Success(fmt.Sprintf("Created repository: github.com/%s/%s", cloneAnalysis.Owner, cloneAnalysis.Name))

	} else if analysis.IsMine {
		// Repository exists and is owned by user
		if analysis.IsFork {
			r.printer.Success("Repository owned by you")
			r.printer.Success(fmt.Sprintf("Is a fork of %s/%s", analysis.Parent.Owner, analysis.Parent.Name))
			upstreamOwner = analysis.Parent.Owner
			upstreamName = analysis.Parent.Name
		} else {
			r.printer.Success("Repository owned by you")
			r.printer.Success("Not a fork")
		}
		cloneAnalysis = analysis

	} else {
		// External repository - need to fork
		r.printer.Success(fmt.Sprintf("Repository owned by %s", analysis.Owner))

		if params.NoFork {
			return nil, fmt.Errorf("external repository requires fork (run without --no-fork)")
		}

		// Check if user already has a fork
		r.printer.Step("Checking for existing fork...")
		existingFork, err := r.ghClient.FindMyFork(ctx, analysis.Owner, analysis.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to check for existing fork: %w", err)
		}

		if existingFork != nil {
			r.printer.Success(fmt.Sprintf("Found existing fork: github.com/%s/%s", existingFork.Owner, existingFork.Name))
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

			r.printer.Step("Forking repository...")
			cloneAnalysis, err = r.ghClient.CreateFork(ctx, analysis.Owner, analysis.Name)
			if err != nil {
				return nil, err
			}
			r.printer.Success(fmt.Sprintf("Created fork: github.com/%s/%s", cloneAnalysis.Owner, cloneAnalysis.Name))
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

	// Clone the repository
	r.printer.Step("Cloning repository...")
	_, err = r.jjExecutor(ctx, cmd.Opts{}, "jj", "git", "clone", cloneURL, clonePath)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}

	// Determine workflow
	workflow := DetermineWorkflow(cloneAnalysis)

	// Configure remotes
	absClonePath, err := filepath.Abs(clonePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Rename origin to fork-remote name
	if params.ForkRemote != "origin" {
		_, err = r.jjExecutor(ctx, cmd.Opts{}, append([]string{"jj", "-R", absClonePath}, "git", "remote", "rename", "origin", params.ForkRemote)...)
		if err != nil {
			return nil, fmt.Errorf("failed to rename origin remote: %w", err)
		}
	}
	r.printer.Success(fmt.Sprintf("Added remote '%s' → %s", params.ForkRemote, cloneURL))

	// For PR workflow, add upstream remote
	result := &Result{
		ClonePath:  clonePath,
		Workflow:   workflow,
		ForkRemote: params.ForkRemote,
	}

	if workflow == WorkflowPR && upstreamOwner != "" {
		// Get upstream URLs if we don't have them
		if upstreamSSH == "" {
			upstreamSSH, upstreamHTTPS, upstreamDefaultBranch, err = r.ghClient.GetUpstreamInfo(ctx, upstreamOwner, upstreamName)
			if err != nil {
				return nil, fmt.Errorf("failed to get upstream info: %w", err)
			}
		}

		upstreamURL := upstreamSSH
		if params.UseHTTPS {
			upstreamURL = upstreamHTTPS
		}

		_, err = r.jjExecutor(ctx, cmd.Opts{}, append([]string{"jj", "-R", absClonePath}, "git", "remote", "add", params.UpstreamRemote, upstreamURL)...)
		if err != nil {
			return nil, fmt.Errorf("failed to add upstream remote: %w", err)
		}
		r.printer.Success(fmt.Sprintf("Added remote '%s' → %s (upstream)", params.UpstreamRemote, upstreamURL))
		result.UpstreamName = params.UpstreamRemote
	}

	// Configure trunk() revset alias to point to the correct remote
	// For main workflow: trunk() = "main@og" (fork remote)
	// For PR workflow: trunk() = "main@up" (upstream remote)
	var trunkRemote, defaultBranch string
	if workflow == WorkflowMain {
		trunkRemote = params.ForkRemote
		defaultBranch = cloneAnalysis.DefaultBranch
	} else {
		trunkRemote = params.UpstreamRemote
		defaultBranch = upstreamDefaultBranch
	}

	trunkAlias := fmt.Sprintf("%s@%s", defaultBranch, trunkRemote)
	_, err = r.jjExecutor(ctx, cmd.Opts{}, append([]string{"jj", "-R", absClonePath}, "config", "set", "--repo", "revset-aliases.\"trunk()\"", trunkAlias)...)
	if err != nil {
		return nil, fmt.Errorf("failed to configure trunk() alias: %w", err)
	}
	r.printer.Success(fmt.Sprintf("Configured trunk() → %s", trunkAlias))

	// Print workflow summary
	r.printer.Info("")
	if workflow == WorkflowMain {
		r.printer.Success("Configured develop-on-main workflow")
		r.printer.Info("")
		r.printer.Info("Workflow: Develop on main")
		r.printer.Info("  Use 'jj-forge change upload' to sync changes")
		r.printer.Info("  Use 'jj-forge change submit' to land to main")
	} else {
		r.printer.Success("Configured PR-based workflow")
		r.printer.Info("")
		r.printer.Info("Workflow: PR-based")
		r.printer.Info("  Use 'jj-forge review open' to create PR (auto-uploads)")
		r.printer.Info("  Use 'jj-forge review update' to sync content and update PR descriptions")
	}

	return result, nil
}

// Run is a convenience function that creates a default Runner and executes the clone.
func Run(ctx context.Context, params Params) (*Result, error) {
	runner := NewRunner()
	return runner.Run(ctx, params)
}
