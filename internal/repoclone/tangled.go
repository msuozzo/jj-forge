package repoclone

import (
	"context"
	"fmt"
	"os"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge/tangled"
	"github.com/msuozzo/jj-forge/internal/ui"
)

// TangledRunner orchestrates a Tangled-specific clone operation.
//
// Ownership is determined by comparing the repository record's owner DID with
// the DID of the tg session. Repositories you own get the develop-on-main
// workflow. Anything else gets a PR-based workflow where both remotes point
// at the same repository, since fork-based reviews are not yet supported.
type TangledRunner struct {
	cli         *tangled.CLI
	gitExecutor cmd.Executor // For `git ls-remote` default-branch discovery
	jjExecutor  cmd.Executor
	ui          *ui.UI
}

// NewTangledRunner creates a TangledRunner with default implementations.
func NewTangledRunner(u *ui.UI) *TangledRunner {
	return &TangledRunner{
		cli:         tangled.NewCLI(cmd.DefaultExecutor),
		gitExecutor: cmd.DefaultExecutor,
		jjExecutor:  cmd.DefaultExecutor,
		ui:          u,
	}
}

// NewTangledRunnerWithDeps creates a TangledRunner with custom dependencies.
func NewTangledRunnerWithDeps(cli *tangled.CLI, gitExecutor, jjExecutor cmd.Executor, u *ui.UI) *TangledRunner {
	return &TangledRunner{
		cli:         cli,
		gitExecutor: gitExecutor,
		jjExecutor:  jjExecutor,
		ui:          u,
	}
}

// Run executes the Tangled clone operation.
func (r *TangledRunner) Run(ctx context.Context, params Params) (*Result, error) {
	u := r.ui
	fmt.Fprintf(u, "Analyzing Tangled repository...\n")

	ref, err := tangled.ParseURL(params.URL)
	if err != nil {
		return nil, &ui.UserError{
			Msg:  fmt.Sprintf("invalid Tangled repository URL: %v", err),
			Hint: "Use the owner/repo form, e.g. https://tangled.org/<handle>/<repo> or git@tangled.org:<handle>/<repo>",
		}
	}

	// Identify the tg session
	status, err := r.cli.AuthStatus(ctx)
	if err != nil {
		return nil, err
	}
	if !status.Authenticated || status.DID == "" {
		return nil, &ui.UserError{
			Msg:  "tg is not logged in",
			Hint: "Run 'tg auth login <handle>' to authenticate with Tangled.",
		}
	}

	// Look up the repository record
	repo, err := r.cli.ViewRepo(ctx, ref.Target())
	if err != nil {
		return nil, err
	}
	ownerDID := repo.OwnerDID()
	isMine := ownerDID != "" && ownerDID == status.DID
	if isMine {
		fmt.Fprintf(u, "%s Repository owned by you\n", u.Styled("task_pass", "✓"))
	} else {
		fmt.Fprintf(u, "%s Repository owned by %s\n", u.Styled("task_pass", "✓"), repo.Author)
		fmt.Fprintf(u, "  Reviews will be opened as branches in this repository (requires push access)\n")
	}

	// Discover the default branch (tg does not report it)
	defaultBranch, err := tangled.DefaultBranch(ctx, r.gitExecutor, params.URL)
	if err != nil {
		return nil, err
	}

	// Determine clone path
	clonePath := params.Path
	if clonePath == "" {
		clonePath = ref.Name
	}
	if _, err := os.Stat(clonePath); err == nil {
		return nil, fmt.Errorf("directory already exists: %s", clonePath)
	}

	forkRemote := params.ForkRemote
	if forkRemote == "" {
		forkRemote = "og"
	}
	upstreamRemote := params.UpstreamRemote
	if upstreamRemote == "" {
		upstreamRemote = "up"
	}

	steps := cloneSteps{
		CloneURL:      params.URL,
		ClonePath:     clonePath,
		ForkRemote:    forkRemote,
		TrackBranches: params.TrackBranches,
		DefaultBranch: defaultBranch,
		TrunkRemote:   forkRemote,
	}
	workflow := WorkflowMain
	if !isMine {
		// PR-based: the upstream remote is the same repository, fetched so
		// that <upstream>/<branch> tracking refs exist for PR bases.
		workflow = WorkflowPR
		steps.UpstreamRemote = upstreamRemote
		steps.UpstreamURL = params.URL
		steps.FetchUpstream = true
		steps.TrunkRemote = upstreamRemote
	}
	if _, err := runCloneSteps(ctx, r.jjExecutor, u, steps); err != nil {
		return nil, err
	}

	result := &Result{
		ClonePath:  clonePath,
		Workflow:   workflow,
		ForkRemote: forkRemote,
	}
	if workflow == WorkflowMain {
		fmt.Fprintln(u)
		fmt.Fprintf(u, "%s Configured develop-on-main workflow (Tangled)\n", u.Styled("task_pass", "✓"))
		fmt.Fprintln(u)
		fmt.Fprintf(u, "Workflow: Develop on main\n")
		fmt.Fprintf(u, "  Use 'jj' to create changes and 'jj-forge change submit' to land them\n")
		fmt.Fprintf(u, "  Use 'jj-forge review open --upstream-remote %s' to review changes as PRs\n", forkRemote)
	} else {
		result.UpstreamName = upstreamRemote
		printPRWorkflowSummary(u, "Tangled")
	}
	return result, nil
}
