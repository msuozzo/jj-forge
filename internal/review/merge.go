package review

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
	"github.com/msuozzo/jj-forge/internal/ui"
)

// ErrNotUploaded is returned when an operation requires a change to have been
// pushed to the fork remote but it has not been.
var ErrNotUploaded = errors.New("change not uploaded")

// ErrHasParentTrailer is returned when an operation requires a change to have
// no forge-parent annotation, indicating a parent should be merged first.
var ErrHasParentTrailer = errors.New("change has forge-parent annotation")

// MergeParams contains parameters for the merge command.
type MergeParams struct {
	Rev               string // Revset to merge review for
	ForkRemote        string // Remote where the branch is pushed
	UpstreamRemote    string // Remote to merge PR from
	UpstreamRemoteURL string // Pre-resolved upstream remote URL (optional; resolved if empty)
	NoCleanup         bool   // Skip local cleanup if true
	UI                *ui.UI // UI for styled output
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
	if err := Validate(rev, reviewRecord,
		RequireReviewExists,
		RequireReviewOpen,
		RequireUploaded(params.ForkRemote),
		RequireNoParentTrailer,
	); err != nil {
		return nil, err
	}
	reviewNumber, err := forgeClient.ParseID(reviewRecord.ForgeID)
	if err != nil {
		return nil, fmt.Errorf("invalid review number in config: %s", reviewRecord.ForgeID)
	}
	upstreamRemoteURL := params.UpstreamRemoteURL
	if upstreamRemoteURL == "" {
		upstreamRemoteURL, err = jjClient.RemoteURL(ctx, params.UpstreamRemote)
		if err != nil {
			return nil, fmt.Errorf("failed to get remote URL for %s: %w", params.UpstreamRemote, err)
		}
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
		// Fetch the target bookmark from fork remote and delete it.
		// If the push fails (e.g. "stale info" from async post-merge ref
		// changes on GitHub), retry up to 2 more times: re-fetch the
		// bookmark, re-delete to resolve any conflict (jj#7722), and retry.
		fmt.Fprintf(u, "Fetching from %s...\n", u.Styled("remote", params.ForkRemote))
		_, err = jjClient.Run(ctx, "git", "fetch", "--remote", params.ForkRemote, "--branch", bookmarkName)
		if err != nil {
			u.PrintWarning("failed to fetch from %s: %v", params.ForkRemote, err)
		}
		fmt.Fprintf(u, "Deleting bookmark %s...\n", u.Styled("bookmark", bookmarkName))
		_, err = jjClient.Run(ctx, "bookmark", "delete", bookmarkName)
		if err != nil {
			u.PrintWarning("failed to delete bookmark %s: %v", bookmarkName, err)
		}
		fmt.Fprintf(u, "Pushing bookmark deletion to %s...\n", u.Styled("remote", params.ForkRemote))
		_, err = jjClient.Run(ctx, "git", "push", "--remote", params.ForkRemote, "--bookmark", bookmarkName)
		for attempt := 0; err != nil && attempt < 2; attempt++ {
			result, fetchErr := jjClient.Run(ctx, "git", "fetch", "--remote", params.ForkRemote, "--branch", bookmarkName)
			if fetchErr != nil || strings.Contains(result.Stderr, "Nothing changed.") {
				break
			}
			jjClient.Run(ctx, "bookmark", "delete", bookmarkName)
			_, err = jjClient.Run(ctx, "git", "push", "--remote", params.ForkRemote, "--bookmark", bookmarkName)
		}
		if err != nil {
			u.PrintWarning("failed to push bookmark deletion: %v", err)
		}
		// Fetch from upstream to update state
		fmt.Fprintf(u, "Fetching from %s...\n", u.Styled("remote", params.UpstreamRemote))
		_, err = jjClient.Run(ctx, "git", "fetch", "--remote", params.UpstreamRemote)
		if err != nil {
			u.PrintWarning("failed to fetch: %v", err)
		}
		// Abandon the merged change, if present.
		// NOTE: The fetch may have already abandoned it which occurs iff
		// git.abandon-unreachable-commits is true AND none of the change's
		// descendents have bookmarks.
		fmt.Fprintf(u, "Abandoning change %s...\n", u.Styled("change_id", rev.ID))
		_, err = jjClient.Run(ctx, "abandon", fmt.Sprintf("present(%s)", rev.ID))
		if err != nil {
			u.PrintWarning("failed to abandon change: %v", err)
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
	fmt.Fprintf(params.UI, "Updating PR links on sibling reviews...\n")
	if err := cleanupLinksAfterMerge(ctx, jjClient, forgeClient, configMgr, rev.ID, upstreamRemoteURL); err != nil {
		params.UI.PrintWarning("failed to clean up PR links: %v", err)
	}
	return &MergeResult{
		ChangeID: rev.ID,
		Number:   reviewNumber,
	}, nil
}

// cleanupLinksAfterMerge updates PR links on direct children of the merged
// change (reviews whose forge-parent pointed at the merged change).
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

	// Collect open review records (excluding the merged one).
	var openRecords []forge.ReviewRecord
	reviewByChange := make(map[string]*forge.ReviewRecord)
	for i := range records {
		rec := &records[i]
		if rec.Status == forge.ReviewStateOpen && rec.ChangeID != mergedChangeID {
			openRecords = append(openRecords, *rec)
			reviewByChange[rec.ChangeID] = rec
		}
	}
	if len(openRecords) == 0 {
		return nil
	}

	// Bulk-resolve all open reviews in one jj call.
	var revsetParts []string
	for _, rec := range openRecords {
		revsetParts = append(revsetParts, rec.ChangeID)
	}
	revs, err := jjClient.Revs(ctx, strings.Join(revsetParts, "|"))
	if err != nil {
		return fmt.Errorf("failed to resolve revisions: %w", err)
	}
	revByChange := make(map[string]*jj.Rev, len(revs))
	for _, rev := range revs {
		revByChange[rev.ID] = rev
	}

	// Find reviews adjacent to the merged change: direct children whose
	// forge-parent pointed at the merged change.
	affectedIDs := make(map[string]bool)
	parentOf := make(map[string]string)
	childrenOf := make(map[string][]string)

	for _, rec := range openRecords {
		changeID := rec.ChangeID
		rev, ok := revByChange[changeID]
		if !ok {
			continue
		}
		trailers := jj.ParseDescriptionTrailers(rev.Description)
		parentTrailer, found := jj.GetTrailer(trailers, forge.ParentTrailerKey)
		if !found {
			continue
		}
		parentID := parentTrailer.Value
		if parentID == mergedChangeID {
			// This review was a child of the merged change.
			affectedIDs[changeID] = true
			continue
		}
		// Track parent/child relationships among remaining open reviews.
		parentOf[changeID] = parentID
		childrenOf[parentID] = append(childrenOf[parentID], changeID)
	}

	if len(affectedIDs) == 0 {
		return nil
	}

	// Update PR descriptions only for affected reviews.
	for changeID := range affectedIDs {
		rec := reviewByChange[changeID]
		reviewNumber, err := forgeClient.ParseID(rec.ForgeID)
		if err != nil {
			continue
		}

		// Build parent links (excluding the merged change).
		var parentLinks []PRLink
		if pID, ok := parentOf[changeID]; ok {
			if pRec, ok := reviewByChange[pID]; ok {
				pNum, err := forgeClient.ParseID(pRec.ForgeID)
				if err == nil {
					parentLinks = append(parentLinks, PRLink{Number: pNum, URL: pRec.URL})
				}
			}
		}

		// Build child links.
		var childLinks []PRLink
		for _, cID := range childrenOf[changeID] {
			if cRec, ok := reviewByChange[cID]; ok {
				cNum, err := forgeClient.ParseID(cRec.ForgeID)
				if err == nil {
					childLinks = append(childLinks, PRLink{Number: cNum, URL: cRec.URL})
				}
			}
		}

		details, err := forgeClient.GetReview(ctx, upstreamURL, reviewNumber)
		if err != nil {
			continue
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
