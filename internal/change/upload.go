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
	SkippedSynced    int
	TrailersUpdated  int
}

// UpdateTrailersResult contains statistics about the trailer update phase.
type UpdateTrailersResult struct {
	TrailersUpdated  int
	SkippedEmpty     int
	SkippedAnonymous int
	Revs             []*jj.Rev // Resolved revisions (pre-trailer-update order, children first)
}

// PushResult contains statistics about the push phase.
type PushResult struct {
	Pushed        int
	SkippedSynced int
}

// UpdateTrailers updates forge-parent trailers for a stack of revisions.
func UpdateTrailers(ctx context.Context, client jj.Client, revset string, u *ui.UI) (*UpdateTrailersResult, error) {
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
		// Skip empty commits
		if rev.IsEmpty {
			fmt.Fprintf(u, "Skipping empty change: %s\n", u.Styled("change_id", rev.ID))
			result.SkippedEmpty++
			continue
		}
		// Skip anonymous commits (empty description)
		if strings.TrimSpace(rev.Description) == "" {
			fmt.Fprintf(u, "Skipping anonymous change: %s\n", u.Styled("change_id", rev.ID))
			result.SkippedAnonymous++
			continue
		}
		// Determine the parent mutable change ID if it exists.
		var mutableParentID string
		for _, pID := range rev.Parents {
			if pRev, ok := revmap[pID]; !ok {
				return nil, fmt.Errorf("missing parent %s for %s", pID, rev.ID)
			} else if pRev.IsMutable {
				mutableParentID = pRev.ID
				break
			}
		}
		// Update trailers
		var newDescription string
		if mutableParentID != "" {
			newDescription = forge.UpdateParentTrailer(rev.Description, mutableParentID)
		} else {
			newDescription = forge.RemoveParentTrailer(rev.Description)
		}
		if newDescription != rev.Description {
			fmt.Fprintf(u, "Updating trailers for %s...\n", u.Styled("change_id", rev.ID))
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
func Push(ctx context.Context, client jj.Client, revset string, remote string, u *ui.UI, preResolved ...[]*jj.Rev) (*PushResult, error) {
	var stack []*jj.Rev
	if len(preResolved) > 0 && preResolved[0] != nil {
		stack = make([]*jj.Rev, len(preResolved[0]))
		copy(stack, preResolved[0])
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
	for _, rev := range stack {
		// Silently skip empty/anonymous
		if rev.IsEmpty || strings.TrimSpace(rev.Description) == "" {
			continue
		}
		// Check if already synced
		if slices.Contains(rev.RemoteBookmarks, remote+"/push-"+rev.ID) {
			fmt.Fprintf(u, "Skipping synced change: %s\n", u.Styled("change_id", rev.ID))
			result.SkippedSynced++
			continue
		}
		// Push the revision
		fmt.Fprintf(u, "Pushing %s to %s...\n", u.Styled("change_id", rev.ID), u.Styled("remote", remote))
		if _, err := client.Run(ctx, "git", "push", "--change", rev.ID, "--remote", remote, "--allow-new"); err != nil {
			return nil, fmt.Errorf("failed to push %s: %w", rev.ID, err)
		}
		result.Pushed++
	}
	return result, nil
}

// Upload orchestrates the trailer updates and pushing of a stack of revisions.
func Upload(ctx context.Context, client jj.Client, revset string, remote string, u *ui.UI) (*UploadResult, error) {
	trailerResult, err := UpdateTrailers(ctx, client, revset, u)
	if err != nil {
		return nil, err
	}
	// If no trailers were updated, commit IDs haven't changed — reuse resolved revs.
	var preResolved []*jj.Rev
	if trailerResult.TrailersUpdated == 0 {
		preResolved = trailerResult.Revs
	}
	pushResult, err := Push(ctx, client, revset, remote, u, preResolved)
	if err != nil {
		return nil, err
	}
	skipped := trailerResult.SkippedEmpty + trailerResult.SkippedAnonymous + pushResult.SkippedSynced
	return &UploadResult{
		Pushed:           pushResult.Pushed,
		Skipped:          skipped,
		SkippedEmpty:     trailerResult.SkippedEmpty,
		SkippedAnonymous: trailerResult.SkippedAnonymous,
		SkippedSynced:    pushResult.SkippedSynced,
		TrailersUpdated:  trailerResult.TrailersUpdated,
	}, nil
}
