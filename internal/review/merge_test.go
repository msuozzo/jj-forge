package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/forge/github"
	"github.com/msuozzo/jj-forge/internal/jjtest"
)

func TestMerge_Success(t *testing.T) {
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
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
		// Merge() call
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`
			},
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "up git@github.com:owner/repo.git\n"
			},
		},
		// forge: merge + strip links from merged PR
		jjtest.Call{Args: []string{"forge:MergeReview", "1"}},
		jjtest.Call{Args: []string{"forge:GetReview", "1"}},
		// No forge:UpdateReview — review has no body/links to strip
		jjtest.Call{
			Args:   []string{"git", "fetch", "--remote", testRemote, "--branch", "push-aaaaaaaaaaaa"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"bookmark", "delete", "push-aaaaaaaaaaaa"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--remote", testRemote, "--bookmark", "push-aaaaaaaaaaaa"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"git", "fetch", "--remote", "up"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"abandon", "present(aaaaaaaaaaaa)"},
			Output: jjtest.EmptyOutput(),
		},
		// AddReviewRecord: getForgeConfig cached from GetReviewByChangeID
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nmerged"]`},
			Output: jjtest.EmptyOutput(),
		},
		// RemoveCheckVerdicts: cache invalidated by SaveRecords, re-reads
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nmerged"]`
			},
		},
		// cleanupLinksAfterMerge: no other open reviews
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	// Create the review in the forge (needed for FakeForge.MergeReview)
	_, err := fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: test",
		FromBranch: "push-aaaaaaaaaaaa",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review in forge: %v", err)
	}

	err = configMgr.AddReviewRecord(forge.ReviewRecord{
		ChangeID: "aaaaaaaaaaaa",
		ForgeID:  "pr/1",
		URL:      "https://github.com/owner/repo/pull/1",
		Status:   forge.ReviewStateOpen,
	})
	if err != nil {
		t.Fatalf("failed to add config record: %v", err)
	}

	wrappedForge := scenario.WrapForge(fakeForge)
	result, err := Merge(context.Background(), scenario.Client(), wrappedForge, configMgr, MergeParams{
		Rev:            "@",
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		NoCleanup:      false,
		UI:             testUI,
	})

	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	if result.ChangeID != "aaaaaaaaaaaa" {
		t.Errorf("expected ChangeID aaaaaaaaaaaa, got %s", result.ChangeID)
	}

	if result.Number != 1 {
		t.Errorf("expected review number 1, got %d", result.Number)
	}

	// Verify review was merged in forge
	review, exists := fakeForge.GetTestReview(result.Number)
	if !exists {
		t.Fatal("review not found in forge")
	}

	wantReview := &github.Review{
		Number: 1,
		Title:  "feat: test",
		Head:   "push-aaaaaaaaaaaa",
		Base:   "main",
		Status: "merged",
		URL:    "https://github.com/owner/repo/pull/1",
	}

	if diff := cmp.Diff(wantReview, review); diff != "" {
		t.Errorf("review mismatch (-want +got):\n%s", diff)
	}

	scenario.Verify()
}

func TestMerge_NoCleanup(t *testing.T) {
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
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
		// Merge() call
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`
			},
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "up git@github.com:owner/repo.git\n"
			},
		},
		// forge: merge + strip links from merged PR
		jjtest.Call{Args: []string{"forge:MergeReview", "1"}},
		jjtest.Call{Args: []string{"forge:GetReview", "1"}},
		// No cleanup commands (NoCleanup=true)
		// AddReviewRecord: getForgeConfig cached from GetReviewByChangeID
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nmerged"]`},
			Output: jjtest.EmptyOutput(),
		},
		// RemoveCheckVerdicts: cache invalidated by SaveRecords, re-reads
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nmerged"]`
			},
		},
		// cleanupLinksAfterMerge: no other open reviews
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	// Create the review in the forge (needed for FakeForge.MergeReview)
	_, err := fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: test",
		FromBranch: "push-aaaaaaaaaaaa",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review in forge: %v", err)
	}

	err = configMgr.AddReviewRecord(forge.ReviewRecord{
		ChangeID: "aaaaaaaaaaaa",
		ForgeID:  "pr/1",
		URL:      "https://github.com/owner/repo/pull/1",
		Status:   forge.ReviewStateOpen,
	})
	if err != nil {
		t.Fatalf("failed to add config record: %v", err)
	}

	wrappedForge := scenario.WrapForge(fakeForge)
	result, err := Merge(context.Background(), scenario.Client(), wrappedForge, configMgr, MergeParams{
		Rev:            "@",
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		NoCleanup:      true,
		UI:             testUI,
	})

	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	if result.Number != 1 {
		t.Errorf("expected review number 1, got %d", result.Number)
	}

	scenario.Verify()
}

