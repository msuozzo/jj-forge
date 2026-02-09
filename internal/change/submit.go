package change

import (
	"context"
	"fmt"
	"slices"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
	"github.com/msuozzo/jj-forge/internal/ui"
)

// SubmitResult tracks the outcome of a submit operation.
type SubmitResult struct {
	Submitted int // Number of changes submitted
}

// Submit adds changes directly to the target branch without PR review.
// For each revision:
//   - removes forge-parent trailers
//   - pushes to fast-forward the branch
//   - verifies the push succeeded
func Submit(ctx context.Context, client jj.Client, revset, remote, branch string, u *ui.UI) (*SubmitResult, error) {
	result := &SubmitResult{}
	// PHASE 1: Fetch and load remote bookmark
	fmt.Fprintf(u, "Fetching from %s to get current state...\n", u.Styled("remote", remote))
	_, err := client.Run(ctx, "git", "fetch", "--remote", remote)
	if err != nil {
		return nil, fmt.Errorf("initial fetch from remote: %w", err)
	}
	remoteBookmark := fmt.Sprintf("%s@%s", branch, remote)
	remoteHeadRevs, err := client.Revs(ctx, remoteBookmark)
	if err != nil {
		return nil, fmt.Errorf("querying remote bookmark %s: %w", remoteBookmark, err)
	}
	if len(remoteHeadRevs) != 1 {
		return nil, fmt.Errorf("expected exactly one revision at %s, got %d", remoteBookmark, len(remoteHeadRevs))
	}
	currentRemoteHead := remoteHeadRevs[0].ID
	fmt.Fprintf(u, "Current remote head at %s: %s\n", u.Styled("bookmark", remoteBookmark), u.Styled("change_id", currentRemoteHead))
	// PHASE 2: Get changes to be submitted
	revs, err := client.Revs(ctx, revset)
	if err != nil {
		return nil, fmt.Errorf("getting revisions: %w", err)
	}
	if len(revs) == 0 {
		return result, nil
	}
	// Get parent revisions
	parentRevset := fmt.Sprintf("parents(%s)~(%s)", revset, revset)
	parents, err := client.Revs(ctx, parentRevset)
	if err != nil {
		return nil, fmt.Errorf("getting parent revisions: %w", err)
	}
	// Build revision map including remote head
	revmap := make(map[string]*jj.Rev)
	for _, rev := range slices.Concat(revs, parents) {
		revmap[rev.ID] = rev
	}
	revmap[currentRemoteHead] = remoteHeadRevs[0]
	// Reverse to process from parent to child (topological order)
	slices.Reverse(revs)
	// PHASE 3: Pre-validate entire stack (fail fast before any pushes)
	expectedParent := currentRemoteHead
	for i, rev := range revs {
		// Check for merge commits (not supported)
		if len(rev.Parents) > 1 {
			return nil, fmt.Errorf(
				"validation failed: revision %s (position %d in stack) is a merge commit (parents: %v).\n"+
					"Submit only supports linear stacks.",
				rev.ID, i+1, rev.Parents)
		}
		// Check parent relationship
		if len(rev.Parents) != 1 || rev.Parents[0] != expectedParent {
			actualParent := ""
			if len(rev.Parents) > 0 {
				actualParent = rev.Parents[0]
			}
			return nil, fmt.Errorf(
				"validation failed: revision %s (position %d in stack) is not a direct child of %s.\n"+
					"Expected parent: %s\n"+
					"Actual parent: %s\n"+
					"Please rebase your stack onto %s before submitting.",
				rev.ID, i+1, remoteBookmark, expectedParent, actualParent, remoteBookmark)
		}
		// Validate parent exists in map
		if _, ok := revmap[expectedParent]; !ok {
			return nil, fmt.Errorf("missing parent %s for revision %s", expectedParent, rev.ID)
		}
		// Next commit should have this one as parent
		expectedParent = rev.ID
	}
	// PHASE 4: Remove trailers, push chain tip, and verify
	for _, rev := range revs {
		newDescription := forge.RemoveParentTrailer(rev.Description)
		if newDescription != rev.Description {
			fmt.Fprintf(u, "Removing forge-parent trailer from %s...\n", u.Styled("change_id", rev.ID))
			_, err := client.Run(ctx, "describe", rev.ID, "--no-edit", "-m", newDescription)
			if err != nil {
				return nil, fmt.Errorf("removing trailer from %s: %w", rev.ID, err)
			}
		}
	}
	// Move bookmark to the chain tip and push once (all ancestors are included)
	chainTip := revs[len(revs)-1]
	fmt.Fprintf(u, "Submitting %d change(s) to %s...\n", len(revs), u.Styled("bookmark", remoteBookmark))
	_, err = client.Run(ctx, "bookmark", "set", branch, "-r", chainTip.ID)
	if err != nil {
		return nil, fmt.Errorf("moving bookmark %s to %s: %w", branch, chainTip.ID, err)
	}
	_, err = client.Run(ctx, "git", "push", "--bookmark", branch, "--remote", remote)
	if err != nil {
		return nil, fmt.Errorf("pushing %s: %w", chainTip.ID, err)
	}
	// Fetch and verify
	_, err = client.Run(ctx, "git", "fetch", "--remote", remote)
	if err != nil {
		return nil, fmt.Errorf("fetching after push: %w", err)
	}
	updatedHeadRevs, err := client.Revs(ctx, remoteBookmark)
	if err != nil {
		return nil, fmt.Errorf("re-querying remote bookmark after push: %w", err)
	}
	if len(updatedHeadRevs) != 1 {
		return nil, fmt.Errorf("expected exactly one revision at %s after push, got %d",
			remoteBookmark, len(updatedHeadRevs))
	}
	if updatedHeadRevs[0].ID != chainTip.ID {
		return nil, fmt.Errorf(
			"remote head verification failed: expected %s at %s, but found %s.\n"+
				"This might indicate a concurrent push by another developer.",
			chainTip.ID, remoteBookmark, updatedHeadRevs[0].ID)
	}
	fmt.Fprintf(u, "Verified: %s is now at %s\n", u.Styled("bookmark", remoteBookmark), u.Styled("change_id", chainTip.ID))
	result.Submitted = len(revs)
	return result, nil
}
