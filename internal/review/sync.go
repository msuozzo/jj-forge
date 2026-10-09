package review

import (
	"context"
	"fmt"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/ui"
)

// SyncReviews re-submits the content of open reviews for the given changes on
// forges that snapshot review content at submission time (see
// forge.ReviewSyncer). Changes without an open review record are skipped.
// It is a no-op for forges that track branches directly.
//
// changeIDs should be the changes whose branches were just pushed. Returns
// the number of reviews synced.
func SyncReviews(
	ctx context.Context,
	forgeClient forge.Forge,
	configMgr *forge.ConfigManager,
	upstreamURL string,
	changeIDs []string,
	tr *ui.TaskTracker,
) (int, error) {
	syncer, ok := forgeClient.(forge.ReviewSyncer)
	if !ok || len(changeIDs) == 0 {
		return 0, nil
	}
	synced := 0
	for _, changeID := range changeIDs {
		rec, err := configMgr.GetReviewByChangeID(changeID)
		if err != nil {
			return synced, fmt.Errorf("failed to read config: %w", err)
		}
		if rec == nil || rec.Status != forge.ReviewStateOpen {
			continue
		}
		reviewID, err := forgeClient.ParseID(rec.ForgeID)
		if err != nil {
			return synced, fmt.Errorf("invalid review ID %s: %w", rec.ForgeID, err)
		}
		if tr != nil {
			tr.SetMessageByName(changeID, "syncing review")
			tr.SetStatusByName(changeID, ui.TaskRunning)
		}
		if err := syncer.SyncReview(ctx, upstreamURL, reviewID); err != nil {
			if tr != nil {
				tr.SetStatusByName(changeID, ui.TaskFailed)
			}
			return synced, fmt.Errorf("failed to sync review #%s for change %s: %w", reviewID, changeID, err)
		}
		if tr != nil {
			tr.SetMessageByName(changeID, "synced")
			tr.SetStatusByName(changeID, ui.TaskPending)
		}
		synced++
	}
	return synced, nil
}
