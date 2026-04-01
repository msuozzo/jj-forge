package change

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
	"github.com/msuozzo/jj-forge/internal/ui"
)

// UploadResult contains statistics about the upload operation.
type UploadResult struct {
	Pushed           int
	Skipped          int
	SkippedEmpty     int
	SkippedAnonymous int
	SkippedImmutable int
	SkippedSynced    int
	TrailersUpdated  int
}

// UpdateTrailersResult contains statistics about the trailer update phase.
type UpdateTrailersResult struct {
	TrailersUpdated  int
	SkippedEmpty     int
	SkippedAnonymous int
	SkippedImmutable int
	Revs             []*jj.Rev // Resolved revisions (pre-trailer-update order, children first)
}

// PushResult contains statistics about the push phase.
type PushResult struct {
	Pushed        int
	SkippedSynced int
}

// UpdateTrailers updates forge-parent trailers for a stack of revisions.
func UpdateTrailers(ctx context.Context, client jj.Client, revset string, u *ui.UI, tracker ...*ui.TaskTracker) (*UpdateTrailersResult, error) {
	var tr *ui.TaskTracker
	if len(tracker) > 0 {
		tr = tracker[0]
	}

	stack, err := client.Revs(ctx, revset)
	if err != nil {
		return nil, fmt.Errorf("failed to get stack: %w", err)
	}
	// Store original order (children first) for callers before reversing.
	origStack := make([]*jj.Rev, len(stack))
	copy(origStack, stack)
	slices.Reverse(stack) // order updates from parents to children
	result := &UpdateTrailersResult{Revs: origStack}
	if len(stack) == 0 {
		return result, nil
	}
	// Also fetch all parents of the target rev set
	pstack, err := client.Revs(ctx, fmt.Sprintf("parents(%s)~(%s)", revset, revset))
	if err != nil {
		return nil, fmt.Errorf("failed to get parent stack: %w", err)
	}
	revmap := make(map[string]*jj.Rev)
	for _, rev := range slices.Concat(stack, pstack) {
		revmap[rev.ID] = rev
	}
	for _, rev := range stack {
		// Skip immutable commits
		if !rev.IsMutable {
			if tr == nil {
				fmt.Fprintf(u, "Skipping immutable change: %s\n", u.Styled("change_id", rev.ID))
			}
			result.SkippedImmutable++
			continue
		}
		// Skip empty commits
		if rev.IsEmpty {
			if tr == nil {
				fmt.Fprintf(u, "Skipping empty change: %s\n", u.Styled("change_id", rev.ID))
			}
			result.SkippedEmpty++
			continue
		}
		// Skip anonymous commits (empty description)
		if strings.TrimSpace(rev.Description) == "" {
			if tr == nil {
				fmt.Fprintf(u, "Skipping anonymous change: %s\n", u.Styled("change_id", rev.ID))
			}
			result.SkippedAnonymous++
			continue
		}
		// Determine all mutable parent change IDs.
		var mutableParentIDs []string
		for _, pID := range rev.Parents {
			if pRev, ok := revmap[pID]; !ok {
				return nil, fmt.Errorf("missing parent %s for %s", pID, rev.ID)
			} else if pRev.IsMutable {
				mutableParentIDs = append(mutableParentIDs, pRev.ID)
			}
		}
		// Update trailers
		var newDescription string
		if len(mutableParentIDs) > 0 {
			newDescription = forge.UpdateParentTrailers(rev.Description, mutableParentIDs)
		} else {
			newDescription = forge.RemoveParentTrailer(rev.Description)
		}
		if newDescription != rev.Description {
			if tr != nil {
				tr.SetMessageByName(rev.ID, "updating trailers")
			} else {
				fmt.Fprintf(u, "Updating trailers for %s...\n", u.Styled("change_id", rev.ID))
			}
			_, err := client.Run(ctx, "describe", rev.ID, "--no-edit", "-m", newDescription)
			if err != nil {
				return nil, fmt.Errorf("failed to update trailers for %s: %w", rev.ID, err)
			}
			result.TrailersUpdated++
		}
	}
	return result, nil
}

