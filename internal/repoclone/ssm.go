package repoclone

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge/ssm"
)

// SSMRunner orchestrates an SSM-specific clone operation.
// SSM repos are always PR-based (no fork model), so the flow is simpler
// than GitHub: clone, rename remote, set trunk() alias, done.
type SSMRunner struct {
	jjExecutor cmd.Executor
	printer    Printer
}

// NewSSMRunner creates an SSMRunner with default implementations.
func NewSSMRunner() *SSMRunner {
	return &SSMRunner{
		jjExecutor: cmd.DefaultExecutor,
		printer:    &DefaultPrinter{},
	}
}

// NewSSMRunnerWithDeps creates an SSMRunner with custom dependencies (for testing).
func NewSSMRunnerWithDeps(jjExecutor cmd.Executor, printer Printer) *SSMRunner {
	return &SSMRunner{
		jjExecutor: jjExecutor,
		printer:    printer,
	}
}

// Run executes the SSM clone operation.
func (r *SSMRunner) Run(ctx context.Context, params Params) (*Result, error) {
	r.printer.Info("Analyzing SSM repository...")

	// Parse and validate URL
	_, _, _, repo, err := ssm.ParseSSMURL(params.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid SSM repository URL: %w", err)
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

	// Clone the repository
	r.printer.Step("Cloning repository...")
	cloneURL := params.URL
	_, err = r.jjExecutor(ctx, cmd.Opts{}, "jj", "git", "clone", cloneURL, clonePath)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}

	absClonePath, err := filepath.Abs(clonePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Rename origin to the configured remote name
	remoteName := params.ForkRemote
	if remoteName == "" {
		remoteName = "og"
	}
	if remoteName != "origin" {
		_, err = r.jjExecutor(ctx, cmd.Opts{}, append([]string{"jj", "-R", absClonePath}, "git", "remote", "rename", "origin", remoteName)...)
		if err != nil {
			return nil, fmt.Errorf("failed to rename origin remote: %w", err)
		}
	}
	r.printer.Success(fmt.Sprintf("Added remote '%s' → %s", remoteName, cloneURL))

	// Determine default branch. Use "main" as a sensible default since we
	// don't have a running SSM client in the clone flow.
	defaultBranch := "main"

	// Configure trunk() alias
	trunkAlias := fmt.Sprintf("%s@%s", defaultBranch, remoteName)
	_, err = r.jjExecutor(ctx, cmd.Opts{}, append([]string{"jj", "-R", absClonePath}, "config", "set", "--repo", "revset-aliases.\"trunk()\"", trunkAlias)...)
	if err != nil {
		return nil, fmt.Errorf("failed to configure trunk() alias: %w", err)
	}
	r.printer.Success(fmt.Sprintf("Configured trunk() → %s", trunkAlias))

	// Print workflow summary
	r.printer.Info("")
	r.printer.Success("Configured PR-based workflow (SSM)")
	r.printer.Info("")
	r.printer.Info("Workflow: PR-based")
	r.printer.Info("  Use 'jj-forge review open' to create PR (auto-uploads)")
	r.printer.Info("  Use 'jj-forge review update' to sync content and update PR descriptions")

	return &Result{
		ClonePath:  clonePath,
		Workflow:   WorkflowPR,
		ForkRemote: remoteName,
	}, nil
}