func TestMerge_StatusErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  forge.ReviewState // "" means no pre-existing record
		wantErr string
	}{
		{"no review found", "", "no review found"},
		{"already merged", forge.ReviewStateMerged, "already merged"},
		{"review closed", forge.ReviewStateClosed, "is closed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := jjtest.NewFakeRepo()
			repo.AddCommits(jjtest.Commit{
				ID:          "aaaaaaaaaaaa",
				Parents:     []string{"root"},
				Description: "feat: test\n",
				IsMutable:   true,
			})

			fakeForge := github.NewFakeForge()

			var calls []jjtest.Call
			if tt.status != "" {
				calls = append(calls,
					jjtest.Call{
						Args:   []string{"config", "list", "--repo", "forge"},
						Output: jjtest.EmptyOutput(),
					},
					jjtest.Call{
						Args:   []string{"config", "set", "--repo", "forge.reviews", fmt.Sprintf(`["aaaaaaaaaaaa\npr/42\nhttps://github.com/owner/repo/pull/42\n%s"]`, tt.status)},
						Output: jjtest.EmptyOutput(),
					},
				)
			}
			calls = append(calls,
				jjtest.Call{
					Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
					Output: jjtest.LogOutput("aaaaaaaaaaaa"),
				},
				jjtest.Call{
					Args: []string{"config", "list", "--repo", "forge"},
					Output: func(r *jjtest.FakeRepo) string {
						if tt.status == "" {
							return ""
						}
						return fmt.Sprintf(`forge.reviews = ["aaaaaaaaaaaa\npr/42\nhttps://github.com/owner/repo/pull/42\n%s"]`, tt.status)
					},
				},
			)

			scenario := jjtest.NewScenario(t, repo, calls...)
			configMgr := forge.NewConfigManager(scenario.Client())

			if tt.status != "" {
				err := configMgr.AddReviewRecord(forge.ReviewRecord{
					ChangeID: "aaaaaaaaaaaa",
					ForgeID:  "pr/42",
					URL:      "https://github.com/owner/repo/pull/42",
					Status:   tt.status,
				})
				if err != nil {
					t.Fatalf("failed to add config record: %v", err)
				}
			}

			wrappedForge := scenario.WrapForge(fakeForge)
			_, err := Merge(context.Background(), scenario.Client(), wrappedForge, configMgr, MergeParams{
				Rev:        "@",
				ForkRemote: testRemote,
				UI:         testUI,
			})

			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected %q in error, got: %v", tt.wantErr, err)
			}

			scenario.Verify()
		})
	}
}

