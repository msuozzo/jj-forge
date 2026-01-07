package review

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// CloseParams contains parameters for the close command.
type CloseParams struct {
	Rev            string // Revset to close review for
	ForkRemote     string // Remote where the branch is pushed
	UpstreamRemote string // Remote to close PR in
	Force          bool   // Skip confirmation if true
	NoCleanup      bool   // Skip local cleanup if true
}

// CloseResult contains the result of the close command.
type CloseResult struct {
	ChangeID string
	Number   int
}

// Close closes a code review and abandons the local change.
func Close(
	ctx context.Context,
	jjClient jj.Client,
	forgeClient forge.Forge,
	configMgr *forge.ConfigManager,
	params CloseParams,
) (*CloseResult, error) {
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
		return nil, fmt.Errorf("no review found for change %s", rev.ID)
	}
	if reviewRecord.Status == forge.ReviewStateClosed {
		return nil, fmt.Errorf("review #%s for change %s is already closed", reviewRecord.ForgeID, rev.ID)
	}
	if reviewRecord.Status == forge.ReviewStateMerged {
		return nil, fmt.Errorf("review #%s for change %s is already merged", reviewRecord.ForgeID, rev.ID)
	}
	reviewNumber, err := forgeClient.ParseID(reviewRecord.ForgeID)
	if err != nil {
		return nil, fmt.Errorf("invalid review number in config: %s", reviewRecord.ForgeID)
	}
	// Prompt for confirmation (unless --force)
	if !params.Force {
		fmt.Printf("This will close review #%d and abandon change %s. Continue? [y/N] ", reviewNumber, rev.ID)
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("failed to read confirmation: %w", err)
		}

		response = strings.ToLower(strings.TrimSpace(response))
		if response != "y" && response != "yes" {
			return nil, fmt.Errorf("operation cancelled")
		}
	}
	upstreamRemoteURL, err := jjClient.RemoteURL(ctx, params.UpstreamRemote)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote URL for %s: %w", params.UpstreamRemote, err)
	}
	// Close review via forge
	if err := forgeClient.CloseReview(ctx, upstreamRemoteURL, reviewNumber); err != nil {
		return nil, fmt.Errorf("failed to close review: %w", err)
	}
	// Cleanup (unless --no-cleanup)
	if !params.NoCleanup {
		bookmarkName := fmt.Sprintf("push-%s", rev.ID)
		// Delete bookmark
		fmt.Printf("Deleting bookmark %s...\n", bookmarkName)
		_, err = jjClient.Run(ctx, "bookmark", "delete", bookmarkName)
		if err != nil {
			fmt.Printf("Warning: failed to delete bookmark %s: %v\n", bookmarkName, err)
		}
		// Push bookmark deletion
		fmt.Printf("Pushing bookmark deletion to %s...\n", params.ForkRemote)
		_, err = jjClient.Run(ctx, "git", "push", "--remote", params.ForkRemote, "--bookmark", bookmarkName)
		if err != nil {
			fmt.Printf("Warning: failed to push bookmark deletion: %v\n", err)
		}
		// Abandon the change
		fmt.Printf("Abandoning change %s...\n", rev.ID)
		_, err = jjClient.Run(ctx, "abandon", rev.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to abandon change: %w", err)
		}
	}
	// Update config status to closed
	reviewRecord.Status = forge.ReviewStateClosed
	if err := configMgr.AddReviewRecord(*reviewRecord); err != nil {
		return nil, fmt.Errorf("failed to update review status: %w", err)
	}
	return &CloseResult{
		ChangeID: rev.ID,
		Number:   reviewNumber,
	}, nil
}
