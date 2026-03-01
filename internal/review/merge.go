package review

import (
	"context"
	"errors"
	"fmt"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
	"github.com/msuozzo/jj-forge/internal/ui"
)

// ErrNotUploaded is returned when a merge is attempted on a change that
// has not been pushed to the fork remote.
var ErrNotUploaded = errors.New("change not uploaded")

// ErrHasParentTrailer is returned when a merge is attempted on a change that
// still has a forge-parent annotation, indicating a parent should be merged first.
var ErrHasParentTrailer = errors.New("change has forge-parent annotation")

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
	if !isUploaded(rev, params.ForkRemote) {
		return nil, fmt.Errorf("change %s has local modifications not yet pushed to %s: %w", rev.ID, params.ForkRemote, ErrNotUploaded)
	}
	if parentTrailer, found := jj.GetTrailer(jj.ParseDescriptionTrailers(rev.Description), forge.ParentTrailerKey); found {
		return nil, fmt.Errorf("change %s has a forge-parent annotation (parent: %s). Merge or close the parent review first, then run 'jj-forge review update' to refresh: %w", rev.ID, parentTrailer.Value, ErrHasParentTrailer)
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
	// Strip managed links section from the merged PR (non-fatal)
	if details, err := forgeClient.GetReview(ctx, upstreamRemoteURL, reviewNumber); err == nil {
		if stripped := StripPRLinks(details.Body); stripped != details.Body {
			if err := forgeClient.UpdateReview(ctx, upstreamRemoteURL, reviewNumber, stripped); err != nil {
				params.UI.PrintWarning("failed to remove PR links from merged review: %v", err)
			}
		}
	}
	// Cleanup (unless --no-cleanup)
	if !params.NoCleanup {
		bookmarkName := fmt.Sprintf("push-%s", rev.ID)
		u := params.UI
		// Fetch from fork remote to update tracking info (the merge may have
		// changed the remote ref, causing push to fail with "stale info").
		fmt.Fprintf(u, "Fetching from %s...\n", u.Styled("remote", params.ForkRemote))
		_, err = jjClient.Run(ctx, "git", "fetch", "--remote", params.ForkRemote)
		if err != nil {
			u.PrintWarning("failed to fetch from %s: %v", params.ForkRemote, err)
		}
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
	// Clean up check verdict for the merged change (non-fatal)
	if err := configMgr.RemoveCheckVerdicts([]string{rev.ID}); err != nil {
		params.UI.PrintWarning("failed to clean up check verdict: %v", err)
	}
	// Clean up PR links on sibling reviews (non-fatal)
	if err := cleanupLinksAfterMerge(ctx, jjClient, forgeClient, configMgr, rev.ID, upstreamRemoteURL); err != nil {
		params.UI.PrintWarning("failed to clean up PR links: %v", err)
	}
	return &MergeResult{
		ChangeID: rev.ID,
		Number:   reviewNumber,
	}, nil
}

// cleanupLinksAfterMerge re-derives and updates PR links for all remaining
// open reviews after a change has been merged.
func cleanupLinksAfterMerge(
	ctx context.Context,
	jjClient jj.Client,
	forgeClient forge.Forge,
	configMgr *forge.ConfigManager,
	mergedChangeID string,
	upstreamURL string,
) error {
	records, err := configMgr.GetReviewRecords()
	if err != nil {
		return fmt.Errorf("failed to get review records: %w", err)
	}

	// Collect open review records (excluding the merged one)
	var openRecords []forge.ReviewRecord
	for _, rec := range records {
		if rec.Status == forge.ReviewStateOpen && rec.ChangeID != mergedChangeID {
			openRecords = append(openRecords, rec)
		}
	}
	if len(openRecords) == 0 {
		return nil
	}

	// Resolve revisions for each open review and build parent/child maps
	reviewByChange := make(map[string]*forge.ReviewRecord)
	parentOf := make(map[string]string)
	childrenOf := make(map[string][]string)

	for i := range openRecords {
		rec := &openRecords[i]
		reviewByChange[rec.ChangeID] = rec

		rev, err := jjClient.Rev(ctx, rec.ChangeID)
		if err != nil {
			continue // Skip if revision can't be resolved
		}
		trailers := jj.ParseDescriptionTrailers(rev.Description)
		parentTrailer, found := jj.GetTrailer(trailers, forge.ParentTrailerKey)
		if found {
			parentID := parentTrailer.Value
			// Only track relationships between open reviews (not the merged one)
			if parentID != mergedChangeID {
				parentOf[rec.ChangeID] = parentID
				childrenOf[parentID] = append(childrenOf[parentID], rec.ChangeID)
			}
		}
	}

	// Update PR descriptions for reviews that referenced the merged change
	for _, rec := range openRecords {
		reviewNumber, err := forgeClient.ParseID(rec.ForgeID)
		if err != nil {
			continue
		}

		details, err := forgeClient.GetReview(ctx, upstreamURL, reviewNumber)
		if err != nil {
			continue
		}

		// Build parent links
		var parentLinks []PRLink
		if pID, ok := parentOf[rec.ChangeID]; ok {
			if pRec, ok := reviewByChange[pID]; ok {
				pNum, err := forgeClient.ParseID(pRec.ForgeID)
				if err == nil {
					parentLinks = append(parentLinks, PRLink{Number: pNum, URL: pRec.URL})
				}
			}
		}

		// Build child links
		var childLinks []PRLink
		for _, cID := range childrenOf[rec.ChangeID] {
			if cRec, ok := reviewByChange[cID]; ok {
				cNum, err := forgeClient.ParseID(cRec.ForgeID)
				if err == nil {
					childLinks = append(childLinks, PRLink{Number: cNum, URL: cRec.URL})
				}
			}
		}

		newBody := SetPRLinks(details.Body, parentLinks, childLinks)
		if newBody != details.Body {
			if err := forgeClient.UpdateReview(ctx, upstreamURL, reviewNumber, newBody); err != nil {
				continue // Non-fatal per PR
			}
		}
	}

	return nil
}