func TestMerge_NotUploaded(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:          "aaaaaaaaaaaa",
		Parents:     []string{"root"},
		Description: "feat: test\n",
		IsMutable:   true,
		// No RemoteBookmarks: change has not been pushed
	})

	fakeForge := github.NewFakeForge()

	scenario := jjtest.NewScenario(t, repo,
		// Pre-create review record
		jjtest.Call{
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
		// Merge() call
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`
			},
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	err := configMgr.AddReviewRecord(forge.ReviewRecord{
		ChangeID: "aaaaaaaaaaaa",
		ForgeID:  "pr/1",
		URL:      "https://github.com/owner/repo/pull/1",
		Status:   forge.ReviewStateOpen,
	})
	if err != nil {
		t.Fatalf("failed to add config record: %v", err)
	}

	wrappedForge := scenario.WrapForge(fakeForge)
	_, err = Merge(context.Background(), scenario.Client(), wrappedForge, configMgr, MergeParams{
		Rev:            "@",
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		UI:             testUI,
	})

	if err == nil {
		t.Fatal("expected ErrNotUploaded, got nil")
	}
	if !errors.Is(err, ErrNotUploaded) {
		t.Errorf("expected ErrNotUploaded, got: %v", err)
	}

	scenario.Verify()
}

func TestMerge_HasParentTrailer(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:              "aaaaaaaaaaaa",
		Parents:         []string{"root"},
		Description:     "feat: test\n\nforge-parent: pppppppppppp\n",
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
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
		// Merge() call
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`
			},
		},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	err := configMgr.AddReviewRecord(forge.ReviewRecord{
		ChangeID: "aaaaaaaaaaaa",
		ForgeID:  "pr/1",
		URL:      "https://github.com/owner/repo/pull/1",
		Status:   forge.ReviewStateOpen,
	})
	if err != nil {
		t.Fatalf("failed to add config record: %v", err)
	}

	wrappedForge := scenario.WrapForge(fakeForge)
	_, err = Merge(context.Background(), scenario.Client(), wrappedForge, configMgr, MergeParams{
		Rev:            "@",
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		UI:             testUI,
	})

	if err == nil {
		t.Fatal("expected ErrHasParentTrailer, got nil")
	}
	if !errors.Is(err, ErrHasParentTrailer) {
		t.Errorf("expected ErrHasParentTrailer, got: %v", err)
	}
	if !strings.Contains(err.Error(), "pppppppppppp") {
		t.Errorf("expected parent ID in error message, got: %v", err)
	}

	scenario.Verify()
}

