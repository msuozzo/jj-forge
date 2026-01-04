package change

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/msuozzo/jj-forge/internal/jj"
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

// Upload orchestrates the trailer updates and pushing of a stack of revisions.
func Upload(ctx context.Context, client jj.Client, revset string, remote string) (*UploadResult, error) {
	stack, err := client.Revs(ctx, revset)
	if err != nil {
		return nil, fmt.Errorf("failed to get stack: %w", err)
	}
	slices.Reverse(stack) // order updates from parents to children
	result := &UploadResult{}
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
			fmt.Printf("Skipping empty change: %s\n", rev.ID)
			result.SkippedEmpty++
			result.Skipped++
			continue
		}
		// Skip anonymous commits (empty description)
		if strings.TrimSpace(rev.Description) == "" {
			fmt.Printf("Skipping anonymous change: %s\n", rev.ID)
			result.SkippedAnonymous++
			result.Skipped++
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
			newDescription = jj.UpdateForgeParent(rev.Description, mutableParentID)
		} else {
			newDescription = jj.RemoveForgeParent(rev.Description)
		}
		if newDescription != rev.Description {
			fmt.Printf("Updating trailers for %s...\n", rev.ID)
			_, err := client.Run(ctx, "describe", rev.ID, "--no-edit", "-m", newDescription)
			if err != nil {
				return nil, fmt.Errorf("failed to update trailers for %s: %w", rev.ID, err)
			}
			result.TrailersUpdated++
			// After describe, the commit has changed, so we need to push
		} else if slices.Contains(rev.RemoteBookmarks, remote+"/push-"+rev.ID) {
			fmt.Printf("Skipping synced change: %s\n", rev.ID)
			result.SkippedSynced++
			result.Skipped++
			continue
		}
		// Push the revision
		fmt.Printf("Pushing %s to %s...\n", rev.ID, remote)
		_, err = client.Run(ctx, "git", "push", "--change", rev.ID, "--remote", remote, "--allow-new")
		if err != nil {
			return nil, fmt.Errorf("failed to push %s: %w", rev.ID, err)
		}
		result.Pushed++
	}
	return result, nil
}
