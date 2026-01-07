package review

import (
	"context"
	"fmt"
	"strings"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// OpenParams contains parameters for the open command.
type OpenParams struct {
	Rev            string   // Revset to open review for
	Reviewers      []string // Reviewer usernames
	UpstreamRemote string   // Remote to create PR against
	ForkRemote     string   // Remote where the branch is pushed
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
	if strings.TrimSpace(rev.Description) == "" {
		return nil, fmt.Errorf("change %s has empty description. Add a description with: jj describe %s", rev.ID, rev.ID)
	}
	if !isUploaded(rev, params.ForkRemote) {
		return nil, fmt.Errorf("change %s has not been uploaded to %s. Run: jj-forge change upload %s", rev.ID, params.ForkRemote, rev.ID)
	}
	// Check if a review already exists
	records, err := configMgr.GetReviewRecords()
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}
	for _, record := range records {
		if record.ChangeID == rev.ID {
			if record.Status == "open" {
				return nil, fmt.Errorf("review already exists for change %s: %s", rev.ID, record.URL)
			} else if record.Status == "merged" {
				return nil, fmt.Errorf("change %s was already merged in review %s", rev.ID, record.ForgeID)
			}
			// If status is "closed", we can create a new review
		}
	}
	// Determine base branch
	upstreamRemoteURL, err := jjClient.RemoteURL(ctx, params.UpstreamRemote)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote URL for %s: %w", params.UpstreamRemote, err)
	}
	upstreamBranch, err := forgeClient.DefaultBranch(ctx, upstreamRemoteURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch: %w", err)
	}
	// Determine fork branch
	forkRepoInfo, err := forge.GetRepoInfo(ctx, jjClient, params.ForkRemote)
	if err != nil {
		return nil, fmt.Errorf("failed to get head remote info: %w", err)
	}
	forkBranch := fmt.Sprintf("%s:push-%s", forkRepoInfo.Owner, rev.ID)
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
		Status:   "open",
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
