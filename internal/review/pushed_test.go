package review

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/forge/github"
	"github.com/msuozzo/jj-forge/internal/jjtest"
)

const openReviewConfig = `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`

func pushedReviewScenario(t *testing.T, remoteBookmarks ...string) *jjtest.Scenario {
	uploaded := len(remoteBookmarks) > 0
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:              "aaaaaaaaaaaa",
		CommitID:        "c0ffeec0ffee",
		Parents:         []string{"root"},
		Description:     "feat: test\n",
		IsMutable:       true,
		RemoteBookmarks: remoteBookmarks,
	})
	calls := []jjtest.Call{
		{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		{
			Args:   []string{"config", "list", "forge"},
			Output: func(*jjtest.FakeRepo) string { return openReviewConfig },
		},
	}
	if uploaded {
		calls = append(calls, jjtest.Call{
			Args:   []string{"log", "--no-graph", "-r", "c0ffeec0ffee", "-T", "commit_id"},
			Output: func(*jjtest.FakeRepo) string { return "c0ffeec0ffee0000000000000000000000000000" },
		})
	}
	return jjtest.NewScenario(t, repo, calls...)
}

func TestFindPushedReview(t *testing.T) {
	scenario := pushedReviewScenario(t, "og/push-aaaaaaaaaaaa")
	got, err := FindPushedReview(context.Background(), scenario.Client(), github.NewFakeForge(), forge.NewConfigManager(scenario.Client()), "@", testRemote)
	if err != nil {
		t.Fatalf("FindPushedReview() error = %v", err)
	}
	scenario.Verify()
	want := &PushedReview{ChangeID: "aaaaaaaaaaaa", ReviewID: "1", CommitID: "c0ffeec0ffee0000000000000000000000000000"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("FindPushedReview() mismatch (-want +got):\n%s", diff)
	}
}

func TestFindPushedReview_NotUploaded(t *testing.T) {
	scenario := pushedReviewScenario(t)
	_, err := FindPushedReview(context.Background(), scenario.Client(), github.NewFakeForge(), forge.NewConfigManager(scenario.Client()), "@", testRemote)
	if !errors.Is(err, ErrNotUploaded) {
		t.Errorf("FindPushedReview() error = %v, want ErrNotUploaded", err)
	}
}
