package change

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/msuozzo/jj-forge/internal/jjtest"
	"github.com/msuozzo/jj-forge/internal/ui"
)

const testRemote = "og"

var testUI = ui.New(io.Discard, ui.ColorNever)

func TestUpload_SingleMutableCommit(t *testing.T) {
	// Single mutable commit on top of immutable root.
	// No trailer should be added (parent is immutable).
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(jjtest.Commit{
		ID:          "aaaaaaaaaaaa",
		Parents:     []string{"root"},
		IsMutable:   true,
		Description: "feat: add feature\n",
	})

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase: Revs(revset)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(mutable())~(mutable())"},
			Output: jjtest.LogOutput("root"),
		},
		// Push phase: no re-resolve (trailers unchanged, revs reused)
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "aaaaaaaaaaaa", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
	)

	client := scenario.Client()
	result, err := Upload(context.Background(), client, "mutable()", testRemote, testUI)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.Pushed != 1 {
		t.Errorf("expected 1 push, got %d", result.Pushed)
	}
	scenario.Verify()
}

func TestUpload_TwoCommitStack(t *testing.T) {
	// Stack: root <- A <- B (both mutable)
	// A: no trailer, B: forge-parent: A
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{ID: "aaaaaaaaaaaa", Parents: []string{"root"}, IsMutable: true, Description: "feat: A\n"},
		jjtest.Commit{ID: "bbbbbbbbbbbb", Parents: []string{"aaaaaaaaaaaa"}, IsMutable: true, Description: "feat: B\n"},
	)

	// jj returns children first (B, A), we reverse to (A, B)
	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(mutable())~(mutable())"},
			Output: jjtest.LogOutput("root"),
		},
		// A: no trailer change (parent is immutable)
		// B: needs forge-parent trailer
		jjtest.Call{
			Args:       []string{"metaedit", "bbbbbbbbbbbb", "-m", "feat: B\n\nforge-parent: aaaaaaaaaaaa\n"},
			Output:     jjtest.EmptyOutput(),
			SideEffect: jjtest.UpdateDescription("bbbbbbbbbbbb", "feat: B\n\nforge-parent: aaaaaaaaaaaa\n"),
		},
		// Push phase: re-resolve Revs(revset)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "aaaaaaaaaaaa", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "bbbbbbbbbbbb", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
	)

	client := scenario.Client()
	result, err := Upload(context.Background(), client, "mutable()", testRemote, testUI)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.Pushed != 2 {
		t.Errorf("expected 2 pushes, got %d", result.Pushed)
	}
	if result.TrailersUpdated != 1 {
		t.Errorf("expected 1 trailer update, got %d", result.TrailersUpdated)
	}
	scenario.Verify()
}

func TestUpload_ThreeCommitStack(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{ID: "aaaaaaaaaaaa", Parents: []string{"root"}, IsMutable: true, Description: "A\n"},
		jjtest.Commit{ID: "bbbbbbbbbbbb", Parents: []string{"aaaaaaaaaaaa"}, IsMutable: true, Description: "B\n"},
		jjtest.Commit{ID: "cccccccccccc", Parents: []string{"bbbbbbbbbbbb"}, IsMutable: true, Description: "C\n"},
	)

	// jj returns children first (C, B, A), we reverse to (A, B, C)
	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("cccccccccccc", "bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(mutable())~(mutable())"},
			Output: jjtest.LogOutput("root"),
		},
		// A: no trailer change (parent is immutable)
		// B: needs forge-parent trailer
		jjtest.Call{
			Args:       []string{"metaedit", "bbbbbbbbbbbb", "-m", "B\n\nforge-parent: aaaaaaaaaaaa\n"},
			Output:     jjtest.EmptyOutput(),
			SideEffect: jjtest.UpdateDescription("bbbbbbbbbbbb", "B\n\nforge-parent: aaaaaaaaaaaa\n"),
		},
		// C: needs forge-parent trailer
		jjtest.Call{
			Args:       []string{"metaedit", "cccccccccccc", "-m", "C\n\nforge-parent: bbbbbbbbbbbb\n"},
			Output:     jjtest.EmptyOutput(),
			SideEffect: jjtest.UpdateDescription("cccccccccccc", "C\n\nforge-parent: bbbbbbbbbbbb\n"),
		},
		// Push phase: re-resolve Revs(revset)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("cccccccccccc", "bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "aaaaaaaaaaaa", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "bbbbbbbbbbbb", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "cccccccccccc", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
	)

	client := scenario.Client()
	result, err := Upload(context.Background(), client, "mutable()", testRemote, testUI)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.Pushed != 3 {
		t.Errorf("expected 3 pushes, got %d", result.Pushed)
	}
	scenario.Verify()
}

