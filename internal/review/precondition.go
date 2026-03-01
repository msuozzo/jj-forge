package review

import (
	"errors"
	"fmt"
	"strings"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// Precondition validates a single property of a revision and its review record.
// record may be nil when no review exists for the revision.
type Precondition func(rev *jj.Rev, record *forge.ReviewRecord) error

// Validate runs all preconditions and returns all errors joined together.
// Unlike short-circuit validation, this reports all failures at once so the
// user can fix multiple issues in one pass. Sentinel errors wrapped by
// individual preconditions are preserved through errors.Join.
func Validate(rev *jj.Rev, record *forge.ReviewRecord, checks ...Precondition) error {
	var errs []error
	for _, check := range checks {
		if err := check(rev, record); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RequireReviewExists checks that a review record exists for the change.
func RequireReviewExists(rev *jj.Rev, record *forge.ReviewRecord) error {
	if record == nil {
		return fmt.Errorf("no review found for change %s. Create one with: jj-forge review open %s", rev.ID, rev.ID)
	}
	return nil
}

// RequireReviewOpen checks that the review is in the open state.
func RequireReviewOpen(rev *jj.Rev, record *forge.ReviewRecord) error {
	if record == nil {
		return nil // RequireReviewExists handles this case
	}
	if record.Status == forge.ReviewStateMerged {
		return fmt.Errorf("review #%s for change %s is already merged", record.ForgeID, rev.ID)
	}
	if record.Status == forge.ReviewStateClosed {
		return fmt.Errorf("review #%s for change %s is closed. Reopen it or create a new review", record.ForgeID, rev.ID)
	}
	return nil
}

// RequireReviewNotClosed checks that the review is not already closed.
func RequireReviewNotClosed(rev *jj.Rev, record *forge.ReviewRecord) error {
	if record == nil {
		return nil
	}
	if record.Status == forge.ReviewStateClosed {
		return fmt.Errorf("review #%s for change %s is already closed", record.ForgeID, rev.ID)
	}
	return nil
}

// RequireReviewNotMerged checks that the review is not already merged.
func RequireReviewNotMerged(rev *jj.Rev, record *forge.ReviewRecord) error {
	if record == nil {
		return nil
	}
	if record.Status == forge.ReviewStateMerged {
		return fmt.Errorf("review #%s for change %s is already merged", record.ForgeID, rev.ID)
	}
	return nil
}

// RequireUploaded returns a precondition that checks the change has been pushed
// to the given remote.
func RequireUploaded(remote string) Precondition {
	return func(rev *jj.Rev, record *forge.ReviewRecord) error {
		if !isUploaded(rev, remote) {
			return fmt.Errorf("change %s has local modifications not yet pushed to %s: %w", rev.ID, remote, ErrNotUploaded)
		}
		return nil
	}
}

// RequireNoParentTrailer checks that the change does not have a forge-parent
// trailer, which would indicate a parent change should be merged first.
func RequireNoParentTrailer(rev *jj.Rev, record *forge.ReviewRecord) error {
	if parentTrailer, found := jj.GetTrailer(jj.ParseDescriptionTrailers(rev.Description), forge.ParentTrailerKey); found {
		return fmt.Errorf("change %s has a forge-parent annotation (parent: %s). Merge or close the parent review first, then run 'jj-forge review update' to refresh: %w", rev.ID, parentTrailer.Value, ErrHasParentTrailer)
	}
	return nil
}

// RequireHasDescription checks that the change has a non-empty description.
func RequireHasDescription(rev *jj.Rev, record *forge.ReviewRecord) error {
	if strings.TrimSpace(rev.Description) == "" {
		return fmt.Errorf("change %s has empty description. Add a description with: jj describe %s", rev.ID, rev.ID)
	}
	return nil
}
