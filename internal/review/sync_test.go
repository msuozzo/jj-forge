package review

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/forge/github"
	"github.com/msuozzo/jj-forge/internal/forge/tangled"
	"github.com/msuozzo/jj-forge/internal/jjtest"
)

const tangledRepoURL = "git@tangled.org:alice.example.com/repo"

func tangledRecord(changeID, rkey string, status forge.ReviewState) forge.ReviewRecord {
	return forge.ReviewRecord{
		ChangeID: changeID,
		ForgeID:  "pr/" + rkey,
		URL:      "https://tangled.org/alice.example.com/repo/pulls",
		Status:   status,
	}
}

// createTangledReview opens a review in the fake and returns its record.
func createTangledReview(t *testing.T, f *tangled.FakeForge, changeID string, status forge.ReviewState) forge.ReviewRecord {
	t.Helper()
	res, err := f.CreateReview(context.Background(), tangledRepoURL, forge.ReviewCreateParams{
		Title: "feat: " + changeID, FromBranch: "push-" + changeID, ToBranch: "main",
	})
	if err != nil {
		t.Fatalf("CreateReview() error = %v", err)
	}
	switch status {
	case forge.ReviewStateMerged:
		if err := f.MergeReview(context.Background(), tangledRepoURL, res.ID, "c0ffee"); err != nil {
			t.Fatal(err)
		}
	case forge.ReviewStateClosed:
		if err := f.CloseReview(context.Background(), tangledRepoURL, res.ID); err != nil {
			t.Fatal(err)
		}
	}
	return tangledRecord(changeID, res.ID, status)
}

func TestSyncReviews_SubmitsRoundsForOpenReviews(t *testing.T) {
	fakeForge := tangled.NewFakeForge()
	openRec := createTangledReview(t, fakeForge, "aaaaaaaaaaaa", forge.ReviewStateOpen)
	mergedRec := createTangledReview(t, fakeForge, "bbbbbbbbbbbb", forge.ReviewStateMerged)
	closedRec := createTangledReview(t, fakeForge, "cccccccccccc", forge.ReviewStateClosed)

	scenario := jjtest.NewScenario(t, jjtest.NewFakeRepo(),
		configListCall(openRec, mergedRec, closedRec),
	)
	configMgr := forge.NewConfigManager(scenario.Client())

	// Push touched all three plus a change without any review.
	pushed := []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb", "cccccccccccc", "dddddddddddd"}
	synced, err := SyncReviews(context.Background(), fakeForge, configMgr, tangledRepoURL, pushed, nil)
	if err != nil {
		t.Fatalf("SyncReviews() error = %v", err)
	}
	if synced != 1 {
		t.Errorf("expected 1 review synced, got %d", synced)
	}
	openID, _ := fakeForge.ParseID(openRec.ForgeID)
	if got := fakeForge.SyncedIDs(); len(got) != 1 || got[0] != openID {
		t.Errorf("expected only the open review to be synced, got %v", got)
	}
	review, _ := fakeForge.GetTestReview(openID)
	if review.Rounds != 2 {
		t.Errorf("expected 2 rounds after sync, got %d", review.Rounds)
	}
	scenario.Verify()
}

func TestSyncReviews_NoopForBranchTrackingForge(t *testing.T) {
	// GitHub's fake does not implement forge.ReviewSyncer, so no config reads
	// should happen at all.
	fakeForge := github.NewFakeForge()
	scenario := jjtest.NewScenario(t, jjtest.NewFakeRepo())
	configMgr := forge.NewConfigManager(scenario.Client())

	synced, err := SyncReviews(context.Background(), fakeForge, configMgr, "github.com/owner/repo", []string{"aaaaaaaaaaaa"}, nil)
	if err != nil {
		t.Fatalf("SyncReviews() error = %v", err)
	}
	if synced != 0 {
		t.Errorf("expected 0 synced, got %d", synced)
	}
	scenario.Verify()
}

func TestSyncReviews_NothingPushed(t *testing.T) {
	fakeForge := tangled.NewFakeForge()
	scenario := jjtest.NewScenario(t, jjtest.NewFakeRepo())
	configMgr := forge.NewConfigManager(scenario.Client())

	synced, err := SyncReviews(context.Background(), fakeForge, configMgr, tangledRepoURL, nil, nil)
	if err != nil {
		t.Fatalf("SyncReviews() error = %v", err)
	}
	if synced != 0 {
		t.Errorf("expected 0 synced, got %d", synced)
	}
	scenario.Verify()
}

func TestSyncReviews_PropagatesSyncError(t *testing.T) {
	fakeForge := tangled.NewFakeForge()
	openRec := createTangledReview(t, fakeForge, "aaaaaaaaaaaa", forge.ReviewStateOpen)
	fakeForge.SetSyncError(errors.New("base is not an ancestor of head"))

	scenario := jjtest.NewScenario(t, jjtest.NewFakeRepo(),
		configListCall(openRec),
	)
	configMgr := forge.NewConfigManager(scenario.Client())

	_, err := SyncReviews(context.Background(), fakeForge, configMgr, tangledRepoURL, []string{"aaaaaaaaaaaa"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not an ancestor") {
		t.Fatalf("expected sync error to propagate, got %v", err)
	}
	if !strings.Contains(err.Error(), "aaaaaaaaaaaa") {
		t.Errorf("expected error to name the change, got %v", err)
	}
}

// TestUpdate_SyncsTangledReviewsAfterPush exercises the full update flow with
// the Tangled fake: the pushed change's open review receives a new round
// before PR links are updated.
func TestUpdate_SyncsTangledReviewsAfterPush(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:          "aaaaaaaaaaaa",
		Parents:     []string{"root"},
		Description: "feat: test\n",
		IsMutable:   true,
		// No remote bookmark: change needs pushing.
	})

	fakeForge := tangled.NewFakeForge()
	openRec := createTangledReview(t, fakeForge, "aaaaaaaaaaaa", forge.ReviewStateOpen)
	openID, _ := fakeForge.ParseID(openRec.ForgeID)

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers: resolve revset and its parents (single commit, no trailer changes)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(@)~(@)"},
			Output: jjtest.LogOutput("root"),
		},
		// Push (trailers unchanged, revs reused, no remote bookmark so it is pushed)
		jjtest.Call{Args: []string{"git", "push", "--change", "aaaaaaaaaaaa", "--remote", "up"}},
		// SyncReviews: read records
		configListCall(openRec),
		// UpdatePRLinks: resolve target revset and expanded revset
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "(@) | (parents(@) & mutable()) | (children(@) & mutable())"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
	)
	configMgr := forge.NewConfigManager(scenario.Client())

	result, err := Update(context.Background(), scenario.Client(), fakeForge, configMgr, UpdateParams{
		Revset:            "@",
		ForkRemote:        "up",
		UpstreamRemote:    "up",
		UpstreamRemoteURL: tangledRepoURL,
		UI:                testUI,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.ReviewsSynced != 1 {
		t.Errorf("expected 1 review synced, got %d", result.ReviewsSynced)
	}
	if result.UploadResult.Pushed != 1 {
		t.Errorf("expected 1 pushed, got %d", result.UploadResult.Pushed)
	}
	if got := fakeForge.SyncedIDs(); len(got) != 1 || got[0] != openID {
		t.Errorf("expected review %s synced, got %v", openID, got)
	}
	scenario.Verify()
}