// Push pushes a stack of revisions to the given remote, skipping those already synced.
// If preResolved is non-nil, it is used directly instead of re-resolving the revset.
// The preResolved slice should be in jj log order (children first); it will be reversed internally.
func Push(ctx context.Context, client jj.Client, revset string, remote string, u *ui.UI, preResolved []*jj.Rev, tracker ...*ui.TaskTracker) (*PushResult, error) {
	var tr *ui.TaskTracker
	if len(tracker) > 0 {
		tr = tracker[0]
	}

	var stack []*jj.Rev
	if preResolved != nil {
		stack = make([]*jj.Rev, len(preResolved))
		copy(stack, preResolved)
	} else {
		// Re-resolve revset since commits may have changed due to trailer updates
		var err error
		stack, err = client.Revs(ctx, revset)
		if err != nil {
			return nil, fmt.Errorf("failed to get stack: %w", err)
		}
	}
	slices.Reverse(stack) // order from parents to children
	result := &PushResult{}
	// Identify pushable revisions (skip empty, anonymous, immutable, and already-synced).
	type pushItem struct {
		rev   *jj.Rev
		index int // index into taskNames for TaskTracker
	}
	var toPush []pushItem
	for _, rev := range stack {
		if rev.IsEmpty || strings.TrimSpace(rev.Description) == "" || !rev.IsMutable {
			continue
		}
		if slices.Contains(rev.RemoteBookmarks, remote+"/push-"+rev.ID) {
			if tr == nil {
				fmt.Fprintf(u, "Skipping synced change: %s\n", u.Styled("change_id", rev.ID))
			}
			result.SkippedSynced++
			continue
		}
		toPush = append(toPush, pushItem{rev: rev, index: len(toPush)})
	}
	if len(toPush) == 0 {
		return result, nil
	}

	if tr == nil {
		// Use internal TaskTracker for progress display when pushing multiple changes.
		fmt.Fprintf(u, "Pushing %d change(s)...\n", len(toPush))
		taskNames := make([]string, len(toPush))
		for i, item := range toPush {
			taskNames[i] = item.rev.ID
		}
		tr = ui.NewTaskTracker(u, taskNames)
		tr.Start()
		defer tr.Finish()
	}

	for _, item := range toPush {
		tr.SetMessageByName(item.rev.ID, "pushing")
		tr.SetStatusByName(item.rev.ID, ui.TaskRunning)
		if _, err := client.Run(ctx, "git", "push", "--change", item.rev.ID, "--remote", remote, "--allow-new"); err != nil {
			tr.SetStatusByName(item.rev.ID, ui.TaskFailed)
			return nil, fmt.Errorf("failed to push %s: %w", item.rev.ID, err)
		}
		// If we are using an external tracker, we don't set terminal status here
		// because other phases (like PR updates) might follow.
		if tr.IsInteractive() && len(tracker) == 0 {
			tr.SetStatusByName(item.rev.ID, ui.TaskDone)
		}
		result.Pushed++
	}
	return result, nil
}

// Upload orchestrates the trailer updates and pushing of a stack of revisions.
func Upload(ctx context.Context, client jj.Client, revset string, remote string, u *ui.UI, tracker ...*ui.TaskTracker) (*UploadResult, error) {
	trailerResult, err := UpdateTrailers(ctx, client, revset, u, tracker...)
	if err != nil {
		return nil, err
	}
	// If no trailers were updated, commit IDs haven't changed — reuse resolved revs.
	var preResolved []*jj.Rev
	if trailerResult.TrailersUpdated == 0 {
		preResolved = trailerResult.Revs
	}
	pushResult, err := Push(ctx, client, revset, remote, u, preResolved, tracker...)
	if err != nil {
		return nil, err
	}
	skipped := trailerResult.SkippedEmpty + trailerResult.SkippedAnonymous + trailerResult.SkippedImmutable + pushResult.SkippedSynced
	return &UploadResult{
		Pushed:           pushResult.Pushed,
		Skipped:          skipped,
		SkippedEmpty:     trailerResult.SkippedEmpty,
		SkippedAnonymous: trailerResult.SkippedAnonymous,
		SkippedImmutable: trailerResult.SkippedImmutable,
		SkippedSynced:    pushResult.SkippedSynced,
		TrailersUpdated:  trailerResult.TrailersUpdated,
	}, nil
}
