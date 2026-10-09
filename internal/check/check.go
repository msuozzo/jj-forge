package check

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
	"github.com/msuozzo/jj-forge/internal/ui"
)

// driftPollInterval controls how often the drift watcher polls jj for
// commit ID changes. Exported for testing.
var driftPollInterval = 1 * time.Second

// Run executes the configured check command against the given revset.
//
// When force is true, the command is always executed (used by standalone jj-forge change check).
// When force is false, execution is skipped if all changes already have passing
// verdicts with matching commit IDs (used by upload/submit/merge).
//
// Multiple revisions are checked in parallel by materializing them into
// persistent pool directories using the backing git store.
//
// A background watcher polls jj for commit ID drift. If a change's commit ID
// changes while its check is pending or running (e.g. user amended), the
// goroutine is cancelled and its pool slot freed.
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
	// Filter out revisions with cached passing verdicts. On a read error,
	// check everything.
	cached, _ := configMgr.GetCheckVerdicts()
	toCheck := filterCached(revs, cached, force)
	if len(toCheck) == 0 {
		return nil // all cached
	}
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
		if ctx.Err() != nil {
			return errInterrupted()
		}
		return err
	}
	defer lock.Unlock()

	// The previous holder may have passed some of these while we waited. Drop
	// those and mark the rest running in one update, so no other process's
	// verdict lands in between. Verdicts for changes jj log no longer labels
	// go too, so they don't pile up.
	stale := staleVerdicts(ctx, client, cached)
	queued := toCheck
	err = configMgr.Update(func(s *forge.State) error {
		s.RemoveChecks(stale...)
		toCheck = filterCached(queued, s.Checks, force)
		for _, rev := range toCheck {
			s.SetCheck(forge.CheckVerdict{
				ChangeID: rev.ID,
				Verdict:  forge.CheckVerdictRunning,
				CommitID: rev.CommitID,
			})
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to set running verdicts: %w", err)
	}
	if passed := len(queued) - len(toCheck); passed > 0 {
		fmt.Fprintf(u, "%d change(s) already passed\n", passed)
	}
	if len(toCheck) == 0 {
		return nil
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
	outstanding := slices.Clone(toCheck)
	defer func() {
		if ctx.Err() == nil {
			return
		}
		// Clear the running verdicts of checks that didn't finish.
		clearRunning(configMgr, outstanding)
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
	// Run checks in parallel with per-goroutine cancellation.
	type result struct {
		rev *jj.Rev
		err error
	}
	resultCh := make(chan result, len(toCheck))

	// Per-goroutine handles for drift cancellation.
	var mu sync.Mutex
	type goroutineHandle struct {
		rev    *jj.Rev
		cancel context.CancelFunc
	}
	handles := make(map[string]*goroutineHandle, len(toCheck))

	for _, rev := range toCheck {
		gctx, cancel := context.WithCancel(ctx)
		mu.Lock()
		handles[rev.ID] = &goroutineHandle{rev: rev, cancel: cancel}
		mu.Unlock()
		go func() {
			defer func() {
				mu.Lock()
				delete(handles, rev.ID)
				mu.Unlock()
			}()
			tracker.SetMessage(revIndex[rev.ID], "queued")
			wd, err := pool.Acquire(gctx)
			if err != nil {
				resultCh <- result{rev: rev, err: err}
				return
			}
			defer pool.Release(wd)
			tracker.SetMessage(revIndex[rev.ID], "running")
			tracker.SetStatus(revIndex[rev.ID], ui.TaskRunning)
			err = runInDir(gctx, pool, runner, wd, rev.CommitID, checkCmd)
			if err != nil && gctx.Err() != nil {
				err = gctx.Err()
			}
			resultCh <- result{rev: rev, err: err}
		}()
	}

	// Start drift watcher: polls jj for commit ID changes and cancels
	// goroutines whose changes have been amended.
	watchCtx, watchCancel := context.WithCancel(ctx)
	ticker := time.NewTicker(driftPollInterval)
	go func() {
		defer watchCancel()
		defer ticker.Stop()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
			}
			mu.Lock()
			if len(handles) == 0 {
				mu.Unlock()
				return
			}
			ids := make([]string, 0, len(handles))
			for id := range handles {
				ids = append(ids, id)
			}
			mu.Unlock()

			batchRevset := strings.Join(ids, "|")
			currentRevs, err := client.Revs(watchCtx, batchRevset)
			if err != nil {
				continue // jj call failed, retry next tick
			}
			currentByID := make(map[string]string, len(currentRevs))
			for _, r := range currentRevs {
				currentByID[r.ID] = r.CommitID
			}

			mu.Lock()
			for id, h := range handles {
				if currentCommit, ok := currentByID[id]; ok && currentCommit != h.rev.CommitID {
					h.cancel()
				}
			}
			mu.Unlock()
		}
	}()

	// Store verdicts as they arrive and collect errors. Results that arrive
	// together are stored in one update, so a run of quick checks doesn't
	// hold the config lock back to back.
	var failures []string
	for remaining := len(toCheck); remaining > 0; {
		// The receive runs before takeReady, waiting for at least one result.
		batch := append([]result{<-resultCh}, takeReady(resultCh, remaining-1)...)
		remaining -= len(batch)

		var verdicts []forge.CheckVerdict
		var drifted []*jj.Rev
		var settled []string // changes no longer outstanding
		for _, r := range batch {
			// Context cancellation means drift (or parent cancellation) — skip verdict.
			if r.err != nil && errors.Is(r.err, context.Canceled) {
				tracker.SetStatus(revIndex[r.rev.ID], ui.TaskSkipped)
				if ctx.Err() == nil {
					// Drift. The parent's cancellation is cleaned up on return.
					drifted = append(drifted, r.rev)
					settled = append(settled, r.rev.ID)
				}
				continue
			}
			verdictStr := forge.CheckVerdictPass
			taskStatus := ui.TaskDone
			if r.err != nil {
				verdictStr = forge.CheckVerdictFail
				taskStatus = ui.TaskFailed
			}
			tracker.SetStatus(revIndex[r.rev.ID], taskStatus)
			verdicts = append(verdicts, forge.CheckVerdict{
				ChangeID: r.rev.ID,
				Verdict:  verdictStr,
				CommitID: r.rev.CommitID,
			})
			settled = append(settled, r.rev.ID)
			if r.err != nil {
				msg := fmt.Sprintf("%s (%s)", r.rev.ID, r.rev.CommitID)
				var execErr *cmd.ExecError
				if errors.As(r.err, &execErr) && execErr.Stderr != "" {
					msg += "\n" + ui.Indent(strings.TrimSpace(execErr.Stderr), 2)
				}
				failures = append(failures, msg)
			}
		}
		err := configMgr.Update(func(s *forge.State) error {
			for _, v := range verdicts {
				s.SetCheck(v)
			}
			clearRunningIn(s, drifted)
			return nil
		})
		if err != nil {
			tracker.Finish()
			watchCancel()
			return fmt.Errorf("failed to store check verdict: %w", err)
		}
		outstanding = slices.DeleteFunc(outstanding, func(rev *jj.Rev) bool {
			return slices.Contains(settled, rev.ID)
		})
	}
	tracker.Finish()
	watchCancel()
	if ctx.Err() != nil {
		return errInterrupted()
	}
	if len(failures) > 0 {
		return &ui.UserError{
			Msg:     "check command failed",
			Details: strings.Join(failures, "\n"),
		}
	}
	return nil
}

// errInterrupted reports a run cut short by its context, so that callers stop
// rather than carry on as if the checks had passed.
func errInterrupted() error {
	return &ui.UserError{Msg: "checks interrupted"}
}

// filterCached returns the subset of revs that need checking. When force is
// true all revs are returned. Otherwise, revisions with a cached passing
// verdict whose commit ID matches are filtered out.
func filterCached(revs []*jj.Rev, verdicts []forge.CheckVerdict, force bool) []*jj.Rev {
	if force {
		return revs
	}
	var toCheck []*jj.Rev
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
	return toCheck
}

// takeReady returns up to atMost values already waiting on ch, without
// blocking.
func takeReady[T any](ch <-chan T, atMost int) []T {
	var taken []T
	for len(taken) < atMost {
		select {
		case v := <-ch:
			taken = append(taken, v)
		default:
			return taken
		}
	}
	return taken
}

// staleVerdicts returns the changes with verdicts that no longer exist or are
// immutable, which jj log doesn't label. On a jj error it returns none.
func staleVerdicts(ctx context.Context, client jj.Client, verdicts []forge.CheckVerdict) []string {
	if len(verdicts) == 0 {
		return nil
	}
	var terms []string
	for _, v := range verdicts {
		terms = append(terms, fmt.Sprintf("present(%s)", v.ChangeID))
	}
	live, err := client.Revs(ctx, fmt.Sprintf("(%s) & mutable()", strings.Join(terms, " | ")))
	if err != nil {
		return nil
	}
	var stale []string
	for _, v := range verdicts {
		if !slices.ContainsFunc(live, func(r *jj.Rev) bool { return r.ID == v.ChangeID }) {
			stale = append(stale, v.ChangeID)
		}
	}
	return stale
}

// clearRunning removes the running verdicts this run wrote for revs. Errors
// are ignored, since a leftover running verdict only shows as ci/~ until the
// next check.
func clearRunning(configMgr *forge.ConfigManager, revs []*jj.Rev) {
	if len(revs) == 0 {
		return
	}
	configMgr.Update(func(s *forge.State) error {
		clearRunningIn(s, revs)
		return nil
	})
}

// clearRunningIn removes the running verdicts this run wrote for revs from s.
// A verdict that has since changed belongs to someone else and is kept.
func clearRunningIn(s *forge.State, revs []*jj.Rev) {
	for _, rev := range revs {
		if v := s.Check(rev.ID); v != nil && v.Verdict == forge.CheckVerdictRunning && v.CommitID == rev.CommitID {
			s.RemoveChecks(rev.ID)
		}
	}
}

func runInDir(ctx context.Context, pool *WorkPool, runner cmd.Executor, wd *WorkDir, commitID, checkCmd string) error {
	if err := pool.Materialize(ctx, wd, commitID); err != nil {
		return fmt.Errorf("failed to materialize %s: %w", commitID, err)
	}

	_, runErr := runner(ctx, cmd.Opts{WorkDir: wd.Path, ProcessGroup: true}, "sh", "-c", checkCmd)
	if ctx.Err() != nil {
		// A check killed partway may have left files half written. The next
		// check in this slot starts from a full copy of its commit.
		if err := pool.invalidate(wd); err != nil {
			return errors.Join(runErr, err)
		}
	}
	return runErr
}
