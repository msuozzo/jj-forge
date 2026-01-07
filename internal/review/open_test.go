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

const testRemote = "og"
const templateMatcher = `change_id.short()++" "++conflict++" "++divergent++" "++!immutable++" "++empty++" "++parents.map(|c| c.change_id().short()).join(",")++" "++remote_bookmarks.map(|b| b.remote() ++ "/" ++ b.name()).join(",")++" "++description.escape_json()++" "++"\n"`

func TestOpen_Success(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:              "aaaaaaaaaaaa",
		Parents:         []string{"root"},
		Description:     "feat: test feature\n\nThis is the body",
		IsMutable:       true,
		RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
	})

	fakeForge := github.NewFakeForge()

	scenario := jjtest.NewScenario(t, repo,
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "og git@github.com:owner/repo.git\n"
			},
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "og git@github.com:owner/repo.git\n"
			},
		},
		jjtest.Call{
			// AddReviewRecord calls GetReviewRecords which calls getForgeConfig
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			// Verification: test calls GetReviewRecords to verify config was updated
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`
			},
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	result, err := Open(context.Background(), scenario.Client(), fakeForge, configMgr, OpenParams{
		Rev:            "@",
		Reviewers:      []string{"reviewer1"},
		UpstreamRemote: testRemote,
		ForkRemote:     testRemote,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if result.ChangeID != "aaaaaaaaaaaa" {
		t.Errorf("expected ChangeID aaaaaaaaaaaa, got %s", result.ChangeID)
	}

	if result.Number != 1 {
		t.Errorf("expected review number 1, got %d", result.Number)
	}

	// Verify review was created in forge
	review, exists := fakeForge.GetReview(1)
	if !exists {
		t.Fatal("review not created in forge")
	}

	wantReview := &github.Review{
		Number:    1,
		Title:     "feat: test feature",
		Body:      "This is the body",
		Head:      "owner:push-aaaaaaaaaaaa",
		Base:      "main",
		Reviewers: []string{"reviewer1"},
		Status:    "open",
		URL:       "https://github.com/owner/repo/pull/1",
	}

	if diff := cmp.Diff(wantReview, review); diff != "" {
		t.Errorf("review mismatch (-want +got):\n%s", diff)
	}

	// Verify config was updated
	records, err := configMgr.GetReviewRecords()
	if err != nil {
		t.Fatalf("failed to get config records: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 config record, got %d", len(records))
	}

	if records[0].ChangeID != "aaaaaaaaaaaa" {
		t.Errorf("expected ChangeID aaaaaaaaaaaa in config, got %s", records[0].ChangeID)
	}

	if records[0].Status != "open" {
		t.Errorf("expected status 'open' in config, got %s", records[0].Status)
	}

	scenario.Verify()
}

func TestOpen_StripsTrailers(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:              "aaaaaaaaaaaa",
		Parents:         []string{"root"},
		Description:     "feat: test feature\n\nThis is the body\n\nforge-parent: pppppppppppp",
		IsMutable:       true,
		RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
	})

	fakeForge := github.NewFakeForge()

	scenario := jjtest.NewScenario(t, repo,
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "og git@github.com:owner/repo.git\n"
			},
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "og git@github.com:owner/repo.git\n"
			},
		},
		jjtest.Call{
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	result, err := Open(context.Background(), scenario.Client(), fakeForge, configMgr, OpenParams{
		Rev:            "@",
		UpstreamRemote: testRemote,
		ForkRemote:     testRemote,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Verify review was created in forge WITHOUT the internal trailer
	review, _ := fakeForge.GetReview(result.Number)
	if review.Body != "This is the body" {
		t.Errorf("expected body 'This is the body', got %q", review.Body)
	}

	scenario.Verify()
}

func TestOpen_StackedReview(t *testing.T) {
	// Test stacked review: parent is mutable and uploaded
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{
			ID:              "aaaaaaaaaaaa",
			Parents:         []string{"root"},
			Description:     "feat: parent feature\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
		},
		jjtest.Commit{
			ID:              "bbbbbbbbbbbb",
			Parents:         []string{"aaaaaaaaaaaa"},
			Description:     "feat: child feature\n",
			IsMutable:       true,
			RemoteBookmarks: []string{"og/push-bbbbbbbbbbbb"},
		},
	)

	fakeForge := github.NewFakeForge()

	scenario := jjtest.NewScenario(t, repo,
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("bbbbbbbbbbbb"),
		},
		jjtest.Call{
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "og git@github.com:owner/repo.git\n"
			},
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "og git@github.com:owner/repo.git\n"
			},
		},
		jjtest.Call{
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["bbbbbbbbbbbb\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	_, err := Open(context.Background(), scenario.Client(), fakeForge, configMgr, OpenParams{
		Rev:            "@",
		Reviewers:      []string{"reviewer1"},
		UpstreamRemote: testRemote,
		ForkRemote:     testRemote,
	})

	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	scenario.Verify()
}

func TestOpen_EmptyDescription(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:              "aaaaaaaaaaaa",
		Parents:         []string{"root"},
		Description:     "",
		IsMutable:       true,
		RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
	})

	fakeForge := github.NewFakeForge()

	scenario := jjtest.NewScenario(t, repo,
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	_, err := Open(context.Background(), scenario.Client(), fakeForge, configMgr, OpenParams{
		Rev:            "@",
		Reviewers:      []string{"reviewer1"},
		UpstreamRemote: testRemote,
		ForkRemote:     testRemote,
	})
	if err == nil {
		t.Fatal("expected error for empty description, got nil")
	}

	if !contains(err.Error(), "empty description") {
		t.Errorf("expected 'empty description' in error, got: %v", err)
	}

	scenario.Verify()
}

func TestOpen_NotUploaded(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:              "aaaaaaaaaaaa",
		Parents:         []string{"root"},
		Description:     "feat: test\n",
		IsMutable:       true,
		RemoteBookmarks: []string{}, // Not uploaded!
	})

	fakeForge := github.NewFakeForge()

	scenario := jjtest.NewScenario(t, repo,
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	_, err := Open(context.Background(), scenario.Client(), fakeForge, configMgr, OpenParams{
		Rev:            "@",
		Reviewers:      []string{"reviewer1"},
		UpstreamRemote: testRemote,
		ForkRemote:     testRemote,
	})

	if err == nil {
		t.Fatal("expected error for not uploaded, got nil")
	}

	if !contains(err.Error(), "has not been uploaded") {
		t.Errorf("expected 'has not been uploaded' in error, got: %v", err)
	}

	scenario.Verify()
}

func TestOpen_AlreadyExists(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:              "aaaaaaaaaaaa",
		Parents:         []string{"root"},
		Description:     "feat: test\n",
		IsMutable:       true,
		RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
	})

	fakeForge := github.NewFakeForge()

	scenario := jjtest.NewScenario(t, repo,
		// Pre-create review record
		jjtest.Call{
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/42\nhttps://github.com/owner/repo/pull/42\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
		// Open() call
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/42\nhttps://github.com/owner/repo/pull/42\nopen"]`
			},
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	// Pre-create a review record
	err := configMgr.AddReviewRecord(forge.ReviewRecord{
		ChangeID: "aaaaaaaaaaaa",
		ForgeID:  "pr/42",
		URL:      "https://github.com/owner/repo/pull/42",
		Status:   "open",
	})
	if err != nil {
		t.Fatalf("failed to add config record: %v", err)
	}

	_, err = Open(context.Background(), scenario.Client(), fakeForge, configMgr, OpenParams{
		Rev:            "@",
		Reviewers:      []string{"reviewer1"},
		UpstreamRemote: testRemote,
		ForkRemote:     testRemote,
	})

	if err == nil {
		t.Fatal("expected error for already exists, got nil")
	}

	if !contains(err.Error(), "review already exists") {
		t.Errorf("expected 'review already exists' in error, got: %v", err)
	}

	scenario.Verify()
}

