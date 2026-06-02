package review

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/forge/github"
	"github.com/msuozzo/jj-forge/internal/jjtest"
)

// Test helpers for import tests.

func makeRecord(changeID string, number int, status forge.ReviewState) forge.ReviewRecord {
	return forge.ReviewRecord{
		ChangeID: changeID,
		ForgeID:  fmt.Sprintf("pr/%d", number),
		URL:      fmt.Sprintf("https://github.com/owner/repo/pull/%d", number),
		Status:   status,
	}
}

func formatConfigValue(records ...forge.ReviewRecord) string {
	var parts []string
	for _, r := range records {
		parts = append(parts, fmt.Sprintf(`"%s\n%s\n%s\n%s"`, r.ChangeID, r.ForgeID, r.URL, string(r.Status)))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func importRemoteListCall() jjtest.Call {
	return jjtest.Call{
		Args: []string{"git", "remote", "list"},
		Output: func(r *jjtest.FakeRepo) string {
			return "up git@github.com:owner/repo.git\n"
		},
	}
}

func configListCall(records ...forge.ReviewRecord) jjtest.Call {
	return jjtest.Call{
		Args: []string{"config", "list", "forge"},
		Output: func(r *jjtest.FakeRepo) string {
			if len(records) == 0 {
				return ""
			}
			return "forge.reviews = " + formatConfigValue(records...)
		},
	}
}

func configSetCall(records ...forge.ReviewRecord) jjtest.Call {
	return jjtest.Call{
		Args:   []string{"config", "set", "--repo", "forge.reviews", formatConfigValue(records...)},
		Output: jjtest.EmptyOutput(),
	}
}

func importLogCall(revset string, ids ...string) jjtest.Call {
	return jjtest.Call{
		Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", revset},
		Output: jjtest.LogOutput(ids...),
	}
}

func TestImport_UpdateExisting(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:          "aaaaaaaaaaaa",
		Parents:     []string{"root"},
		Description: "feat: test\n",
		IsMutable:   true,
	})

	fakeForge := github.NewFakeForge()
	prResult, err := fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: test",
		FromBranch: "push-aaaaaaaaaaaa",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review: %v", err)
	}
	fakeForge.MergeReview(context.Background(), "github.com/owner/repo", prResult.Number)

	openRec := makeRecord("aaaaaaaaaaaa", prResult.Number, forge.ReviewStateOpen)
	mergedRec := makeRecord("aaaaaaaaaaaa", prResult.Number, forge.ReviewStateMerged)

	scenario := jjtest.NewScenario(t, repo,
		importRemoteListCall(),
		configListCall(openRec),
		importLogCall("@", "aaaaaaaaaaaa"),
		configSetCall(mergedRec),
	)

	configMgr := forge.NewConfigManager(scenario.Client())
	res, err := Import(context.Background(), scenario.Client(), fakeForge, configMgr, ImportParams{
		Revset:         "@",
		UpstreamRemote: "up",
	})
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}
	if res.Updated != 1 {
		t.Errorf("expected 1 updated, got %d", res.Updated)
	}
	if res.Added != 0 {
		t.Errorf("expected 0 added, got %d", res.Added)
	}
	scenario.Verify()
}

func TestImport_DiscoverNew(t *testing.T) {
	tests := []struct {
		name            string
		bookmarks       []string
		remoteBookmarks []string
		fromBranch      string
	}{
		{
			name:       "LocalBookmark",
			bookmarks:  []string{"my-feature"},
			fromBranch: "my-feature",
		},
		{
			name:            "RemoteBookmark",
			remoteBookmarks: []string{"og/my-feature"},
			fromBranch:      "my-feature",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changeID := "bbbbbbbbbbbb"
			repo := jjtest.NewFakeRepo()
			repo.AddCommits(jjtest.Commit{
				ID:              changeID,
				Parents:         []string{"root"},
				Description:     "feat: new feature\n",
				IsMutable:       true,
				Bookmarks:       tt.bookmarks,
				RemoteBookmarks: tt.remoteBookmarks,
			})

			fakeForge := github.NewFakeForge()
			prResult, err := fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
				Title:      "feat: new feature",
				FromBranch: tt.fromBranch,
				ToBranch:   "main",
			})
			if err != nil {
				t.Fatalf("failed to create review: %v", err)
			}

			newRec := makeRecord(changeID, prResult.Number, forge.ReviewStateOpen)
			scenario := jjtest.NewScenario(t, repo,
				importRemoteListCall(),
				configListCall(),
				importLogCall("@", changeID),
				configSetCall(newRec),
			)

			configMgr := forge.NewConfigManager(scenario.Client())
			res, err := Import(context.Background(), scenario.Client(), fakeForge, configMgr, ImportParams{
				Revset:         "@",
				UpstreamRemote: "up",
			})
			if err != nil {
				t.Fatalf("Import failed: %v", err)
			}
			if res.Added != 1 {
				t.Errorf("expected 1 added, got %d", res.Added)
			}
			scenario.Verify()
		})
	}
}

func TestImport_NoChange(t *testing.T) {
	changeID := "dddddddddddd"
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:          changeID,
		Parents:     []string{"root"},
		Description: "feat: no change\n",
		IsMutable:   true,
	})

	fakeForge := github.NewFakeForge()
	_, err := fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: no change",
		FromBranch: "some-branch",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review: %v", err)
	}

	rec := makeRecord(changeID, 1, forge.ReviewStateOpen)
	scenario := jjtest.NewScenario(t, repo,
		importRemoteListCall(),
		configListCall(rec),
		importLogCall("@", changeID),
	)

	configMgr := forge.NewConfigManager(scenario.Client())
	res, err := Import(context.Background(), scenario.Client(), fakeForge, configMgr, ImportParams{
		Revset:         "@",
		UpstreamRemote: "up",
	})
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}
	if res.Added != 0 {
		t.Errorf("expected 0 added, got %d", res.Added)
	}
	if res.Updated != 0 {
		t.Errorf("expected 0 updated, got %d", res.Updated)
	}
	scenario.Verify()
}
