package review

import (
	"context"
	"errors"
	"fmt"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

var (
	ErrReviewAlreadyExists = errors.New("review already exists")
)

// OpenParams contains parameters for the open command.
type OpenParams struct {
	Rev               string   // Revset to open review for
	Reviewers         []string // Reviewer usernames
	UpstreamRemote    string   // Remote to create PR against
	UpstreamRemoteURL string   // Pre-resolved upstream remote URL (optional; resolved if empty)
	ForkRemote        string   // Remote where the branch is pushed
	TargetBranch      string   // Base branch to create PR against (optional; default branch used if empty)
}

// OpenResult contains the result of the open command.
type OpenResult struct {
	ChangeID string
	Number   int
	URL      string
}

// Open creates a new code review for a change.
func Open(
	ctx context.Context,
	jjClient jj.Client,
	forgeClient forge.Forge,
	configMgr *forge.ConfigManager,
	params OpenParams,
) (*OpenResult, error) {
	rev, err := jjClient.Rev(ctx, params.Rev)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve revision %s: %w", params.Rev, err)
	}
	// Validate the change
	if err := Validate(rev, nil,
		RequireHasDescription,
		RequireUploaded(params.ForkRemote),
	); err != nil {
		return nil, err
	}
	// Check if a review already exists
	existingRecord, err := configMgr.GetReviewByChangeID(rev.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}
	if existingRecord != nil {
		switch existingRecord.Status {
		case forge.ReviewStateOpen:
			return nil, fmt.Errorf("%w for change %s: %s", ErrReviewAlreadyExists, rev.ID, existingRecord.URL)
		case forge.ReviewStateMerged:
			return nil, fmt.Errorf("%w: change %s was already merged in review %s", ErrReviewAlreadyExists, rev.ID, existingRecord.ForgeID)
		}
		// If status is closed, we can create a new review
	}
	// Determine base branch
	upstreamRemoteURL := params.UpstreamRemoteURL
	if upstreamRemoteURL == "" {
		upstreamRemoteURL, err = jjClient.RemoteURL(ctx, params.UpstreamRemote)
		if err != nil {
			return nil, fmt.Errorf("failed to get remote URL for %s: %w", params.UpstreamRemote, err)
		}
	}
	var upstreamBranch string
	if params.TargetBranch != "" {
		upstreamBranch = params.TargetBranch
	} else {
		var err error
		upstreamBranch, err = forgeClient.DefaultBranch(ctx, upstreamRemoteURL)
		if err != nil {
			return nil, fmt.Errorf("failed to get default branch: %w", err)
		}
	}
	// Determine fork branch
	forkBranch, err := forgeClient.FormatHeadBranch(ctx, jjClient, params.ForkRemote, rev.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get head branch: %w", err)
	}
	// Exclude forge-parent trailer from PR description
	description := forge.RemoveParentTrailer(rev.Description)
	// Create review
	title, body := splitTitleBody(description)
	result, err := forgeClient.CreateReview(ctx, upstreamRemoteURL, forge.ReviewCreateParams{
		Title:      title,
		Body:       body,
		FromBranch: forkBranch,
		ToBranch:   upstreamBranch,
		Reviewers:  params.Reviewers,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create review: %w", err)
	}
	// Store review in config
	record := forge.ReviewRecord{
		ChangeID: rev.ID,
		ForgeID:  forgeClient.FormatID(result.Number),
		URL:      result.URL,
		Status:   forge.ReviewStateOpen,
	}
	if err := configMgr.AddReviewRecord(record); err != nil {
		return nil, fmt.Errorf("failed to save review record: %w", err)
	}
	return &OpenResult{
		ChangeID: rev.ID,
		Number:   result.Number,
		URL:      result.URL,
	}, nil
}
