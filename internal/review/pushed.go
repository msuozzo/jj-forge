package review

import (
	"context"
	"fmt"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// PushedReview is a change's open review and the commit pushed for it.
type PushedReview struct {
	ChangeID string
	ReviewID string // Forge-native review ID
	CommitID string // Full ID of the pushed commit
}

// FindPushedReview returns the open review of the change at rev. The change
// must be pushed to forkRemote as it is locally, since checks on a stale push
// say nothing about the local commit.
func FindPushedReview(
	ctx context.Context,
	jjClient jj.Client,
	forgeClient forge.Forge,
	configMgr *forge.ConfigManager,
	rev, forkRemote string,
) (*PushedReview, error) {
	r, err := jjClient.Rev(ctx, rev)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve revision %s: %w", rev, err)
	}
	record, err := configMgr.GetReviewByChangeID(r.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}
	if err := Validate(r, record, RequireReviewExists, RequireReviewOpen, RequireUploaded(forkRemote)); err != nil {
		return nil, err
	}
	reviewID, err := forgeClient.ParseID(record.ForgeID)
	if err != nil {
		return nil, fmt.Errorf("invalid review ID in config: %s", record.ForgeID)
	}
	commitID, err := fullCommitID(ctx, jjClient, r.CommitID)
	if err != nil {
		return nil, err
	}
	return &PushedReview{ChangeID: r.ID, ReviewID: reviewID, CommitID: commitID}, nil
}