func TestUpload_MultipleParents(t *testing.T) {
	// C has two mutable parents A and B — should get two forge-parent trailers.
	//   A   B
	//    \ /
	//     C
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{ID: "aaaaaaaaaaaa", Parents: []string{"root"}, IsMutable: true, Description: "A\n"},
		jjtest.Commit{ID: "bbbbbbbbbbbb", Parents: []string{"root"}, IsMutable: true, Description: "B\n"},
		jjtest.Commit{ID: "cccccccccccc", Parents: []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"}, IsMutable: true, Description: "C\n"},
	)

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("cccccccccccc", "bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(mutable())~(mutable())"},
			Output: jjtest.LogOutput("root"),
		},
		// A: no trailer (parent is immutable root)
		// B: no trailer (parent is immutable root)
		// C: gets two forge-parent trailers
		jjtest.Call{
			Args:       []string{"metaedit", "cccccccccccc", "-m", "C\n\nforge-parent: aaaaaaaaaaaa\nforge-parent: bbbbbbbbbbbb\n"},
			Output:     jjtest.EmptyOutput(),
			SideEffect: jjtest.UpdateDescription("cccccccccccc", "C\n\nforge-parent: aaaaaaaaaaaa\nforge-parent: bbbbbbbbbbbb\n"),
		},
		// Push phase: re-resolve Revs(revset)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("cccccccccccc", "bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "aaaaaaaaaaaa", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "bbbbbbbbbbbb", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "cccccccccccc", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
	)

	client := scenario.Client()
	result, err := Upload(context.Background(), client, "mutable()", testRemote, testUI)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.Pushed != 3 {
		t.Errorf("expected 3 pushes, got %d", result.Pushed)
	}
	if result.TrailersUpdated != 1 {
		t.Errorf("expected 1 trailer update, got %d", result.TrailersUpdated)
	}
	scenario.Verify()
}

func TestUpload_TrailerAlreadyCorrect(t *testing.T) {
	// B already has correct trailer - no describe call, but still pushes (not synced)
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{ID: "aaaaaaaaaaaa", Parents: []string{"root"}, IsMutable: true, Description: "A\n"},
		jjtest.Commit{ID: "bbbbbbbbbbbb", Parents: []string{"aaaaaaaaaaaa"}, IsMutable: true, Description: "B\n\nforge-parent: aaaaaaaaaaaa\n"},
	)

	// jj returns children first (B, A), we reverse to (A, B)
	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(mutable())~(mutable())"},
			Output: jjtest.LogOutput("root"),
		},
		// No describe call - trailer already correct
		// Push phase: no re-resolve (trailers unchanged, revs reused)
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "aaaaaaaaaaaa", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "bbbbbbbbbbbb", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
	)

	client := scenario.Client()
	result, err := Upload(context.Background(), client, "mutable()", testRemote, testUI)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.TrailersUpdated != 0 {
		t.Errorf("expected 0 trailer updates, got %d", result.TrailersUpdated)
	}
	scenario.Verify()
}

func TestUpload_TrailerRemoval(t *testing.T) {
	// A has a stale forge-parent trailer that should be removed
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{
			ID:          "aaaaaaaaaaaa",
			Parents:     []string{"root"},
			IsMutable:   true,
			Description: "A\n\nforge-parent: oldparent\n",
		},
	)

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(mutable())~(mutable())"},
			Output: jjtest.LogOutput("root"),
		},
		jjtest.Call{
			Args:       []string{"metaedit", "aaaaaaaaaaaa", "-m", "A\n"},
			Output:     jjtest.EmptyOutput(),
			SideEffect: jjtest.UpdateDescription("aaaaaaaaaaaa", "A\n"),
		},
		// Push phase: re-resolve Revs(revset)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "aaaaaaaaaaaa", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
	)

	client := scenario.Client()
	result, err := Upload(context.Background(), client, "mutable()", testRemote, testUI)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.TrailersUpdated != 1 {
		t.Errorf("expected 1 trailer update, got %d", result.TrailersUpdated)
	}
	scenario.Verify()
}

func TestUpload_PushFailure(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{ID: "aaaaaaaaaaaa", Parents: []string{"root"}, IsMutable: true, Description: "A\n"},
	)

	pushErr := errors.New("push failed: remote rejected")
	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(mutable())~(mutable())"},
			Output: jjtest.LogOutput("root"),
		},
		// Push phase: no re-resolve (trailers unchanged, revs reused)
		jjtest.Call{
			Args: []string{"git", "push", "--change", "aaaaaaaaaaaa", "--remote", testRemote, "--allow-new"},
			Err:  pushErr,
		},
	)

	client := scenario.Client()
	_, err := Upload(context.Background(), client, "mutable()", testRemote, testUI)
	if err == nil {
		t.Fatal("Upload() expected error, got nil")
	}
	if !errors.Is(err, pushErr) {
		t.Fatalf("Upload() error = %v, want %v", err, pushErr)
	}
	scenario.Verify()
}

