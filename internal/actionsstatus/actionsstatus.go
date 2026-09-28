// Package actionsstatus implements jj forge util actions-status, which
// reports whether GitHub Actions passed on the commit pushed to a pull
// request.
package actionsstatus

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/msuozzo/jj-forge/internal/ui"
)

// Verdict is the outcome of the checks on a pull request's head.
type Verdict string

const (
	Passed   Verdict = "passed"
	Failed   Verdict = "failed"
	Running  Verdict = "running"
	NoChecks Verdict = "none" // No workflow runs for the push and nothing else reports
)

// Status is a verdict and its details: what failed, is still running or was
// skipped.
type Status struct {
	Verdict Verdict
	Detail  string
}

// GitHub runs gh api requests. *github.Client implements it.
type GitHub interface {
	API(ctx context.Context, args ...string) (string, error)
}

// Wait checks the pull request every poll until the checks pass, fail or are
// found not to run, for at most timeout (0 for no limit), reporting progress
// to w. With a zero poll it checks once.
func Wait(ctx context.Context, gh GitHub, repoURI, number, commitID string, poll, timeout time.Duration, w io.Writer) (*Status, error) {
	r, err := newReader(gh, repoURI)
	if err != nil {
		return nil, err
	}
	var last string
	deadline := time.Now().Add(timeout)
	for {
		status, err := r.check(ctx, number, commitID)
		if err != nil || status.Verdict != Running || poll == 0 {
			return status, err
		}
		wait := poll
		if timeout > 0 {
			left := time.Until(deadline)
			if left <= 0 {
				status.Detail = fmt.Sprintf("gave up after %s: %s", timeout, status.Detail)
				return status, nil
			}
			wait = min(wait, left)
		}
		if status.Detail != last {
			fmt.Fprintf(w, "Waiting for checks: %s\n", status.Detail)
			last = status.Detail
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// check reports the checks on the pull request's head. They only count once
// GitHub reports commitID, a full commit ID, as the head, so a push it has not
// processed yet reads as running. The checks are not finished until every run
// that predict expects has a check suite, since a quick run for one event can
// finish before the others start.
func (r *reader) check(ctx context.Context, number, commitID string) (*Status, error) {
	pr, err := r.pullRequest(ctx, number)
	if err != nil {
		return nil, err
	}
	head := pr.Head
	if head.OID != commitID {
		if err := r.checkBranch(ctx, number, pr, commitID); err != nil {
			return nil, err
		}
		return running("GitHub has not registered %.12s yet (PR head is %.12s)", commitID, head.OID), nil
	}
	var actions, unfinished, failed []string
	for _, s := range head.CheckSuites {
		switch {
		case s.Workflow == "":
			continue // Other apps' suites, some of which never finish
		case s.Status != "COMPLETED":
			if !slices.Contains(unfinished, s.Workflow) {
				unfinished = append(unfinished, s.Workflow)
			}
		case !slices.Contains([]string{"SUCCESS", "NEUTRAL", "SKIPPED"}, s.Conclusion):
			failed = append(failed, fmt.Sprintf("%s (%s)", s.Workflow, strings.ToLower(s.Conclusion)))
		}
		actions = append(actions, s.Workflow)
	}
	switch {
	case len(failed) > 0:
		return &Status{Verdict: Failed, Detail: strings.Join(failed, ", ")}, nil
	case head.RollupState == "FAILURE" || head.RollupState == "ERROR":
		return &Status{Verdict: Failed, Detail: "checks from other apps failed"}, nil
	case pr.Mergeable == "CONFLICTING":
		return nil, fmt.Errorf("PR #%s has merge conflicts with %s, and GitHub runs no pull_request workflows until they are resolved", number, pr.BaseRefName)
	case len(pr.MergeParents) != 2 || !slices.Contains(pr.MergeParents, commitID):
		// GitHub reads the workflows from a test merge commit, which it makes
		// for each head shortly after the push.
		return running("waiting for GitHub to test-merge %.12s into %s", commitID, pr.BaseRefName), nil
	}
	expected, reason, err := r.predict(ctx, number, pr)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, e := range expected {
		if !slices.ContainsFunc(head.CheckSuites, e.startedBy) {
			missing = append(missing, e.String())
		}
	}
	status := &Status{Verdict: Passed}
	switch {
	case len(missing) > 0:
		return running("waiting for %s to start on %.12s", strings.Join(missing, ", "), commitID), nil
	case len(unfinished) > 0:
		return running("running: %s", strings.Join(unfinished, ", ")), nil
	case len(actions) == 0 && head.RollupState == "":
		status = &Status{Verdict: NoChecks, Detail: reason}
	case head.RollupState != "SUCCESS":
		return running("checks from other apps have not finished"), nil
	case len(actions) == 0:
		status.Detail = "no Actions workflow runs for this push, and the other checks passed"
	}
	// The pull request can lag behind its branch, so before reporting
	// success, make sure that no other push has replaced the commit.
	if err := r.checkBranch(ctx, number, pr, commitID); err != nil {
		return nil, err
	}
	return status, nil
}

// checkBranch returns an error when the pull request's branch on GitHub has
// moved away from commitID, for example after a push from elsewhere.
func (r *reader) checkBranch(ctx context.Context, number string, pr *pullRequest, commitID string) error {
	at, err := r.branchAt(ctx, pr.HeadRepo, pr.HeadRefName)
	if err != nil || at == commitID {
		return err
	}
	return &ui.UserError{
		Msg:     fmt.Sprintf("the branch of PR #%s has moved", number),
		Details: fmt.Sprintf("%s on GitHub is at %.12s, not at the pushed commit %.12s", pr.HeadRefName, at, commitID),
		Hint:    "Fetch with 'jj git fetch', then push the change again with 'jj forge review update'.",
	}
}

func running(format string, args ...any) *Status {
	return &Status{Verdict: Running, Detail: fmt.Sprintf(format, args...)}
}