func TestOpen_ForgeError(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:              "aaaaaaaaaaaa",
		Parents:         []string{"root"},
		Description:     "feat: test\n",
		IsMutable:       true,
		RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
	})

	fakeForge := github.NewFakeForge()
	fakeForge.SetCreateError(errors.New("forge error"))

	scenario := jjtest.NewScenario(t, repo,
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "og git@github.com:owner/repo.git\n"
			},
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "og git@github.com:owner/repo.git\n"
			},
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	_, err := Open(context.Background(), scenario.Client(), fakeForge, configMgr, OpenParams{
		Rev:            "@",
		Reviewers:      []string{"reviewer1"},
		UpstreamRemote: testRemote,
		ForkRemote:     testRemote,
	})

	if err == nil {
		t.Fatal("expected error from forge, got nil")
	}

	if !contains(err.Error(), "failed to create review") {
		t.Errorf("expected 'failed to create review' in error, got: %v", err)
	}

	scenario.Verify()
}

func TestOpen_CanReopenClosed(t *testing.T) {
	// If a review was previously closed, we can create a new one
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:              "aaaaaaaaaaaa",
		Parents:         []string{"root"},
		Description:     "feat: test\n",
		IsMutable:       true,
		RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
	})

	fakeForge := github.NewFakeForge()

	scenario := jjtest.NewScenario(t, repo,
		// Pre-create closed review record
		jjtest.Call{
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/42\nhttps://github.com/owner/repo/pull/42\nclosed"]`},
			Output: jjtest.EmptyOutput(),
		},
		// Open() call
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/42\nhttps://github.com/owner/repo/pull/42\nclosed"]`
			},
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "og git@github.com:owner/repo.git\n"
			},
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "og git@github.com:owner/repo.git\n"
			},
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/42\nhttps://github.com/owner/repo/pull/42\nclosed"]`
			},
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	// Pre-create a closed review record
	err := configMgr.AddReviewRecord(forge.ReviewRecord{
		ChangeID: "aaaaaaaaaaaa",
		ForgeID:  "pr/42",
		URL:      "https://github.com/owner/repo/pull/42",
		Status:   "closed",
	})
	if err != nil {
		t.Fatalf("failed to add config record: %v", err)
	}

	result, err := Open(context.Background(), scenario.Client(), fakeForge, configMgr, OpenParams{
		Rev:            "@",
		Reviewers:      []string{"reviewer1"},
		UpstreamRemote: testRemote,
		ForkRemote:     testRemote,
	})

	if err != nil {
		t.Fatalf("Open() error = %v, should allow reopening closed review", err)
	}

	// Should create a new review
	if result.Number != 1 {
		t.Errorf("expected new review number 1, got %d", result.Number)
	}

	scenario.Verify()
}