func TestMerge_ForgeError(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:              "aaaaaaaaaaaa",
		Parents:         []string{"root"},
		Description:     "feat: test\n",
		IsMutable:       true,
		RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
	})

	fakeForge := github.NewFakeForge()
	fakeForge.SetMergeError(errors.New("forge error"))

	scenario := jjtest.NewScenario(t, repo,
		// Pre-create review record
		jjtest.Call{
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
		// Merge() call
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`
			},
		},
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "up git@github.com:owner/repo.git\n"
			},
		},
		// forge: MergeReview returns error
		jjtest.Call{Args: []string{"forge:MergeReview", "1"}},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	err := configMgr.AddReviewRecord(forge.ReviewRecord{
		ChangeID: "aaaaaaaaaaaa",
		ForgeID:  "pr/1",
		URL:      "https://github.com/owner/repo/pull/1",
		Status:   forge.ReviewStateOpen,
	})
	if err != nil {
		t.Fatalf("failed to add config record: %v", err)
	}

	wrappedForge := scenario.WrapForge(fakeForge)
	_, err = Merge(context.Background(), scenario.Client(), wrappedForge, configMgr, MergeParams{
		Rev:            "@",
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		UI:             testUI,
	})

	if err == nil {
		t.Fatal("expected error from forge, got nil")
	}

	if !strings.Contains(err.Error(), "failed to merge review") {
		t.Errorf("expected 'failed to merge review' in error, got: %v", err)
	}

	scenario.Verify()
}

func TestMerge_LinkCleanup(t *testing.T) {
	// Merge bottom of a 3-PR stack: A <- B <- C
	// After merging A, B and C should have their links cleaned up.
	// B previously had parent=A; after merge, B has no parent link.
	// C still has parent=B; after merge, C keeps parent=B link.
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

	reviewsConfig := `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen", "bbbbbbbbbbbb\npr/2\nhttps://github.com/owner/repo/pull/2\nopen", "cccccccccccc\npr/3\nhttps://github.com/owner/repo/pull/3\nopen"]`
	mergedConfig := `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nmerged", "bbbbbbbbbbbb\npr/2\nhttps://github.com/owner/repo/pull/2\nopen", "cccccccccccc\npr/3\nhttps://github.com/owner/repo/pull/3\nopen"]`

	scenario := jjtest.NewScenario(t, repo,
		// Pre-create review records (3 calls: list, set, list, set, list, set)
		jjtest.Call{
			Args:   []string{"config", "list", "--repo", "forge"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`
			},
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen", "bbbbbbbbbbbb\npr/2\nhttps://github.com/owner/repo/pull/2\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen", "bbbbbbbbbbbb\npr/2\nhttps://github.com/owner/repo/pull/2\nopen"]`
			},
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen", "bbbbbbbbbbbb\npr/2\nhttps://github.com/owner/repo/pull/2\nopen", "cccccccccccc\npr/3\nhttps://github.com/owner/repo/pull/3\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
		// Merge() call: Rev("aaaaaaaaaaaa")
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "aaaaaaaaaaaa"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		// GetReviewByChangeID → GetReviewRecords
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return reviewsConfig
			},
		},
		// RemoteURL
		jjtest.Call{
			Args: []string{"git", "remote", "list"},
			Output: func(r *jjtest.FakeRepo) string {
				return "up git@github.com:owner/repo.git\n"
			},
		},
		// forge: merge + strip links from merged PR A
		jjtest.Call{Args: []string{"forge:MergeReview", "1"}},
		jjtest.Call{Args: []string{"forge:GetReview", "1"}},
		jjtest.Call{Args: []string{"forge:UpdateReview", "1"}},
		// Cleanup: fetch fork + bookmark delete + push + fetch upstream
		jjtest.Call{
			Args:   []string{"git", "fetch", "--remote", testRemote, "--branch", "push-aaaaaaaaaaaa"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"bookmark", "delete", "push-aaaaaaaaaaaa"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--remote", testRemote, "--bookmark", "push-aaaaaaaaaaaa"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"git", "fetch", "--remote", "up"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"abandon", "present(aaaaaaaaaaaa)"},
			Output: jjtest.EmptyOutput(),
		},
		// AddReviewRecord (mark merged): getForgeConfig cached from GetReviewByChangeID
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nmerged", "bbbbbbbbbbbb\npr/2\nhttps://github.com/owner/repo/pull/2\nopen", "cccccccccccc\npr/3\nhttps://github.com/owner/repo/pull/3\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
		// RemoveCheckVerdicts: cache invalidated by SaveRecords, re-reads
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return mergedConfig
			},
		},
		// cleanupLinksAfterMerge: getForgeConfig cached from RemoveCheckVerdicts (no write)
		// cleanupLinksAfterMerge: bulk Revs for open reviews
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "bbbbbbbbbbbb|cccccccccccc"},
			Output: jjtest.LogOutput("bbbbbbbbbbbb", "cccccccccccc"),
		},
		// cleanupLinksAfterMerge: GetReview + UpdateReview for B only
		jjtest.Call{Args: []string{"forge:GetReview", "2"}},
		jjtest.Call{Args: []string{"forge:UpdateReview", "2"}},
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	// Create reviews in forge
	_, err := fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: first",
		Body:       "First body\n\n> Children: [#2](https://github.com/owner/repo/pull/2)",
		FromBranch: "push-aaaaaaaaaaaa",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review: %v", err)
	}
	_, err = fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: middle",
		Body:       "Middle body\n\n> Parents: [#1](https://github.com/owner/repo/pull/1)\n> Children: [#3](https://github.com/owner/repo/pull/3)",
		FromBranch: "push-bbbbbbbbbbbb",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review: %v", err)
	}
	_, err = fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: last",
		Body:       "Last body\n\n> Parents: [#2](https://github.com/owner/repo/pull/2)",
		FromBranch: "push-cccccccccccc",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review: %v", err)
	}

	// Add review records
	for _, rec := range []forge.ReviewRecord{
		{ChangeID: "aaaaaaaaaaaa", ForgeID: "pr/1", URL: "https://github.com/owner/repo/pull/1", Status: forge.ReviewStateOpen},
		{ChangeID: "bbbbbbbbbbbb", ForgeID: "pr/2", URL: "https://github.com/owner/repo/pull/2", Status: forge.ReviewStateOpen},
		{ChangeID: "cccccccccccc", ForgeID: "pr/3", URL: "https://github.com/owner/repo/pull/3", Status: forge.ReviewStateOpen},
	} {
		if err := configMgr.AddReviewRecord(rec); err != nil {
			t.Fatalf("failed to add config record: %v", err)
		}
	}

	wrappedForge := scenario.WrapForge(fakeForge)
	result, err := Merge(context.Background(), scenario.Client(), wrappedForge, configMgr, MergeParams{
		Rev:            "aaaaaaaaaaaa",
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		NoCleanup:      false,
		UI:             testUI,
	})

	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	if result.Number != 1 {
		t.Errorf("expected review number 1, got %d", result.Number)
	}

	// Verify A's links were stripped after merge
	aReview, _ := fakeForge.GetTestReview(1)
	if strings.Contains(aReview.Body, "> Children:") {
		t.Errorf("expected A's links stripped after merge, got body %q", aReview.Body)
	}
	if aReview.Body != "First body" {
		t.Errorf("expected A's body to be %q, got %q", "First body", aReview.Body)
	}

	// Verify B's links: parent (A) was merged, so parent link removed. Child (C) still exists.
	bReview, _ := fakeForge.GetTestReview(2)
	if strings.Contains(bReview.Body, "> Parents:") {
		t.Errorf("expected B's parent link removed after merge, got body %q", bReview.Body)
	}
	if !strings.Contains(bReview.Body, "> Children: [#3]") {
		t.Errorf("expected B to still have child #3 link, got body %q", bReview.Body)
	}

	// Verify C's links: parent is B (still open), so parent link preserved.
	cReview, _ := fakeForge.GetTestReview(3)
	if !strings.Contains(cReview.Body, "> Parents: [#2]") {
		t.Errorf("expected C to still have parent #2 link, got body %q", cReview.Body)
	}

	scenario.Verify()
}

