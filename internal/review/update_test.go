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

func TestUpdate_SinglePR_NoLinks(t *testing.T) {
	// Single PR with no parent/child — no links should be added.
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:              "aaaaaaaaaaaa",
		Parents:         []string{"root"},
		Description:     "feat: standalone\n",
		IsMutable:       true,
		RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
	})

	fakeForge := github.NewFakeForge()

	revset := "::@ & mutable()"
	expandedRevset := fmt.Sprintf("(%s) | (parents(%s) & mutable())", revset, revset)

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase: Revs(revset)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", revset},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		// UpdateTrailers phase: Revs(parents(revset)~(revset))
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", fmt.Sprintf("parents(%s)~(%s)", revset, revset)},
			Output: jjtest.LogOutput("root"),
		},
		// Push: skip re-resolve (trailers unchanged, revs reused)
		// Push: skip synced (already has remote bookmark)
		// UpdatePRLinks phase: Revs(expandedRevset) for links
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", expandedRevset},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		// GetReviewRecords
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`
			},
		},
		// RemoteURL
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "up git@github.com:owner/repo.git\n"
			},
		},
		// GetReview is called for PR #1 — no forge-parent trailer, so no links
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	// Create review in forge
	_, err := fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: standalone",
		Body:       "Original body",
		FromBranch: "push-aaaaaaaaaaaa",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review: %v", err)
	}

	result, err := Update(context.Background(), scenario.Client(), fakeForge, configMgr, UpdateParams{
		Revset:         revset,
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		UI:             testUI,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if result.PRsUpdated != 0 {
		t.Errorf("expected 0 PRs updated, got %d", result.PRsUpdated)
	}

	// Verify body unchanged
	review, _ := fakeForge.GetTestReview(1)
	if review.Body != "Original body" {
		t.Errorf("expected body unchanged, got %q", review.Body)
	}

	scenario.Verify()
}

func TestUpdate_TwoStackedPRs(t *testing.T) {
	// Two stacked PRs: parent gets Children link, child gets Parents link.
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{
			ID:              "aaaaaaaaaaaa",
			Parents:         []string{"root"},
			Description:     "feat: parent\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
		},
		jjtest.Commit{
			ID:              "bbbbbbbbbbbb",
			Parents:         []string{"aaaaaaaaaaaa"},
			Description:     "feat: child\n\nforge-parent: aaaaaaaaaaaa\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-bbbbbbbbbbbb"},
		},
	)

	fakeForge := github.NewFakeForge()

	revset := "::@ & mutable()"
	parentRevset := fmt.Sprintf("parents(%s)~(%s)", revset, revset)
	expandedRevset := fmt.Sprintf("(%s) | (parents(%s) & mutable())", revset, revset)

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase: Revs(revset)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", revset},
			Output: jjtest.LogOutput("bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		// UpdateTrailers phase: Revs(parents)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", parentRevset},
			Output: jjtest.LogOutput("root"),
		},
		// Push: skip re-resolve (trailers unchanged, revs reused)
		// Push: skip sync (already synced)
		// UpdatePRLinks phase: Revs(expandedRevset)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", expandedRevset},
			Output: jjtest.LogOutput("bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		// GetReviewRecords
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen", "bbbbbbbbbbbb\npr/2\nhttps://github.com/owner/repo/pull/2\nopen"]`
			},
		},
		// RemoteURL
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "up git@github.com:owner/repo.git\n"
			},
		},
		// GetReview + UpdateReview for each PR are handled by fakeForge
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	// Create reviews in forge
	_, err := fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: parent",
		Body:       "Parent body",
		FromBranch: "push-aaaaaaaaaaaa",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review: %v", err)
	}
	_, err = fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: child",
		Body:       "Child body",
		FromBranch: "push-bbbbbbbbbbbb",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review: %v", err)
	}

	result, err := Update(context.Background(), scenario.Client(), fakeForge, configMgr, UpdateParams{
		Revset:         revset,
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		UI:             testUI,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if result.PRsUpdated != 2 {
		t.Errorf("expected 2 PRs updated, got %d", result.PRsUpdated)
	}

	// Verify parent PR got children link
	parentReview, _ := fakeForge.GetTestReview(1)
	if !strings.Contains(parentReview.Body, "Children: [#2]") {
		t.Errorf("expected parent PR to have children link, got body %q", parentReview.Body)
	}

	// Verify child PR got parent link
	childReview, _ := fakeForge.GetTestReview(2)
	if !strings.Contains(childReview.Body, "Parents: [#1]") {
		t.Errorf("expected child PR to have parent link, got body %q", childReview.Body)
	}

	scenario.Verify()
}

func TestUpdate_ThreeStackedPRs_MiddleGetsBoth(t *testing.T) {
	// Three stacked: A <- B <- C. Middle (B) gets both Parents and Children.
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{
			ID:              "aaaaaaaaaaaa",
			Parents:         []string{"root"},
			Description:     "feat: first\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
		},
		jjtest.Commit{
			ID:              "bbbbbbbbbbbb",
			Parents:         []string{"aaaaaaaaaaaa"},
			Description:     "feat: middle\n\nforge-parent: aaaaaaaaaaaa\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-bbbbbbbbbbbb"},
		},
		jjtest.Commit{
			ID:              "cccccccccccc",
			Parents:         []string{"bbbbbbbbbbbb"},
			Description:     "feat: last\n\nforge-parent: bbbbbbbbbbbb\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-cccccccccccc"},
		},
	)

	fakeForge := github.NewFakeForge()

	revset := "::@ & mutable()"
	parentRevset := fmt.Sprintf("parents(%s)~(%s)", revset, revset)
	expandedRevset := fmt.Sprintf("(%s) | (parents(%s) & mutable())", revset, revset)

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", revset},
			Output: jjtest.LogOutput("cccccccccccc", "bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", parentRevset},
			Output: jjtest.LogOutput("root"),
		},
		// Push: skip re-resolve (trailers unchanged, revs reused)
		// Push: skip sync (all synced)
		// UpdatePRLinks phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", expandedRevset},
			Output: jjtest.LogOutput("cccccccccccc", "bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen", "bbbbbbbbbbbb\npr/2\nhttps://github.com/owner/repo/pull/2\nopen", "cccccccccccc\npr/3\nhttps://github.com/owner/repo/pull/3\nopen"]`
			},
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "up git@github.com:owner/repo.git\n"
			},
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	// Create reviews
	for _, params := range []forge.ReviewCreateParams{
		{Title: "feat: first", Body: "First", FromBranch: "push-aaaaaaaaaaaa", ToBranch: "main"},
		{Title: "feat: middle", Body: "Middle", FromBranch: "push-bbbbbbbbbbbb", ToBranch: "main"},
		{Title: "feat: last", Body: "Last", FromBranch: "push-cccccccccccc", ToBranch: "main"},
	} {
		if _, err := fakeForge.CreateReview(context.Background(), "github.com/owner/repo", params); err != nil {
			t.Fatalf("failed to create review: %v", err)
		}
	}

	result, err := Update(context.Background(), scenario.Client(), fakeForge, configMgr, UpdateParams{
		Revset:         revset,
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		UI:             testUI,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if result.PRsUpdated != 3 {
		t.Errorf("expected 3 PRs updated, got %d", result.PRsUpdated)
	}

	// A: has child B
	aReview, _ := fakeForge.GetTestReview(1)
	if !strings.Contains(aReview.Body, "Children: [#2]") {
		t.Errorf("expected A to have child #2 link, got body %q", aReview.Body)
	}

	// B: has parent A and child C
	bReview, _ := fakeForge.GetTestReview(2)
	if !strings.Contains(bReview.Body, "Parents: [#1]") {
		t.Errorf("expected B to have parent #1 link, got body %q", bReview.Body)
	}
	if !strings.Contains(bReview.Body, "Children: [#3]") {
		t.Errorf("expected B to have child #3 link, got body %q", bReview.Body)
	}

	// C: has parent B
	cReview, _ := fakeForge.GetTestReview(3)
	if !strings.Contains(cReview.Body, "Parents: [#2]") {
		t.Errorf("expected C to have parent #2 link, got body %q", cReview.Body)
	}

	scenario.Verify()
}

func TestUpdate_ChangeWithoutReviewSkipped(t *testing.T) {
	// Change without a review record should be skipped.
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:              "aaaaaaaaaaaa",
		Parents:         []string{"root"},
		Description:     "feat: no review\n",
		IsMutable:       true,
		RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
	})

	fakeForge := github.NewFakeForge()

	revset := "::@ & mutable()"
	parentRevset := fmt.Sprintf("parents(%s)~(%s)", revset, revset)
	expandedRevset := fmt.Sprintf("(%s) | (parents(%s) & mutable())", revset, revset)

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", revset},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", parentRevset},
			Output: jjtest.LogOutput("root"),
		},
		// Push: skip re-resolve (trailers unchanged, revs reused)
		// UpdatePRLinks phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", expandedRevset},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return "" // No review records
			},
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "up git@github.com:owner/repo.git\n"
			},
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	result, err := Update(context.Background(), scenario.Client(), fakeForge, configMgr, UpdateParams{
		Revset:         revset,
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		UI:             testUI,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if result.PRsUpdated != 0 {
		t.Errorf("expected 0 PRs updated, got %d", result.PRsUpdated)
	}

	scenario.Verify()
}

func TestUpdate_PartialStack_ParentGetsChildLink(t *testing.T) {
	// Stack A←B, A already has open PR. Update with revset covering only B.
	// The expanded revset should include A (mutable parent), so A gets child link.
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{
			ID:              "aaaaaaaaaaaa",
			Parents:         []string{"root"},
			Description:     "feat: parent\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
		},
		jjtest.Commit{
			ID:              "bbbbbbbbbbbb",
			Parents:         []string{"aaaaaaaaaaaa"},
			Description:     "feat: child\n\nforge-parent: aaaaaaaaaaaa\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-bbbbbbbbbbbb"},
		},
	)

	fakeForge := github.NewFakeForge()

	// The user passes a revset that only includes B
	revset := "bbbbbbbbbbbb"
	parentRevset := fmt.Sprintf("parents(%s)~(%s)", revset, revset)
	expandedRevset := fmt.Sprintf("(%s) | (parents(%s) & mutable())", revset, revset)

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase: Revs(revset) — only B
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", revset},
			Output: jjtest.LogOutput("bbbbbbbbbbbb"),
		},
		// UpdateTrailers phase: Revs(parents) — A is parent of B, outside revset
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", parentRevset},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		// Push: skip re-resolve (trailers unchanged, revs reused)
		// Push: skip sync (already synced)
		// UpdatePRLinks phase: Revs(expandedRevset) — includes both A and B
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", expandedRevset},
			Output: jjtest.LogOutput("bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		// GetReviewRecords
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen", "bbbbbbbbbbbb\npr/2\nhttps://github.com/owner/repo/pull/2\nopen"]`
			},
		},
		// RemoteURL
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "up git@github.com:owner/repo.git\n"
			},
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	// Create reviews in forge
	_, err := fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: parent",
		Body:       "Parent body",
		FromBranch: "push-aaaaaaaaaaaa",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review: %v", err)
	}
	_, err = fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: child",
		Body:       "Child body",
		FromBranch: "push-bbbbbbbbbbbb",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review: %v", err)
	}

	result, err := Update(context.Background(), scenario.Client(), fakeForge, configMgr, UpdateParams{
		Revset:         revset,
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		UI:             testUI,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if result.PRsUpdated != 2 {
		t.Errorf("expected 2 PRs updated, got %d", result.PRsUpdated)
	}

	// Verify parent PR (A) got children link to B
	parentReview, _ := fakeForge.GetTestReview(1)
	if !strings.Contains(parentReview.Body, "Children: [#2]") {
		t.Errorf("expected parent PR to have children link, got body %q", parentReview.Body)
	}

	// Verify child PR (B) got parent link to A
	childReview, _ := fakeForge.GetTestReview(2)
	if !strings.Contains(childReview.Body, "Parents: [#1]") {
		t.Errorf("expected child PR to have parent link, got body %q", childReview.Body)
	}

	scenario.Verify()
}

func TestUpdate_MultipleParentsAndChildren(t *testing.T) {
	// Diamond: C has two parents (D, E) and two children (A, B).
	//
	//   A   B
	//    \ /
	//     C
	//    / \
	//   D   E
	//
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{
			ID:              "dddddddddddd",
			Parents:         []string{"root"},
			Description:     "feat: left root\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-dddddddddddd"},
		},
		jjtest.Commit{
			ID:              "eeeeeeeeeeee",
			Parents:         []string{"root"},
			Description:     "feat: right root\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-eeeeeeeeeeee"},
		},
		jjtest.Commit{
			ID:              "cccccccccccc",
			Parents:         []string{"dddddddddddd", "eeeeeeeeeeee"},
			Description:     "feat: merge\n\nforge-parent: dddddddddddd\nforge-parent: eeeeeeeeeeee\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-cccccccccccc"},
		},
		jjtest.Commit{
			ID:              "aaaaaaaaaaaa",
			Parents:         []string{"cccccccccccc"},
			Description:     "feat: left leaf\n\nforge-parent: cccccccccccc\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
		},
		jjtest.Commit{
			ID:              "bbbbbbbbbbbb",
			Parents:         []string{"cccccccccccc"},
			Description:     "feat: right leaf\n\nforge-parent: cccccccccccc\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-bbbbbbbbbbbb"},
		},
	)

	fakeForge := github.NewFakeForge()

	revset := "::@ & mutable()"
	parentRevset := fmt.Sprintf("parents(%s)~(%s)", revset, revset)
	expandedRevset := fmt.Sprintf("(%s) | (parents(%s) & mutable())", revset, revset)

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", revset},
			Output: jjtest.LogOutput("bbbbbbbbbbbb", "aaaaaaaaaaaa", "cccccccccccc", "eeeeeeeeeeee", "dddddddddddd"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", parentRevset},
			Output: jjtest.LogOutput("root"),
		},
		// Push: skip re-resolve (trailers unchanged, revs reused)
		// Push: skip sync (all synced)
		// UpdatePRLinks phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", expandedRevset},
			Output: jjtest.LogOutput("bbbbbbbbbbbb", "aaaaaaaaaaaa", "cccccccccccc", "eeeeeeeeeeee", "dddddddddddd"),
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["dddddddddddd\npr/1\nhttps://github.com/owner/repo/pull/1\nopen", "eeeeeeeeeeee\npr/2\nhttps://github.com/owner/repo/pull/2\nopen", "cccccccccccc\npr/3\nhttps://github.com/owner/repo/pull/3\nopen", "aaaaaaaaaaaa\npr/4\nhttps://github.com/owner/repo/pull/4\nopen", "bbbbbbbbbbbb\npr/5\nhttps://github.com/owner/repo/pull/5\nopen"]`
			},
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "up git@github.com:owner/repo.git\n"
			},
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	// Create reviews (numbered 1-5 in creation order: D, E, C, A, B)
	for _, params := range []forge.ReviewCreateParams{
		{Title: "feat: left root", Body: "Left root body", FromBranch: "push-dddddddddddd", ToBranch: "main"},
		{Title: "feat: right root", Body: "Right root body", FromBranch: "push-eeeeeeeeeeee", ToBranch: "main"},
		{Title: "feat: merge", Body: "Merge body", FromBranch: "push-cccccccccccc", ToBranch: "main"},
		{Title: "feat: left leaf", Body: "Left leaf body", FromBranch: "push-aaaaaaaaaaaa", ToBranch: "main"},
		{Title: "feat: right leaf", Body: "Right leaf body", FromBranch: "push-bbbbbbbbbbbb", ToBranch: "main"},
	} {
		if _, err := fakeForge.CreateReview(context.Background(), "github.com/owner/repo", params); err != nil {
			t.Fatalf("failed to create review: %v", err)
		}
	}

	result, err := Update(context.Background(), scenario.Client(), fakeForge, configMgr, UpdateParams{
		Revset:         revset,
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		UI:             testUI,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if result.PRsUpdated != 5 {
		t.Errorf("expected 5 PRs updated, got %d", result.PRsUpdated)
	}

	rURL := "https://redirect.github.com/owner/repo/pull"

	// D (#1): no parents, child C (#3)
	dReview, _ := fakeForge.GetTestReview(1)
	wantD := fmt.Sprintf("Left root body\n\n> Children: [#3](%s/3)", rURL)
	if dReview.Body != wantD {
		t.Errorf("PR #1 (D) body mismatch\n got: %q\nwant: %q", dReview.Body, wantD)
	}

	// E (#2): no parents, child C (#3)
	eReview, _ := fakeForge.GetTestReview(2)
	wantE := fmt.Sprintf("Right root body\n\n> Children: [#3](%s/3)", rURL)
	if eReview.Body != wantE {
		t.Errorf("PR #2 (E) body mismatch\n got: %q\nwant: %q", eReview.Body, wantE)
	}

	// C (#3): parents D (#1) and E (#2), children A (#4) and B (#5)
	cReview, _ := fakeForge.GetTestReview(3)
	wantC := fmt.Sprintf("Merge body\n\n> Parents: [#1](%s/1), [#2](%s/2)\n> Children: [#4](%s/4), [#5](%s/5)", rURL, rURL, rURL, rURL)
	if cReview.Body != wantC {
		t.Errorf("PR #3 (C) body mismatch\n got: %q\nwant: %q", cReview.Body, wantC)
	}

	// A (#4): parent C (#3), no children
	aReview, _ := fakeForge.GetTestReview(4)
	wantA := fmt.Sprintf("Left leaf body\n\n> Parents: [#3](%s/3)", rURL)
	if aReview.Body != wantA {
		t.Errorf("PR #4 (A) body mismatch\n got: %q\nwant: %q", aReview.Body, wantA)
	}

	// B (#5): parent C (#3), no children
	bReview, _ := fakeForge.GetTestReview(5)
	wantB := fmt.Sprintf("Right leaf body\n\n> Parents: [#3](%s/3)", rURL)
	if bReview.Body != wantB {
		t.Errorf("PR #5 (B) body mismatch\n got: %q\nwant: %q", bReview.Body, wantB)
	}

	scenario.Verify()
}

func TestUpdate_UploadError(t *testing.T) {
	// Upload errors should propagate.
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:          "aaaaaaaaaaaa",
		Parents:     []string{"root"},
		Description: "feat: test\n",
		IsMutable:   true,
	})

	fakeForge := github.NewFakeForge()

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase fails on Revs call
		jjtest.Call{
			Args: []string{"log", "--no-graph", "--template", templateMatcher, "-r", "::@ & mutable()"},
			Err:  fmt.Errorf("jj error: no such revset"),
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	_, err := Update(context.Background(), scenario.Client(), fakeForge, configMgr, UpdateParams{
		Revset:         "::@ & mutable()",
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		UI:             testUI,
	})

	if err == nil {
		t.Fatal("expected error from upload, got nil")
	}

	if !strings.Contains(err.Error(), "upload failed") {
		t.Errorf("expected 'upload failed' in error, got: %v", err)
	}

	scenario.Verify()
}

func TestUpdate_CheckFnError_AbortsPush(t *testing.T) {
	// CheckFn error should abort before pushing.
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:          "aaaaaaaaaaaa",
		Parents:     []string{"root"},
		Description: "feat: test\n",
		IsMutable:   true,
	})

	fakeForge := github.NewFakeForge()

	revset := "::@ & mutable()"

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase: Revs(revset)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", revset},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		// UpdateTrailers phase: Revs(parents)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", fmt.Sprintf("parents(%s)~(%s)", revset, revset)},
			Output: jjtest.LogOutput("root"),
		},
		// CheckFn runs here and returns error — no push or link update calls
	)

	configMgr := forge.NewConfigManager(scenario.Client())
	checkErr := fmt.Errorf("check failed: lint errors")

	_, err := Update(context.Background(), scenario.Client(), fakeForge, configMgr, UpdateParams{
		Revset:         revset,
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		UI:             testUI,
		CheckFn:        func() error { return checkErr },
	})

	if err == nil {
		t.Fatal("expected error from CheckFn, got nil")
	}
	if err != checkErr {
		t.Errorf("expected CheckFn error, got: %v", err)
	}

	scenario.Verify()
}

func TestUpdate_PreResolvedUpstreamURL(t *testing.T) {
	// When UpstreamRemoteURL is provided, the RemoteURL call in UpdatePRLinks
	// should be skipped.
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{
			ID:              "aaaaaaaaaaaa",
			Parents:         []string{"root"},
			Description:     "feat: parent\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
		},
		jjtest.Commit{
			ID:              "bbbbbbbbbbbb",
			Parents:         []string{"aaaaaaaaaaaa"},
			Description:     "feat: child\n\nforge-parent: aaaaaaaaaaaa\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-bbbbbbbbbbbb"},
		},
	)

	fakeForge := github.NewFakeForge()

	revset := "::@ & mutable()"
	parentRevset := fmt.Sprintf("parents(%s)~(%s)", revset, revset)
	expandedRevset := fmt.Sprintf("(%s) | (parents(%s) & mutable())", revset, revset)

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase: Revs(revset)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", revset},
			Output: jjtest.LogOutput("bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		// UpdateTrailers phase: Revs(parents)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", parentRevset},
			Output: jjtest.LogOutput("root"),
		},
		// Push: skip re-resolve (trailers unchanged, revs reused)
		// Push: skip sync (already synced)
		// UpdatePRLinks phase: Revs(expandedRevset)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", expandedRevset},
			Output: jjtest.LogOutput("bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		// GetReviewRecords
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen", "bbbbbbbbbbbb\npr/2\nhttps://github.com/owner/repo/pull/2\nopen"]`
			},
		},
		// No "git remote list" call: UpstreamRemoteURL is pre-resolved
		// GetReview + UpdateReview for each PR are handled by fakeForge
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	// Create reviews in forge
	_, err := fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: parent",
		Body:       "Parent body",
		FromBranch: "push-aaaaaaaaaaaa",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review: %v", err)
	}
	_, err = fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: child",
		Body:       "Child body",
		FromBranch: "push-bbbbbbbbbbbb",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review: %v", err)
	}

	result, err := Update(context.Background(), scenario.Client(), fakeForge, configMgr, UpdateParams{
		Revset:            revset,
		ForkRemote:        testRemote,
		UpstreamRemote:    "up",
		UpstreamRemoteURL: "git@github.com:owner/repo.git",
		UI:                testUI,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if result.PRsUpdated != 2 {
		t.Errorf("expected 2 PRs updated, got %d", result.PRsUpdated)
	}

	// Verify parent PR got children link
	parentReview, _ := fakeForge.GetTestReview(1)
	if !strings.Contains(parentReview.Body, "Children: [#2]") {
		t.Errorf("expected parent PR to have children link, got body %q", parentReview.Body)
	}

	// Verify child PR got parent link
	childReview, _ := fakeForge.GetTestReview(2)
	if !strings.Contains(childReview.Body, "Parents: [#1]") {
		t.Errorf("expected child PR to have parent link, got body %q", childReview.Body)
	}

	scenario.Verify()
}
