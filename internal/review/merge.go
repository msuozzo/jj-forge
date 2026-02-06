package review

import (
	"context"
	"fmt"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// MergeParams contains parameters for the merge command.
type MergeParams struct {
	Rev            string // Revset to merge review for
	ForkRemote     string // Remote where the branch is pushed
	UpstreamRemote string // Remote to merge PR from
	NoCleanup      bool   // Skip local cleanup if true
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
		// Delete bookmark
		fmt.Printf("Deleting bookmark %s...\n", bookmarkName)
		_, err = jjClient.Run(ctx, "bookmark", "delete", bookmarkName)
		if err != nil {
			// Non-fatal: log warning and continue
			fmt.Printf("Warning: failed to delete bookmark %s: %v\n", bookmarkName, err)
		}
		// Push bookmark deletion
		fmt.Printf("Pushing bookmark deletion to %s...\n", params.ForkRemote)
		_, err = jjClient.Run(ctx, "git", "push", "--remote", params.ForkRemote, "--bookmark", bookmarkName)
		if err != nil {
			fmt.Printf("Warning: failed to push bookmark deletion: %v\n", err)
		}
		// Fetch from upstream to update state
		fmt.Printf("Fetching from %s...\n", params.UpstreamRemote)
		_, err = jjClient.Run(ctx, "git", "fetch", "--remote", params.UpstreamRemote)
		if err != nil {
			fmt.Printf("Warning: failed to fetch: %v\n", err)
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