func TestMerge_PreResolvedUpstreamURL(t *testing.T) {
	// When UpstreamRemoteURL is provided, the RemoteURL call should be skipped.
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
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`},
			Output: jjtest.EmptyOutput(),
		},
		// Merge() call
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "@"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`
			},
		},
		// No "git remote list" call: UpstreamRemoteURL is pre-resolved
		// forge: merge + strip links from merged PR
		jjtest.Call{Args: []string{"forge:MergeReview", "1"}},
		jjtest.Call{Args: []string{"forge:GetReview", "1"}},
		// No cleanup commands (NoCleanup=true)
		// AddReviewRecord: getForgeConfig cached from GetReviewByChangeID
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nmerged"]`},
			Output: jjtest.EmptyOutput(),
		},
		// RemoveCheckVerdicts: cache invalidated by SaveRecords, re-reads
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nmerged"]`
			},
		},
		// cleanupLinksAfterMerge: no other open reviews
	)

	configMgr := forge.NewConfigManager(scenario.Client())

	// Create the review in the forge (needed for FakeForge.MergeReview)
	_, err := fakeForge.CreateReview(context.Background(), "github.com/owner/repo", forge.ReviewCreateParams{
		Title:      "feat: test",
		FromBranch: "push-aaaaaaaaaaaa",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("failed to create review in forge: %v", err)
	}

	err = configMgr.AddReviewRecord(forge.ReviewRecord{
		ChangeID: "aaaaaaaaaaaa",
		ForgeID:  "pr/1",
		URL:      "https://github.com/owner/repo/pull/1",
		Status:   forge.ReviewStateOpen,
	})
	if err != nil {
		t.Fatalf("failed to add config record: %v", err)
	}

	wrappedForge := scenario.WrapForge(fakeForge)
	result, err := Merge(context.Background(), scenario.Client(), wrappedForge, configMgr, MergeParams{
		Rev:               "@",
		ForkRemote:        testRemote,
		UpstreamRemote:    "up",
		UpstreamRemoteURL: "git@github.com:owner/repo.git",
		NoCleanup:         true,
		UI:                testUI,
	})

	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	wantReview := &github.Review{
		Number: 1,
		Title:  "feat: test",
		Head:   "push-aaaaaaaaaaaa",
		Base:   "main",
		Status: "merged",
		URL:    "https://github.com/owner/repo/pull/1",
	}

	review, exists := fakeForge.GetTestReview(result.Number)
	if !exists {
		t.Fatal("review not found in forge")
	}
	if diff := cmp.Diff(wantReview, review); diff != "" {
		t.Errorf("review mismatch (-want +got):\n%s", diff)
	}

	scenario.Verify()
}
