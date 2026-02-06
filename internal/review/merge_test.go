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
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`
			},
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nmerged"]`},
			Output: jjtest.EmptyOutput(),
		},
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

	result, err := Merge(context.Background(), scenario.Client(), fakeForge, configMgr, MergeParams{
		Rev:            "@",
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		NoCleanup:      false,
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
	review, exists := fakeForge.GetReview(1)
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
		// No cleanup commands
		jjtest.Call{
			Args: []string{"config", "list", "--repo", "forge"},
			Output: func(r *jjtest.FakeRepo) string {
				return `forge.reviews = ["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nopen"]`
			},
		},
		jjtest.Call{
			Args:   []string{"config", "set", "--repo", "forge.reviews", `["aaaaaaaaaaaa\npr/1\nhttps://github.com/owner/repo/pull/1\nmerged"]`},
			Output: jjtest.EmptyOutput(),
		},
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

	result, err := Merge(context.Background(), scenario.Client(), fakeForge, configMgr, MergeParams{
		Rev:            "@",
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
		NoCleanup:      true,
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

			_, err := Merge(context.Background(), scenario.Client(), fakeForge, configMgr, MergeParams{
				Rev:        "@",
				ForkRemote: testRemote,
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

func TestMerge_ForgeError(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:          "aaaaaaaaaaaa",
		Parents:     []string{"root"},
		Description: "feat: test\n",
		IsMutable:   true,
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

	_, err = Merge(context.Background(), scenario.Client(), fakeForge, configMgr, MergeParams{
		Rev:            "@",
		ForkRemote:     testRemote,
		UpstreamRemote: "up",
	})

	if err == nil {
		t.Fatal("expected error from forge, got nil")
	}

	if !strings.Contains(err.Error(), "failed to merge review") {
		t.Errorf("expected 'failed to merge review' in error, got: %v", err)
	}

	scenario.Verify()
}
