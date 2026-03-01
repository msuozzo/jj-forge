package review

import (
	"context"
	"fmt"
	"slices"

	"github.com/msuozzo/jj-forge/internal/change"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
	"github.com/msuozzo/jj-forge/internal/ui"
)

// UpdateParams contains parameters for the update command.
type UpdateParams struct {
	Revset            string       // Revset to update
	ForkRemote        string       // Remote where branches are pushed
	UpstreamRemote    string       // Remote to update PRs on
	UpstreamRemoteURL string       // Pre-resolved upstream remote URL (optional; resolved if empty)
	UI                *ui.UI       // UI for styled output
	CheckFn           func() error // Optional: runs between trailer updates and push
}

// UpdateResult contains the result of the update command.
type UpdateResult struct {
	UploadResult *change.UploadResult
	PRsUpdated   int
}

// Update uploads content and updates PR descriptions with parent/child links.
func Update(
	ctx context.Context,
	jjClient jj.Client,
	forgeClient forge.Forge,
	configMgr *forge.ConfigManager,
	params UpdateParams,
) (*UpdateResult, error) {
	// Phase 1: Update trailers
	trailerResult, err := change.UpdateTrailers(ctx, jjClient, params.Revset, params.UI)
	if err != nil {
		return nil, fmt.Errorf("upload failed: %w", err)
	}
	// Phase 2: Run checks (if configured)
	if params.CheckFn != nil {
		if err := params.CheckFn(); err != nil {
			return nil, err
		}
	}
	// Phase 3: Push
	// If no trailers were updated, commit IDs haven't changed — reuse resolved revs.
	var preResolved []*jj.Rev
	if trailerResult.TrailersUpdated == 0 {
		preResolved = trailerResult.Revs
	}
	pushResult, err := change.Push(ctx, jjClient, params.Revset, params.ForkRemote, params.UI, preResolved)
	if err != nil {
		return nil, fmt.Errorf("upload failed: %w", err)
	}
	skipped := trailerResult.SkippedEmpty + trailerResult.SkippedAnonymous + pushResult.SkippedSynced
	uploadResult := &change.UploadResult{
		Pushed:           pushResult.Pushed,
		Skipped:          skipped,
		SkippedEmpty:     trailerResult.SkippedEmpty,
		SkippedAnonymous: trailerResult.SkippedAnonymous,
		SkippedSynced:    pushResult.SkippedSynced,
		TrailersUpdated:  trailerResult.TrailersUpdated,
	}
	// Phase 4: Update PR descriptions with links
	prsUpdated, err := UpdatePRLinks(ctx, jjClient, forgeClient, configMgr, params.Revset, params.UpstreamRemote, params.UpstreamRemoteURL)
	if err != nil {
		return nil, err
	}
	return &UpdateResult{
		UploadResult: uploadResult,
		PRsUpdated:   prsUpdated,
	}, nil
}

// UpdatePRLinks updates PR descriptions with parent/child links for the given revset.
// The revset is expanded to include mutable parents so that parent PRs get child links
// even when only a subset of the stack is passed.
// If upstreamRemoteURL is non-empty, it is used instead of resolving upstreamRemote.
// Returns the number of PRs updated.
func UpdatePRLinks(
	ctx context.Context,
	jjClient jj.Client,
	forgeClient forge.Forge,
	configMgr *forge.ConfigManager,
	revset string,
	upstreamRemote string,
	upstreamRemoteURL ...string,
) (int, error) {
	expandedRevset := fmt.Sprintf("(%s) | (parents(%s) & mutable())", revset, revset)
	stack, err := jjClient.Revs(ctx, expandedRevset)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve expanded revset: %w", err)
	}
	slices.Reverse(stack) // parent-first order
	if len(stack) == 0 {
		return 0, nil
	}

	// Get all review records
	records, err := configMgr.GetReviewRecords()
	if err != nil {
		return 0, fmt.Errorf("failed to get review records: %w", err)
	}
	reviewByChange := make(map[string]*forge.ReviewRecord)
	for i := range records {
		if records[i].Status == forge.ReviewStateOpen {
			reviewByChange[records[i].ChangeID] = &records[i]
		}
	}

	// Build parent/child map from forge-parent trailers
	parentOf := make(map[string]string)     // changeID -> parent changeID
	childrenOf := make(map[string][]string) // changeID -> child changeIDs

	stackIDs := make(map[string]bool)
	for _, rev := range stack {
		stackIDs[rev.ID] = true
	}

	for _, rev := range stack {
		trailers := jj.ParseDescriptionTrailers(rev.Description)
		parentTrailer, found := jj.GetTrailer(trailers, forge.ParentTrailerKey)
		if found {
			parentID := parentTrailer.Value
			parentOf[rev.ID] = parentID
			childrenOf[parentID] = append(childrenOf[parentID], rev.ID)
		}
	}

	var upstreamURL string
	if len(upstreamRemoteURL) > 0 && upstreamRemoteURL[0] != "" {
		upstreamURL = upstreamRemoteURL[0]
	} else {
		var err error
		upstreamURL, err = jjClient.RemoteURL(ctx, upstreamRemote)
		if err != nil {
			return 0, fmt.Errorf("failed to get remote URL for %s: %w", upstreamRemote, err)
		}
	}

	prsUpdated := 0
	for _, rev := range stack {
		rec, ok := reviewByChange[rev.ID]
		if !ok {
			continue // No open review for this change
		}
		reviewNumber, err := forgeClient.ParseID(rec.ForgeID)
		if err != nil {
			return 0, fmt.Errorf("invalid review ID %s: %w", rec.ForgeID, err)
		}

		// Get current PR details from forge
		details, err := forgeClient.GetReview(ctx, upstreamURL, reviewNumber)
		if err != nil {
			return 0, fmt.Errorf("failed to get review #%d: %w", reviewNumber, err)
		}

		// Build parent links
		var parentLinks []PRLink
		if pID, ok := parentOf[rev.ID]; ok {
			if pRec, ok := reviewByChange[pID]; ok {
				pNum, err := forgeClient.ParseID(pRec.ForgeID)
				if err == nil {
					parentLinks = append(parentLinks, PRLink{Number: pNum, URL: pRec.URL})
				}
			}
		}

		// Build child links
		var childLinks []PRLink
		for _, cID := range childrenOf[rev.ID] {
			if cRec, ok := reviewByChange[cID]; ok {
				cNum, err := forgeClient.ParseID(cRec.ForgeID)
				if err == nil {
					childLinks = append(childLinks, PRLink{Number: cNum, URL: cRec.URL})
				}
			}
		}

		// Update body with links
		newBody := SetPRLinks(details.Body, parentLinks, childLinks)
		if newBody != details.Body {
			if err := forgeClient.UpdateReview(ctx, upstreamURL, reviewNumber, newBody); err != nil {
				return 0, fmt.Errorf("failed to update review #%d: %w", reviewNumber, err)
			}
			prsUpdated++
		}
	}

	return prsUpdated, nil
}
