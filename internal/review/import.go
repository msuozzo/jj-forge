package review

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// ImportParams contains parameters for the import command.
type ImportParams struct {
	Revset         string
	UpstreamRemote string
	All            bool
}

// ImportResult contains the summary of the import operation.
type ImportResult struct {
	Updated int
	Added   int
}

// Import finds and updates review records.
func Import(ctx context.Context, jjClient jj.Client, forgeClient forge.Forge, configMgr *forge.ConfigManager, params ImportParams) (*ImportResult, error) {
	upstreamURL, err := jjClient.RemoteURL(ctx, params.UpstreamRemote)
	if err != nil {
		return nil, fmt.Errorf("failed to get upstream remote URL: %w", err)
	}

	records, err := configMgr.GetReviewRecords()
	if err != nil {
		return nil, fmt.Errorf("failed to get review records: %w", err)
	}

	recordMap := make(map[string]forge.ReviewRecord)
	for _, r := range records {
		recordMap[r.ChangeID] = r
	}

	if params.Revset == "" && !params.All {
		return nil, fmt.Errorf("revset is required when --all is not set")
	}
	var revsToCheck []*jj.Rev
	if params.All {
		revsToCheck, err = jjClient.Revs(ctx, "mutable()")
	} else {
		revsToCheck, err = jjClient.Revs(ctx, params.Revset)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get revisions: %w", err)
	}

	changesToProcess := make(map[string]struct{})
	for _, r := range records {
		changesToProcess[r.ChangeID] = struct{}{}
	}

	revMap := make(map[string]*jj.Rev)
	for _, r := range revsToCheck {
		changesToProcess[r.ID] = struct{}{}
		revMap[r.ID] = r
	}

	workCh := make(chan string, len(changesToProcess))
	for cid := range changesToProcess {
		workCh <- cid
	}
	close(workCh)

	var wg sync.WaitGroup
	resultCh := make(chan processResult, len(changesToProcess))

	concurrency := 10
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for changeID := range workCh {
				rec, ok := recordMap[changeID]
				var recPtr *forge.ReviewRecord
				if ok {
					recPtr = &rec
				}
				resultCh <- processChange(ctx, changeID, recPtr, revMap[changeID], forgeClient, upstreamURL)
			}
		}()
	}

	wg.Wait()
	close(resultCh)

	finalRecords := make(map[string]forge.ReviewRecord)
	// Initialize with existing records
	for k, v := range recordMap {
		finalRecords[k] = v
	}

	res := &ImportResult{}
	var errs []error

	for r := range resultCh {
		if r.Err != nil {
			errs = append(errs, r.Err)
			continue
		}
		if r.Record != nil {
			finalRecords[r.Record.ChangeID] = *r.Record
			if r.Added {
				res.Added++
			}
			if r.Updated {
				res.Updated++
			}
		}
	}

	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Printf("Warning: %v\n", e)
		}
	}

	if res.Added > 0 || res.Updated > 0 {
		newRecordList := make([]forge.ReviewRecord, 0, len(finalRecords))
		for _, r := range finalRecords {
			newRecordList = append(newRecordList, r)
		}
		slices.SortFunc(newRecordList, func(a, b forge.ReviewRecord) int {
			return strings.Compare(a.ChangeID, b.ChangeID)
		})
		if err := configMgr.SaveRecords(newRecordList); err != nil {
			return nil, fmt.Errorf("failed to save records: %w", err)
		}
	}

	return res, nil
}

// processResult holds the outcome of processing a single change.
type processResult struct {
	Record  *forge.ReviewRecord
	Added   bool
	Updated bool
	Err     error
}

func processChange(ctx context.Context, changeID string, record *forge.ReviewRecord, rev *jj.Rev, forgeClient forge.Forge, repoURI string) processResult {
	if record != nil {
		number, err := forgeClient.ParseID(record.ForgeID)
		if err != nil {
			return processResult{Err: err}
		}
		details, err := forgeClient.GetReview(ctx, repoURI, number)
		if err != nil {
			return processResult{Err: err}
		}
		if details.State != record.Status {
			record.Status = details.State
			return processResult{Record: record, Updated: true}
		}
		return processResult{Record: record}
	}

	if rev != nil {
		candidates := []string{}
		candidates = append(candidates, rev.Bookmarks...)
		for _, rb := range rev.RemoteBookmarks {
			parts := strings.SplitN(rb, "/", 2)
			if len(parts) == 2 {
				candidates = append(candidates, parts[1])
			}
		}
		slices.Sort(candidates)
		candidates = slices.Compact(candidates)

		var findErrors []error
		for _, branch := range candidates {
			details, err := forgeClient.FindReview(ctx, repoURI, branch)
			if err != nil {
				findErrors = append(findErrors, fmt.Errorf("FindReview(%s): %w", branch, err))
				continue
			}
			if details != nil {
				return processResult{
					Record: &forge.ReviewRecord{
						ChangeID: rev.ID,
						ForgeID:  forgeClient.FormatID(details.Number),
						URL:      details.URL,
						Status:   details.State,
					},
					Added: true,
				}
			}
		}
		if len(findErrors) > 0 {
			return processResult{Err: fmt.Errorf("errors searching for reviews for %s: %v", changeID, findErrors)}
		}
	}

	return processResult{}
}