func TestUpload_EmptyRevset(t *testing.T) {
	repo := jjtest.NewFakeRepo()

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase: empty result
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "none()"},
			Output: jjtest.EmptyOutput(),
		},
		// Push phase: no re-resolve (trailers unchanged, revs reused — empty)
	)

	client := scenario.Client()
	result, err := Upload(context.Background(), client, "none()", testRemote, testUI)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.Pushed != 0 || result.Skipped != 0 {
		t.Errorf("expected no activity, got pushed=%d skipped=%d", result.Pushed, result.Skipped)
	}
	scenario.Verify()
}

func TestUpload_SkipEmptyCommit(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{ID: "aaaaaaaaaaaa", Parents: []string{"root"}, IsMutable: true, Description: "A\n", IsEmpty: true},
	)

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(mutable())~(mutable())"},
			Output: jjtest.LogOutput("root"),
		},
		// No describe - skipped (empty)
		// Push phase: no re-resolve (trailers unchanged, revs reused)
		// No push - silently skipped (empty)
	)

	client := scenario.Client()
	result, err := Upload(context.Background(), client, "mutable()", testRemote, testUI)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.SkippedEmpty != 1 {
		t.Errorf("expected 1 skipped empty, got %d", result.SkippedEmpty)
	}
	if result.Pushed != 0 {
		t.Errorf("expected 0 pushes, got %d", result.Pushed)
	}
	scenario.Verify()
}

func TestUpload_SkipAnonymousCommit(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{ID: "aaaaaaaaaaaa", Parents: []string{"root"}, IsMutable: true, Description: ""},
	)

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(mutable())~(mutable())"},
			Output: jjtest.LogOutput("root"),
		},
		// Push phase: no re-resolve (trailers unchanged, revs reused)
		// No push - silently skipped (anonymous)
	)

	client := scenario.Client()
	result, err := Upload(context.Background(), client, "mutable()", testRemote, testUI)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.SkippedAnonymous != 1 {
		t.Errorf("expected 1 skipped anonymous, got %d", result.SkippedAnonymous)
	}
	scenario.Verify()
}

func TestUpload_SkipSyncedCommit(t *testing.T) {
	// Commit already synced (has remote bookmark pointing to it)
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{
			ID:              "aaaaaaaaaaaa",
			Parents:         []string{"root"},
			IsMutable:       true,
			Description:     "A\n",
			RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
		},
	)

	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(mutable())~(mutable())"},
			Output: jjtest.LogOutput("root"),
		},
		// Push phase: no re-resolve (trailers unchanged, revs reused)
		// No push - already synced
	)

	client := scenario.Client()
	result, err := Upload(context.Background(), client, "mutable()", testRemote, testUI)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.SkippedSynced != 1 {
		t.Errorf("expected 1 skipped synced, got %d", result.SkippedSynced)
	}
	if result.Pushed != 0 {
		t.Errorf("expected 0 pushes, got %d", result.Pushed)
	}
	scenario.Verify()
}

func TestUpload_PushWhenTrailerChangedEvenIfSynced(t *testing.T) {
	// Commit has remote bookmark, but trailer needs update - must push
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{ID: "aaaaaaaaaaaa", Parents: []string{"root"}, IsMutable: true, Description: "A\n"},
		jjtest.Commit{
			ID:              "bbbbbbbbbbbb",
			Parents:         []string{"aaaaaaaaaaaa"},
			IsMutable:       true,
			Description:     "B\n", // Missing trailer
			RemoteBookmarks: []string{"og/push-bbbbbbbbbbbb"},
		},
	)

	// jj returns children first (B, A), we reverse to (A, B)
	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(mutable())~(mutable())"},
			Output: jjtest.LogOutput("root"),
		},
		// Trailer update for B - describe changes the commit, clearing remote bookmark
		jjtest.Call{
			Args:   []string{"metaedit", "bbbbbbbbbbbb", "-m", "B\n\nforge-parent: aaaaaaaaaaaa\n"},
			Output: jjtest.EmptyOutput(),
			SideEffect: func(r *jjtest.FakeRepo) {
				jjtest.UpdateDescription("bbbbbbbbbbbb", "B\n\nforge-parent: aaaaaaaaaaaa\n")(r)
				// Describe changes the commit, so the remote bookmark no longer points to it
				r.Commits["bbbbbbbbbbbb"].RemoteBookmarks = nil
			},
		},
		// Push phase: re-resolve Revs(revset)
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("bbbbbbbbbbbb", "aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "aaaaaaaaaaaa", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "bbbbbbbbbbbb", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
	)

	client := scenario.Client()
	result, err := Upload(context.Background(), client, "mutable()", testRemote, testUI)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.Pushed != 2 {
		t.Errorf("expected 2 pushes, got %d", result.Pushed)
	}
	if result.SkippedSynced != 0 {
		t.Errorf("expected 0 skipped synced, got %d", result.SkippedSynced)
	}
	scenario.Verify()
}

