package check

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// Run executes the configured check command against the given revset.
//
// When force is true, the command is always executed (used by standalone jj-forge check).
// When force is false, execution is skipped if all changes already have passing
// verdicts with matching commit IDs (used by upload/submit/merge).
//
// Multiple revisions are checked in parallel: the working copy revision (if
// present) runs in-place via `jj util exec`, while other revisions are
// materialized into persistent pool directories using the backing git store.
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
	// Ignore immutable revisions
	revs = slices.DeleteFunc(revs, func(r *jj.Rev) bool {
		return !r.IsMutable
	})
	if len(revs) == 0 {
		return nil
	}
	// Filter out revisions with cached passing verdicts (batch)
	var toCheck []*jj.Rev
	if force {
		toCheck = revs
	} else {
		verdicts, err := configMgr.GetCheckVerdicts()
		if err != nil {
			return fmt.Errorf("failed to get verdicts: %w", err)
		}
		for _, rev := range revs {
			i := slices.IndexFunc(verdicts, func(v forge.CheckVerdict) bool {
				return v.ChangeID == rev.ID
			})
			if i != -1 {
				if verdict := verdicts[i]; verdict.Verdict == forge.CheckVerdictPass && verdict.CommitID == rev.CommitID {
					continue // skip cached pass
				}
			}
			toCheck = append(toCheck, rev)
		}
	}
	if len(toCheck) == 0 {
		return nil // all cached
	}
	// Get repo path for the runner
	repoPath, err := client.Root(ctx)
	if err != nil {
		return fmt.Errorf("failed to get repo root: %w", err)
	}
	// Partition: working copy vs others
	wcRev, err := client.Rev(ctx, "@")
	if err != nil {
		return fmt.Errorf("failed to resolve working copy: %w", err)
	}
	var wcCheck *jj.Rev
	var poolChecks []*jj.Rev
	for _, rev := range toCheck {
		if rev.CommitID == wcRev.CommitID {
			wcCheck = rev
		} else {
			poolChecks = append(poolChecks, rev)
		}
	}
	// Initialize pool only if needed
	var pool *WorkPool
	if len(poolChecks) > 0 {
		gitDir, err := client.GitDir(ctx)
		if err != nil {
			return fmt.Errorf("failed to get git dir: %w", err)
		}
		baseDir := filepath.Join(repoPath, ".jj", "forge-check-pool")
		pool, err = NewWorkPool(gitDir, baseDir, defaultPoolSize, runner)
		if err != nil {
			return fmt.Errorf("failed to create work pool: %w", err)
		}
	}
	// Run checks in parallel, streaming verdicts as they complete.
	type result struct {
		rev *jj.Rev
		err error
	}
	resultCh := make(chan result, len(toCheck))
	for _, rev := range toCheck {
		go func() {
			if wcCheck != nil && rev.CommitID == wcCheck.CommitID {
				resultCh <- result{rev: rev, err: runInWorkingCopy(ctx, runner, repoPath, checkCmd)}
			} else {
				resultCh <- result{rev: rev, err: runInPool(ctx, pool, runner, rev.CommitID, checkCmd)}
			}
		}()
	}
	// Store verdicts as they arrive and collect errors.
	var failures []string
	for range len(toCheck) {
		r := <-resultCh
		verdictStr := forge.CheckVerdictPass
		if r.err != nil {
			verdictStr = forge.CheckVerdictFail
		}
		if err := configMgr.SetCheckVerdict(forge.CheckVerdict{
			ChangeID: r.rev.ID,
			Verdict:  verdictStr,
			CommitID: r.rev.CommitID,
		}); err != nil {
			return fmt.Errorf("failed to store check verdict: %w", err)
		}
		if r.err != nil {
			failures = append(failures, fmt.Sprintf("%s (%s)", r.rev.ID, r.rev.CommitID))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("check command failed for: %s", strings.Join(failures, ", "))
	}
	return nil
}

func runInWorkingCopy(ctx context.Context, runner cmd.Executor, repoPath, checkCmd string) error {
	args := []string{"jj"}
	if repoPath != "" {
		args = append(args, "-R", repoPath)
	}
	args = append(args, "util", "exec", "--", "sh", "-c", checkCmd)
	_, err := runner(ctx, cmd.Opts{}, args...)
	return err
}

func runInPool(ctx context.Context, pool *WorkPool, runner cmd.Executor, commitID, checkCmd string) error {
	wd, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire pool directory: %w", err)
	}
	defer pool.Release(wd)

	if err := pool.Materialize(ctx, wd, commitID); err != nil {
		return fmt.Errorf("failed to materialize %s: %w", commitID, err)
	}

	_, runErr := runner(ctx, cmd.Opts{WorkDir: wd.Path}, "sh", "-c", checkCmd)
	return runErr
}
