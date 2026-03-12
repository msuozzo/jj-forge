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
	"github.com/msuozzo/jj-forge/internal/ui"
)

// Run executes the configured check command against the given revset.
//
// When force is true, the command is always executed (used by standalone jj-forge change check).
// When force is false, execution is skipped if all changes already have passing
// verdicts with matching commit IDs (used by upload/submit/merge).
//
// Multiple revisions are checked in parallel by materializing them into
// persistent pool directories using the backing git store.
func Run(ctx context.Context, client jj.Client, configMgr *forge.ConfigManager, revset string, force bool, runner cmd.Executor, u *ui.UI) error {
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
	fmt.Fprintf(u, "Running checks on %d change(s)...\n", len(toCheck))
	// Build task tracker for progress display.
	taskNames := make([]string, len(toCheck))
	revIndex := make(map[string]int, len(toCheck))
	for i, rev := range toCheck {
		taskNames[i] = rev.ID
		revIndex[rev.ID] = i
	}
	tracker := ui.NewTaskTracker(u, taskNames)
	tracker.Start()
	repoRoot, err := client.Root(ctx)
	if err != nil {
		return fmt.Errorf("failed to get repo root: %w", err)
	}
	forgeDir, err := forge.Dir(repoRoot)
	if err != nil {
		return err
	}
	lock, err := acquireLockWait(ctx, forgeDir, u)
	if err != nil {
		return err
	}
	defer lock.release()
	// Write "running" verdicts before starting execution.
	var runningVerdicts []forge.CheckVerdict
	for _, rev := range toCheck {
		runningVerdicts = append(runningVerdicts, forge.CheckVerdict{
			ChangeID: rev.ID,
			Verdict:  forge.CheckVerdictRunning,
			CommitID: rev.CommitID,
		})
	}
	if err := configMgr.SetCheckVerdicts(runningVerdicts); err != nil {
		return fmt.Errorf("failed to set running verdicts: %w", err)
	}
	outstanding := slices.Clone(toCheck)
	defer func() {
		if ctx.Err() == nil {
			return
		}
		// Remove verdicts for checks that haven't yet completed.
		var ids []string
		for _, rev := range outstanding {
			ids = append(ids, rev.ID)
		}
		if len(ids) > 0 {
			configMgr.RemoveCheckVerdicts(ids)
		}
	}()
	// Initialize pool.
	gitDir, err := client.GitDir(ctx)
	if err != nil {
		return fmt.Errorf("failed to get git dir: %w", err)
	}
	baseDir := filepath.Join(forgeDir, "check-pool")
	pool, err := NewWorkPool(gitDir, baseDir, defaultPoolSize, runner)
	if err != nil {
		return fmt.Errorf("failed to create work pool: %w", err)
	}
	// Run checks in parallel, streaming verdicts as they complete.
	type result struct {
		rev *jj.Rev
		err error
	}
	resultCh := make(chan result, len(toCheck))
	for _, rev := range toCheck {
		go func() {
			wd, err := pool.Acquire(ctx)
			if err != nil {
				resultCh <- result{rev: rev, err: fmt.Errorf("failed to acquire pool directory: %w", err)}
				return
			}
			defer pool.Release(wd)
			tracker.SetStatus(revIndex[rev.ID], ui.TaskRunning)
			resultCh <- result{rev: rev, err: runInDir(ctx, pool, runner, wd, rev.CommitID, checkCmd)}
		}()
	}
	// Store verdicts as they arrive and collect errors.
	var failures []string
	for range len(toCheck) {
		var r result
		select {
		case r = <-resultCh:
		case <-ctx.Done():
			tracker.Finish()
			return ctx.Err()
		}
		verdictStr := forge.CheckVerdictPass
		taskStatus := ui.TaskDone
		if r.err != nil {
			verdictStr = forge.CheckVerdictFail
			taskStatus = ui.TaskFailed
		}
		tracker.SetStatus(revIndex[r.rev.ID], taskStatus)
		if err := configMgr.SetCheckVerdict(forge.CheckVerdict{
			ChangeID: r.rev.ID,
			Verdict:  verdictStr,
			CommitID: r.rev.CommitID,
		}); err != nil {
			tracker.Finish()
			return fmt.Errorf("failed to store check verdict: %w", err)
		}
		outstanding = slices.DeleteFunc(outstanding, func(rev *jj.Rev) bool {
			return rev.ID == r.rev.ID
		})
		if r.err != nil {
			failures = append(failures, fmt.Sprintf("%s (%s)", r.rev.ID, r.rev.CommitID))
		}
	}
	tracker.Finish()
	if len(failures) > 0 {
		return fmt.Errorf("check command failed for: %s", strings.Join(failures, ", "))
	}
	return nil
}

func runInDir(ctx context.Context, pool *WorkPool, runner cmd.Executor, wd *WorkDir, commitID, checkCmd string) error {
	if err := pool.Materialize(ctx, wd, commitID); err != nil {
		return fmt.Errorf("failed to materialize %s: %w", commitID, err)
	}

	_, runErr := runner(ctx, cmd.Opts{WorkDir: wd.Path}, "sh", "-c", checkCmd)
	return runErr
}
