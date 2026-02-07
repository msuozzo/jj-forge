package review

import (
	"context"
	"fmt"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
	"github.com/msuozzo/jj-forge/internal/ui"
)

// MergeParams contains parameters for the merge command.
type MergeParams struct {
	Rev            string // Revset to merge review for
	ForkRemote     string // Remote where the branch is pushed
	UpstreamRemote string // Remote to merge PR from
	NoCleanup      bool   // Skip local cleanup if true
	UI             *ui.UI // UI for styled output
}

// MergeResult contains the result of the merge command.
type MergeResult struct {
	ChangeID string
	Number   int
}

// Merge merges a code review and optionally cleans up local state.
func Merge(
	ctx context.Context,
	jjClient jj.Client,
	forgeClient forge.Forge,
	configMgr *forge.ConfigManager,
	params MergeParams,
) (*MergeResult, error) {
	rev, err := jjClient.Rev(ctx, params.Rev)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve revision %s: %w", params.Rev, err)
	}
	// Find review record in config
	reviewRecord, err := configMgr.GetReviewByChangeID(rev.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}
	if reviewRecord == nil {
		return nil, fmt.Errorf("no review found for change %s. Create one with: jj-forge review open %s", rev.ID, rev.ID)
	}
	if reviewRecord.Status == forge.ReviewStateMerged {
		return nil, fmt.Errorf("review #%s for change %s is already merged", reviewRecord.ForgeID, rev.ID)
	}
	if reviewRecord.Status == forge.ReviewStateClosed {
		return nil, fmt.Errorf("review #%s for change %s is closed. Reopen it or create a new review", reviewRecord.ForgeID, rev.ID)
	}
	reviewNumber, err := forgeClient.ParseID(reviewRecord.ForgeID)
	if err != nil {
		return nil, fmt.Errorf("invalid review number in config: %s", reviewRecord.ForgeID)
	}
	upstreamRemoteURL, err := jjClient.RemoteURL(ctx, params.UpstreamRemote)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote URL for %s: %w", params.UpstreamRemote, err)
	}
	// Merge review via forge
	if err := forgeClient.MergeReview(ctx, upstreamRemoteURL, reviewNumber); err != nil {
		return nil, fmt.Errorf("failed to merge review: %w", err)
	}
	// Cleanup (unless --no-cleanup)
	if !params.NoCleanup {
		bookmarkName := fmt.Sprintf("push-%s", rev.ID)
		u := params.UI
		// Delete bookmark
		fmt.Fprintf(u, "Deleting bookmark %s...\n", u.Styled("bookmark", bookmarkName))
		_, err = jjClient.Run(ctx, "bookmark", "delete", bookmarkName)
		if err != nil {
			// Non-fatal: log warning and continue
			u.PrintWarning("failed to delete bookmark %s: %v", bookmarkName, err)
		}
		// Push bookmark deletion
		fmt.Fprintf(u, "Pushing bookmark deletion to %s...\n", u.Styled("remote", params.ForkRemote))
		_, err = jjClient.Run(ctx, "git", "push", "--remote", params.ForkRemote, "--bookmark", bookmarkName)
		if err != nil {
			u.PrintWarning("failed to push bookmark deletion: %v", err)
		}
		// Fetch from upstream to update state
		fmt.Fprintf(u, "Fetching from %s...\n", u.Styled("remote", params.UpstreamRemote))
		_, err = jjClient.Run(ctx, "git", "fetch", "--remote", params.UpstreamRemote)
		if err != nil {
			u.PrintWarning("failed to fetch: %v", err)
		}
	}
	reviewRecord.Status = forge.ReviewStateMerged
	if err := configMgr.AddReviewRecord(*reviewRecord); err != nil {
		return nil, fmt.Errorf("failed to update review status: %w", err)
	}
	return &MergeResult{
		ChangeID: rev.ID,
		Number:   reviewNumber,
	}, nil
}
