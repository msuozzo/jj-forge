package change

import (
	"context"
	"fmt"
	"slices"

	"github.com/msuozzo/jj-forge/internal/jj"
)

// SubmitResult tracks the outcome of a submit operation.
type SubmitResult struct {
	Submitted int // Number of changes submitted
}

// Submit adds changes directly to the target branch without PR review.
// For each revision:
//   - pushes to fast-forward the branch
//   - verifies the push succeeded
func Submit(ctx context.Context, client jj.Client, revset, remote, branch string) (*SubmitResult, error) {
	result := &SubmitResult{}
	// PHASE 1: Fetch and load remote bookmark
	fmt.Printf("Fetching from %s to get current state...\n", remote)
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
	fmt.Printf("Current remote head at %s: %s\n", remoteBookmark, currentRemoteHead)
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
	// PHASE 4: Process each revision (remove trailer, push, fetch, verify)
	expectedParent = currentRemoteHead
	for i, rev := range revs {
		fmt.Printf("\nProcessing commit %d/%d: %s\n", i+1, len(revs), rev.ID)
		// Move the bookmark to point to this commit, then push it
		fmt.Printf("  Submitting %s to %s...\n", rev.ID, remoteBookmark)
		_, err := client.Run(ctx, "bookmark", "set", branch, "-r", rev.ID)
		if err != nil {
			return nil, fmt.Errorf("moving bookmark %s to %s: %w", branch, rev.ID, err)
		}
		// Push the bookmark to fast-forward the remote branch
		_, err = client.Run(ctx, "git", "push", "--bookmark", branch, "--remote", remote)
		if err != nil {
			return nil, fmt.Errorf("pushing %s: %w", rev.ID, err)
		}
		result.Submitted++
		// Fetch from remote to update local state
		fmt.Printf("  Fetching from %s...\n", remote)
		_, err = client.Run(ctx, "git", "fetch", "--remote", remote)
		if err != nil {
			return nil, fmt.Errorf("fetching after push %d: %w", i+1, err)
		}
		// Re-query remote bookmark to verify push succeeded
		updatedHeadRevs, err := client.Revs(ctx, remoteBookmark)
		if err != nil {
			return nil, fmt.Errorf("re-querying remote bookmark after push: %w", err)
		}
		if len(updatedHeadRevs) != 1 {
			return nil, fmt.Errorf("expected exactly one revision at %s after push, got %d",
				remoteBookmark, len(updatedHeadRevs))
		}
		// Verify the push was successful (detect concurrent pushes)
		newRemoteHead := updatedHeadRevs[0].ID
		if newRemoteHead != rev.ID {
			return nil, fmt.Errorf(
				"remote head verification failed: expected %s at %s, but found %s.\n"+
					"This might indicate a concurrent push by another developer.",
				rev.ID, remoteBookmark, newRemoteHead)
		}
		fmt.Printf("  ✓ Verified: %s is now at %s\n", rev.ID, remoteBookmark)
		expectedParent = rev.ID
	}
	return result, nil
}