func TestUpload_MixedSkipAndPush(t *testing.T) {
	// Mix: empty, anonymous, synced, and needs-push commits
	// jj returns in reverse topo order, we reverse to get parents first
	// LogOutput order is reversed by Upload
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{ID: "anon0000", Parents: []string{"root"}, IsMutable: true, Description: ""},
		jjtest.Commit{ID: "emptyyyy", Parents: []string{"root"}, IsMutable: true, Description: "empty\n", IsEmpty: true},
		jjtest.Commit{ID: "needspsh", Parents: []string{"root"}, IsMutable: true, Description: "needs push\n"},
		jjtest.Commit{ID: "synced00", Parents: []string{"root"}, IsMutable: true, Description: "synced\n", RemoteBookmarks: []string{"og/push-synced00"}},
	)

	// jj returns: synced00, needspsh, emptyyyy, anon0000 (reverse topo)
	// After reverse: anon0000, emptyyyy, needspsh, synced00
	scenario := jjtest.NewScenario(t, repo,
		// UpdateTrailers phase
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "mutable()"},
			Output: jjtest.LogOutput("synced00", "needspsh", "emptyyyy", "anon0000"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(mutable())~(mutable())"},
			Output: jjtest.LogOutput("root"),
		},
		// No trailer changes needed (all have immutable parent root)
		// Push phase: no re-resolve (trailers unchanged, revs reused)
		// anon0000: silently skipped (anonymous)
		// emptyyyy: silently skipped (empty)
		// needspsh: pushed
		jjtest.Call{
			Args:   []string{"git", "push", "--change", "needspsh", "--remote", testRemote, "--allow-new"},
			Output: jjtest.EmptyOutput(),
		},
		// synced00: skipped (synced)
	)

	client := scenario.Client()
	result, err := Upload(context.Background(), client, "mutable()", testRemote, testUI)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.SkippedEmpty != 1 {
		t.Errorf("expected 1 skipped empty, got %d", result.SkippedEmpty)
	}
	if result.SkippedAnonymous != 1 {
		t.Errorf("expected 1 skipped anonymous, got %d", result.SkippedAnonymous)
	}
	if result.SkippedSynced != 1 {
		t.Errorf("expected 1 skipped synced, got %d", result.SkippedSynced)
	}
	if result.Pushed != 1 {
		t.Errorf("expected 1 push, got %d", result.Pushed)
	}
	scenario.Verify()
}

func TestUpload_SkipImmutableChange(t *testing.T) {
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{ID: "aaaaaaaaaaaa", Parents: []string{"root"}, IsMutable: false, Description: "A\n"},
	)

	scenario := jjtest.NewScenario(t, repo,
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "all()"},
			Output: jjtest.LogOutput("aaaaaaaaaaaa"),
		},
		jjtest.Call{
			Args:   []string{"log", "--no-graph", "--template", templateMatcher, "-r", "parents(all())~(all())"},
			Output: jjtest.LogOutput("root"),
		},
		// No push - skipped (immutable)
	)

	client := scenario.Client()
	result, err := Upload(context.Background(), client, "all()", testRemote, testUI)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.SkippedImmutable != 1 {
		t.Errorf("expected 1 skipped immutable, got %d", result.SkippedImmutable)
	}
	if result.Pushed != 0 {
		t.Errorf("expected 0 pushes, got %d", result.Pushed)
	}
	scenario.Verify()
}

// templateMatcher matches the jj log template used by client.Revs()
var templateMatcher = `change_id.short()++" "++commit_id.short()++" "++conflict++" "++divergent++" "++!immutable++" "++empty++" "++parents.map(|c| c.change_id().short()).join(",")++" "++bookmarks.map(|b| b.name()).join(",")++" "++remote_bookmarks.map(|b| b.remote() ++ "/" ++ b.name()).join(",")++" "++description.escape_json()++" "++"\n"`
