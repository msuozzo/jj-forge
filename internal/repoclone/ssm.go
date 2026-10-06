package repoclone

import (
	"context"
	"fmt"
	"os"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge"
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

	remoteName := params.ForkRemote
	if remoteName == "" {
		remoteName = forge.DefaultForkRemote
	}
	upstreamRemote := params.UpstreamRemote
	if upstreamRemote == "" {
		upstreamRemote = forge.DefaultUpstreamRemote
	}

	// SSM uses no forks: the upstream remote points at the same URL.
	_, err = runCloneSteps(ctx, r.jjExecutor, u, cloneSteps{
		CloneURL:       params.URL,
		ClonePath:      clonePath,
		ForkRemote:     remoteName,
		UpstreamRemote: upstreamRemote,
		UpstreamURL:    params.URL,
		TrackBranches:  params.TrackBranches,
		DefaultBranch:  "main",
		TrunkRemote:    upstreamRemote,
	})
	if err != nil {
		return nil, err
	}

	printPRWorkflowSummary(u, "SSM")

	return &Result{
		ClonePath:    clonePath,
		Workflow:     WorkflowPR,
		ForkRemote:   remoteName,
		UpstreamName: upstreamRemote,
	}, nil
}