func TestOpen_CrossRepo(t *testing.T) {
	// Test cross-repo PR: branch is on "og" (fork), PR is against "up" (upstream)
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:              "aaaaaaaaaaaa",
		Parents:         []string{"root"},
		Description:     "feat: cross-repo feature\n",
		IsMutable:       true,
		RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
	})

	// Upstream forge
	fakeForge := github.NewFakeForge()

	scenario := jjtest.NewScenario(t, repo,
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			// Upstream remote info
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "og git@github.com:fork-owner/repo.git\nup git@github.com:upstream-owner/repo.git\n"
			},
		},
		jjtest.Call{
			// Fork remote info
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "og git@github.com:fork-owner/repo.git\nup git@github.com:upstream-owner/repo.git\n"
			},
		},
		jjtest.Call{
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/upstream-owner/repo/pull/1\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	result, err := Open(context.Background(), scenario.Client(), fakeForge, configMgr, OpenParams{
		Rev:            "@",
		UpstreamRemote: "up",
		ForkRemote:     "og",
	})

	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Verify review was created with fork-owner:push-aaaaaaaaaaaa as head
	review, exists := fakeForge.GetReview(result.Number)
	if !exists {
		t.Fatal("review not created in forge")
	}

	if review.Head != "fork-owner:push-aaaaaaaaaaaa" {
		t.Errorf("expected Head fork-owner:push-aaaaaaaaaaaa, got %s", review.Head)
	}

	if review.Base != "main" {
		t.Errorf("expected Base main, got %s", review.Base)
	}

	scenario.Verify()
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
