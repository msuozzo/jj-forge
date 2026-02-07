package check

import (
	"context"
	"fmt"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// Run executes the configured check command against the given revset.
//
// When force is true, the command is always executed (used by standalone jj-forge check).
// When force is false, execution is skipped if all changes already have passing
// verdicts with matching commit IDs (used by upload/submit/merge).
func Run(ctx context.Context, client jj.Client, configMgr *forge.ConfigManager, revset string, force bool, runner cmd.Executor) error {
	// Read check command from config
	checkCmd, err := configMgr.GetCheckCommand()
	if err != nil {
		return fmt.Errorf("failed to get check command: %w", err)
	}
	if checkCmd == "" {
		return nil // No check command configured, nothing to do
	}
	// Resolve revisions from revset
	revs, err := client.Revs(ctx, revset)
	if err != nil {
		return fmt.Errorf("failed to resolve revset %q: %w", revset, err)
	}
	if len(revs) == 0 {
		return nil // No revisions to check
	}
	// TODO: Support checking multiple revisions once jj has a `jj run`-style command runner.
	if len(revs) > 1 {
		return fmt.Errorf("check currently supports only a single revision, got %d", len(revs))
	}
	rev := revs[0]
	// Check stored verdict (skip if passing and not forced)
	if !force {
		verdict, err := configMgr.GetCheckVerdictByChangeID(rev.ID)
		if err != nil {
			return fmt.Errorf("failed to get check verdict: %w", err)
		}
		if verdict != nil && verdict.Verdict == forge.CheckVerdictPass && verdict.CommitID == rev.CommitID {
			return nil
		}
	}
	// Get repo path for the runner
	repoPath, err := client.Root(ctx)
	if err != nil {
		return fmt.Errorf("failed to get repo root: %w", err)
	}
	// TODO: Execute at the specific revision once multi-rev support lands.
	args := []string{"jj"}
	if repoPath != "" {
		args = append(args, "-R", repoPath)
	}
	args = append(args, "util", "exec", "--", "sh", "-c", checkCmd)
	_, runErr := runner(ctx, cmd.Opts{}, args...)
	// Store verdict
	verdictStr := forge.CheckVerdictPass
	if runErr != nil {
		verdictStr = forge.CheckVerdictFail
	}
	if err := configMgr.SetCheckVerdict(forge.CheckVerdict{
		ChangeID: rev.ID,
		Verdict:  verdictStr,
		CommitID: rev.CommitID,
	}); err != nil {
		return fmt.Errorf("failed to store check verdict: %w", err)
	}
	if runErr != nil {
		return fmt.Errorf("check command failed: %w", runErr)
	}
	return nil
}
