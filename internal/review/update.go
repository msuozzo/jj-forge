package review

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

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
	Ordered           bool         // If true, update PRs sequentially in parent-to-child order
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
	skipped := trailerResult.SkippedEmpty + trailerResult.SkippedAnonymous + trailerResult.SkippedImmutable + pushResult.SkippedSynced
	uploadResult := &change.UploadResult{
		Pushed:           pushResult.Pushed,
		Skipped:          skipped,
		SkippedEmpty:     trailerResult.SkippedEmpty,
		SkippedAnonymous: trailerResult.SkippedAnonymous,
		SkippedImmutable: trailerResult.SkippedImmutable,
		SkippedSynced:    pushResult.SkippedSynced,
		TrailersUpdated:  trailerResult.TrailersUpdated,
	}
	// Phase 4: Update PR descriptions with links
	prsUpdated, err := UpdatePRLinks(ctx, jjClient, forgeClient, configMgr, UpdatePRLinksParams{
		Revset:            params.Revset,
		UpstreamRemote:    params.UpstreamRemote,
		UpstreamRemoteURL: params.UpstreamRemoteURL,
		Ordered:           params.Ordered,
	})
	if err != nil {
		return nil, err
	}
	return &UpdateResult{
		UploadResult: uploadResult,
		PRsUpdated:   prsUpdated,
	}, nil
}

// UpdatePRLinksParams contains parameters for UpdatePRLinks.
type UpdatePRLinksParams struct {
	Revset            string // Revset to update
	UpstreamRemote    string // Remote to update PRs on
	UpstreamRemoteURL string // Pre-resolved upstream remote URL (optional; resolved if empty)
	Ordered           bool   // If true, update PRs sequentially in parent-to-child order
}

// UpdatePRLinks updates PR descriptions with parent/child links for the given revset.
// Only PRs for commits in the revset are updated. The revset is expanded to include
// mutable parents and children as context for reading trailers, so that parent and
// child links are accurate even when only a subset of the stack is uploaded.
// If upstreamRemoteURL is non-empty, it is used instead of resolving upstreamRemote.
// Returns the number of PRs updated.
func UpdatePRLinks(
	ctx context.Context,
	jjClient jj.Client,
	forgeClient forge.Forge,
	configMgr *forge.ConfigManager,
	params UpdatePRLinksParams,
) (int, error) {
	revset := params.Revset
	upstreamRemote := params.UpstreamRemote
	// Resolve the target revset whose PRs will be updated.
	targetRevs, err := jjClient.Revs(ctx, revset)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve revset: %w", err)
	}
	targetIDs := make(map[string]bool, len(targetRevs))
	for _, rev := range targetRevs {
		targetIDs[rev.ID] = true
	}

	// Expand to include mutable parents and children for trailer context only.
	expandedRevset := fmt.Sprintf("(%s) | (parents(%s) & mutable()) | (children(%s) & mutable())", revset, revset, revset)
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
	parentOf := make(map[string][]string)   // changeID -> parent changeIDs
	childrenOf := make(map[string][]string) // changeID -> child changeIDs

	for _, rev := range stack {
		trailers := jj.ParseDescriptionTrailers(rev.Description)
		parentTrailers := jj.GetAllTrailers(trailers, forge.ParentTrailerKey)
		for _, pt := range parentTrailers {
			parentID := pt.Value
			parentOf[rev.ID] = append(parentOf[rev.ID], parentID)
			childrenOf[parentID] = append(childrenOf[parentID], rev.ID)
		}
	}

	var upstreamURL string
	if params.UpstreamRemoteURL != "" {
		upstreamURL = params.UpstreamRemoteURL
	} else {
		var err error
		upstreamURL, err = jjClient.RemoteURL(ctx, upstreamRemote)
		if err != nil {
			return 0, fmt.Errorf("failed to get remote URL for %s: %w", upstreamRemote, err)
		}
	}

	// Pre-compute link data and identify PRs to update (serial, local-only).
	type prUpdate struct {
		reviewNumber int
		parentLinks  []PRLink
		childLinks   []PRLink
	}
	var updates []prUpdate
	for _, rev := range stack {
		if !targetIDs[rev.ID] {
			continue // Context-only rev (parent/child), don't update its PR
		}
		rec, ok := reviewByChange[rev.ID]
		if !ok {
			continue // No open review for this change
		}
		reviewNumber, err := forgeClient.ParseID(rec.ForgeID)
		if err != nil {
			return 0, fmt.Errorf("invalid review ID %s: %w", rec.ForgeID, err)
		}
		// Build parent links
		var parentLinks []PRLink
		for _, pID := range parentOf[rev.ID] {
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
		updates = append(updates, prUpdate{
			reviewNumber: reviewNumber,
			parentLinks:  parentLinks,
			childLinks:   childLinks,
		})
	}
	if len(updates) == 0 {
		return 0, nil
	}

	var prsUpdated atomic.Int32
	updateOne := func(u prUpdate) error {
		details, err := forgeClient.GetReview(ctx, upstreamURL, u.reviewNumber)
		if err != nil {
			return fmt.Errorf("failed to get review #%d: %w", u.reviewNumber, err)
		}
		newBody := SetPRLinks(details.Body, u.parentLinks, u.childLinks)
		if newBody != details.Body {
			if err := forgeClient.UpdateReview(ctx, upstreamURL, u.reviewNumber, newBody); err != nil {
				return fmt.Errorf("failed to update review #%d: %w", u.reviewNumber, err)
			}
			prsUpdated.Add(1)
		}
		return nil
	}

	if params.Ordered {
		// Sequential updates in parent-to-child order.
		for _, u := range updates {
			if err := updateOne(u); err != nil {
				return 0, err
			}
		}
	} else {
		// Parallel forge API calls.
		var wg sync.WaitGroup
		errCh := make(chan error, len(updates))
		for _, u := range updates {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := updateOne(u); err != nil {
					errCh <- err
				}
			}()
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			return 0, err
		}
	}
	return int(prsUpdated.Load()), nil
}
