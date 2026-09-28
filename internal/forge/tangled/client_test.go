package tangled

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jjtest"
)

const testRepoURL = "git@tangled.org:alice.example.com/repo"
const testPullsURL = "https://tangled.org/alice.example.com/repo/pulls"

// call records one executor invocation.
type call struct {
	args []string
	opts cmd.Opts
}

// scriptedExecutor returns canned stdout keyed by the joined argument string
// (excluding the binary), recording every call. Unknown commands fail the test.
func scriptedExecutor(t *testing.T, responses map[string]string, calls *[]call) cmd.Executor {
	t.Helper()
	return func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		*calls = append(*calls, call{args: args, opts: opts})
		key := strings.Join(args, " ")
		out, ok := responses[key]
		if !ok {
			t.Errorf("unexpected command: %s", key)
			return nil, errors.New("unexpected command: " + key)
		}
		return &cmd.Result{Stdout: out}, nil
	}
}

func hasEnv(opts cmd.Opts, kv string) bool {
	return slices.Contains(opts.Env, kv)
}

// withTestWeb points a client at a fake appview serving the given PR pages and
// feed entries (relative repo paths + titles), and disables retry sleeps.
func withTestWeb(t *testing.T, client *Client, pages map[int]string, entries ...[2]string) (*Client, string) {
	t.Helper()
	fw := newFakeWeb(t, "alice.example.com", "repo", pages)
	base := fw.srv.URL + "/alice.example.com/repo"
	var rendered []string
	for _, e := range entries {
		rendered = append(rendered, feedEntry(base, e[0], e[1]))
	}
	fw.setFeed(rendered...)
	client.WithWebBaseURL(fw.srv.URL)
	client.web.sleep = func(time.Duration) {}
	return client, base
}

func TestCreateReview_Success(t *testing.T) {
	var calls []call
	exec := scriptedExecutor(t, map[string]string{
		"tg pr create --repo alice.example.com/repo --title Test PR --body Test body --head push-abc123 --base up/main --json": `{"uri":"at://did:plc:me/sh.tangled.repo.pull/3mujbgpmfg422","title":"Test PR","base":"main","head":"push-abc123"}`,
	}, &calls)
	jjScenario := jjtest.NewScenario(t, jjtest.NewFakeRepo(),
		jjtest.Call{Args: []string{"git", "export"}},
	)
	client, base := withTestWeb(t, NewClient("/repo/.jj/repo/store/git", "up", jjScenario.Client(), exec),
		map[int]string{7: prPage("3mujbgpmfg422", "")},
		[2]string{"pulls/7", "[PR #7] Test PR"})

	result, err := client.CreateReview(context.Background(), testRepoURL, forge.ReviewCreateParams{
		Title:      "Test PR",
		Body:       "Test body",
		FromBranch: "push-abc123",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("CreateReview() error = %v", err)
	}
	want := &forge.ReviewCreateResult{ID: "3mujbgpmfg422", URL: base + "/pulls/7"}
	if diff := cmp.Diff(want, result); diff != "" {
		t.Errorf("result mismatch (-want +got):\n%s", diff)
	}
	jjScenario.Verify()
	if len(calls) != 1 {
		t.Fatalf("expected 1 tg call, got %d", len(calls))
	}
	if !hasEnv(calls[0].opts, "GIT_DIR=/repo/.jj/repo/store/git") {
		t.Errorf("expected GIT_DIR in env, got %v", calls[0].opts.Env)
	}
}

func TestCreateReview_NoJJClient(t *testing.T) {
	var calls []call
	exec := scriptedExecutor(t, map[string]string{
		"tg pr create --repo alice.example.com/repo --title T --body  --head push-abc --base main --json": `{"uri":"at://did:plc:me/sh.tangled.repo.pull/3abc"}`,
	}, &calls)
	client, _ := withTestWeb(t, NewClient("", "", nil, exec), nil) // feed empty: not ingested

	result, err := client.CreateReview(context.Background(), testRepoURL, forge.ReviewCreateParams{
		Title: "T", FromBranch: "push-abc", ToBranch: "main",
	})
	if err != nil {
		t.Fatalf("CreateReview() error = %v", err)
	}
	if result.ID != "3abc" {
		t.Errorf("expected ID 3abc, got %s", result.ID)
	}
	if result.URL != testPullsURL {
		t.Errorf("expected fallback to pulls list URL, got %s", result.URL)
	}
	if hasEnv(calls[0].opts, "GIT_DIR=") {
		t.Errorf("unexpected GIT_DIR in env: %v", calls[0].opts.Env)
	}
}

func TestCreateReview_Errors(t *testing.T) {
	t.Run("reviewers requested", func(t *testing.T) {
		client := NewClient("", "up", nil, nil) // no tg or jj calls expected
		_, err := client.CreateReview(context.Background(), testRepoURL, forge.ReviewCreateParams{
			Title: "T", FromBranch: "h", ToBranch: "main", Reviewers: []string{"bob"},
		})
		if err == nil || !strings.Contains(err.Error(), "assigning reviewers (bob) is not implemented") {
			t.Errorf("expected reviewers error, got %v", err)
		}
	})
	t.Run("executor error", func(t *testing.T) {
		exec := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
			return nil, errors.New("not logged in")
		}
		client := NewClient("", "up", nil, exec)
		_, err := client.CreateReview(context.Background(), testRepoURL, forge.ReviewCreateParams{Title: "T", FromBranch: "h", ToBranch: "main"})
		if err == nil || !strings.Contains(err.Error(), "failed to create PR") {
			t.Errorf("expected create error, got %v", err)
		}
	})
	t.Run("missing uri", func(t *testing.T) {
		exec := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
			return &cmd.Result{Stdout: `{}`}, nil
		}
		client := NewClient("", "up", nil, exec)
		_, err := client.CreateReview(context.Background(), testRepoURL, forge.ReviewCreateParams{Title: "T", FromBranch: "h", ToBranch: "main"})
		if err == nil || !strings.Contains(err.Error(), "no record URI") {
			t.Errorf("expected missing URI error, got %v", err)
		}
	})
	t.Run("invalid repo URI", func(t *testing.T) {
		client := NewClient("", "up", nil, nil)
		_, err := client.CreateReview(context.Background(), "https://tangled.org/did:plc:onlyrepo", forge.ReviewCreateParams{})
		if err == nil || !strings.Contains(err.Error(), "invalid repository URI") {
			t.Errorf("expected invalid URI error, got %v", err)
		}
	})
	t.Run("jj export error", func(t *testing.T) {
		jjScenario := jjtest.NewScenario(t, jjtest.NewFakeRepo(),
			jjtest.Call{Args: []string{"git", "export"}, Err: errors.New("conflicted bookmark")},
		)
		client := NewClient("", "up", jjScenario.Client(), nil)
		_, err := client.CreateReview(context.Background(), testRepoURL, forge.ReviewCreateParams{Title: "T", FromBranch: "h", ToBranch: "main"})
		if err == nil || !strings.Contains(err.Error(), "export jj bookmarks") {
			t.Errorf("expected export error, got %v", err)
		}
	})
}

func TestMergeReview(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var calls []call
		exec := scriptedExecutor(t, map[string]string{
			"tg pr merge 3abc --repo alice.example.com/repo --json": `{"rkey":"3abc","merged":true,"statusRecorded":true}`,
		}, &calls)
		client := NewClient("", "up", nil, exec)
		if err := client.MergeReview(context.Background(), testRepoURL, "3abc", "c0ffee"); err != nil {
			t.Fatalf("MergeReview() error = %v", err)
		}
	})
	t.Run("merged but status not recorded is still success", func(t *testing.T) {
		var calls []call
		exec := scriptedExecutor(t, map[string]string{
			"tg pr merge 3abc --repo alice.example.com/repo --json": `{"rkey":"3abc","merged":true,"statusRecorded":false,"warnings":["could not record merged pull request status"]}`,
		}, &calls)
		client := NewClient("", "up", nil, exec)
		if err := client.MergeReview(context.Background(), testRepoURL, "3abc", "c0ffee"); err != nil {
			t.Fatalf("MergeReview() error = %v", err)
		}
	})
	t.Run("not merged", func(t *testing.T) {
		var calls []call
		exec := scriptedExecutor(t, map[string]string{
			"tg pr merge 3abc --repo alice.example.com/repo --json": `{"rkey":"3abc","merged":false,"warnings":["conflict"]}`,
		}, &calls)
		client := NewClient("", "up", nil, exec)
		err := client.MergeReview(context.Background(), testRepoURL, "3abc", "c0ffee")
		if err == nil || !strings.Contains(err.Error(), "conflict") {
			t.Fatalf("expected not-merged error, got %v", err)
		}
	})
	t.Run("executor error", func(t *testing.T) {
		exec := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
			return nil, errors.New("boom")
		}
		client := NewClient("", "up", nil, exec)
		err := client.MergeReview(context.Background(), testRepoURL, "3abc", "c0ffee")
		if err == nil || !strings.Contains(err.Error(), "failed to merge PR #3abc") {
			t.Fatalf("expected merge error, got %v", err)
		}
	})
}

func TestCloseReview(t *testing.T) {
	var calls []call
	exec := scriptedExecutor(t, map[string]string{
		"tg pr close 3abc --repo alice.example.com/repo --json": `{"rkey":"3abc","state":"closed"}`,
	}, &calls)
	client := NewClient("", "up", nil, exec)
	if err := client.CloseReview(context.Background(), testRepoURL, "3abc"); err != nil {
		t.Fatalf("CloseReview() error = %v", err)
	}
}

func TestFindReview(t *testing.T) {
	list := `{"items":[
		{"rkey":"3aaa","uri":"at://did:plc:x/sh.tangled.repo.pull/3aaa","title":"Other","state":"merged","sourceBranch":"push-other","targetBranch":"main"},
		{"rkey":"3bbb","uri":"at://did:plc:x/sh.tangled.repo.pull/3bbb","title":"Mine","state":"open","sourceBranch":"push-abc123","targetBranch":"main"}
	]}`
	var calls []call
	exec := scriptedExecutor(t, map[string]string{
		"tg pr list alice.example.com/repo --json": list,
	}, &calls)
	client, base := withTestWeb(t, NewClient("", "up", nil, exec),
		map[int]string{1: prPage("3aaa", ""), 2: prPage("3bbb", "")},
		[2]string{"pulls/2", "[PR #2] Mine"}, [2]string{"pulls/1", "[PR #1] Other"})

	got, err := client.FindReview(context.Background(), testRepoURL, "push-abc123")
	if err != nil {
		t.Fatalf("FindReview() error = %v", err)
	}
	want := &forge.ReviewDetails{ID: "3bbb", URL: base + "/pulls/2", State: forge.ReviewStateOpen, Title: "Mine"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("FindReview() mismatch (-want +got):\n%s", diff)
	}

	missing, err := client.FindReview(context.Background(), testRepoURL, "push-nope")
	if err != nil {
		t.Fatalf("FindReview() error = %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for unknown branch, got %+v", missing)
	}
}

func TestGetReview(t *testing.T) {
	var calls []call
	exec := scriptedExecutor(t, map[string]string{
		"tg pr view 3abc --repo alice.example.com/repo --json": `{"rkey":"3abc","title":"Test PR","state":"merged","body":"Body text","sourceBranch":"push-abc","targetBranch":"main"}`,
	}, &calls)
	client := NewClient("", "up", nil, exec)

	got, err := client.GetReview(context.Background(), testRepoURL, "3abc")
	if err != nil {
		t.Fatalf("GetReview() error = %v", err)
	}
	want := &forge.ReviewDetails{ID: "3abc", URL: testPullsURL, State: forge.ReviewStateMerged, Title: "Test PR", Body: "Body text"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GetReview() mismatch (-want +got):\n%s", diff)
	}
}

func TestUpdateReview(t *testing.T) {
	var calls []call
	exec := scriptedExecutor(t, map[string]string{
		"tg pr edit 3abc --body New body": "",
	}, &calls)
	client := NewClient("", "up", nil, exec)
	if err := client.UpdateReview(context.Background(), testRepoURL, "3abc", "New body"); err != nil {
		t.Fatalf("UpdateReview() error = %v", err)
	}
}

func TestSyncReview(t *testing.T) {
	var calls []call
	exec := scriptedExecutor(t, map[string]string{
		"tg pr view 3abc --repo alice.example.com/repo --json": `{"rkey":"3abc","title":"T","state":"open","sourceBranch":"push-abc","targetBranch":"develop"}`,
		"tg pr update 3abc --base up/develop":                  "",
	}, &calls)
	jjScenario := jjtest.NewScenario(t, jjtest.NewFakeRepo(),
		jjtest.Call{Args: []string{"git", "export"}},
	)
	client := NewClient("/gitdir", "up", jjScenario.Client(), exec)

	if err := client.SyncReview(context.Background(), testRepoURL, "3abc"); err != nil {
		t.Fatalf("SyncReview() error = %v", err)
	}
	jjScenario.Verify()
	if len(calls) != 2 {
		t.Fatalf("expected 2 tg calls, got %d", len(calls))
	}
	if calls[1].args[1] != "pr" || calls[1].args[2] != "update" {
		t.Errorf("expected second call to be pr update, got %v", calls[1].args)
	}
}

func TestSyncReview_NoTargetBranch(t *testing.T) {
	var calls []call
	exec := scriptedExecutor(t, map[string]string{
		"tg pr view 3abc --repo alice.example.com/repo --json": `{"rkey":"3abc","title":"T","state":"open"}`,
	}, &calls)
	client := NewClient("", "up", nil, exec)
	err := client.SyncReview(context.Background(), testRepoURL, "3abc")
	if err == nil || !strings.Contains(err.Error(), "no target branch") {
		t.Fatalf("expected target branch error, got %v", err)
	}
}

func TestDefaultBranch(t *testing.T) {
	var calls []call
	exec := scriptedExecutor(t, map[string]string{
		"git ls-remote --symref " + testRepoURL + " HEAD": "warning: redirecting to https://knot1.tangled.sh/did:plc:x/\nref: refs/heads/master\tHEAD\n2e7c4a1483d1edfff9538e0eb6022f1ee28b3f4b\tHEAD\n",
	}, &calls)
	client := NewClient("", "up", nil, exec)

	for i := 0; i < 2; i++ {
		branch, err := client.DefaultBranch(context.Background(), testRepoURL)
		if err != nil {
			t.Fatalf("DefaultBranch() error = %v", err)
		}
		if branch != "master" {
			t.Errorf("expected master, got %q", branch)
		}
	}
	if len(calls) != 1 {
		t.Errorf("expected default branch to be cached after one call, got %d calls", len(calls))
	}
	if !hasEnv(calls[0].opts, "GIT_TERMINAL_PROMPT=0") {
		t.Errorf("expected GIT_TERMINAL_PROMPT=0 in env, got %v", calls[0].opts.Env)
	}
}

func TestDefaultBranch_Errors(t *testing.T) {
	t.Run("no symref", func(t *testing.T) {
		exec := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
			return &cmd.Result{Stdout: "2e7c4a1483d1edfff9538e0eb6022f1ee28b3f4b\tHEAD\n"}, nil
		}
		_, err := DefaultBranch(context.Background(), exec, testRepoURL)
		if err == nil || !strings.Contains(err.Error(), "did not advertise") {
			t.Errorf("expected missing symref error, got %v", err)
		}
	})
	t.Run("executor error", func(t *testing.T) {
		exec := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
			return nil, errors.New("auth failed")
		}
		_, err := DefaultBranch(context.Background(), exec, testRepoURL)
		if err == nil || !strings.Contains(err.Error(), "auth failed") {
			t.Errorf("expected wrapped error, got %v", err)
		}
	})
}

func TestSetupRuleset_Unsupported(t *testing.T) {
	client := NewClient("", "up", nil, nil)
	if err := client.SetupRuleset(context.Background(), testRepoURL); err == nil {
		t.Error("expected SetupRuleset to report unsupported")
	}
}

func TestFormatAndParseID(t *testing.T) {
	client := NewClient("", "up", nil, nil)
	if got := client.FormatID("3mujbgpmfg422"); got != "pr/3mujbgpmfg422" {
		t.Errorf("FormatID() = %q", got)
	}
	tests := []struct {
		id      string
		want    string
		wantErr bool
	}{
		{"pr/3mujbgpmfg422", "3mujbgpmfg422", false},
		{"3mujbgpmfg422", "3mujbgpmfg422", false},
		{"pr/", "", true},
		{"pr/has space", "", true},
		{"pr/has/slash", "", true},
	}
	for _, tt := range tests {
		got, err := client.ParseID(tt.id)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseID(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestFormatHeadBranch(t *testing.T) {
	client := NewClient("", "up", nil, nil)
	branch, err := client.FormatHeadBranch(context.Background(), nil, "og", "aaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("FormatHeadBranch() error = %v", err)
	}
	if branch != "push-aaaaaaaaaaaa" {
		t.Errorf("FormatHeadBranch() = %q", branch)
	}
	if client.SupportsForks() {
		t.Error("SupportsForks() should be false")
	}
}

func TestWithTGCommand(t *testing.T) {
	var calls []call
	exec := scriptedExecutor(t, map[string]string{
		"my-tg pr edit 3abc --body b": "",
	}, &calls)
	client := NewClient("", "up", nil, exec).WithTGCommand("my-tg")
	if err := client.UpdateReview(context.Background(), testRepoURL, "3abc", "b"); err != nil {
		t.Fatalf("UpdateReview() error = %v", err)
	}
}

func TestCLI_AuthStatusAndViewRepo(t *testing.T) {
	var calls []call
	exec := scriptedExecutor(t, map[string]string{
		"tg auth status --json":                      `{"authenticated":true,"status":"active","did":"did:plc:me","handle":"alice.example.com"}`,
		"tg repo view alice.example.com/repo --json": `{"name":"repo","uri":"at://did:plc:me/sh.tangled.repo/repo","author":"alice.example.com","knot":"knot1.tangled.sh","repoDid":"did:plc:repo"}`,
	}, &calls)
	cli := NewCLI(exec)

	status, err := cli.AuthStatus(context.Background())
	if err != nil {
		t.Fatalf("AuthStatus() error = %v", err)
	}
	if !status.Authenticated || status.DID != "did:plc:me" || status.Handle != "alice.example.com" {
		t.Errorf("unexpected auth status: %+v", status)
	}

	info, err := cli.ViewRepo(context.Background(), "alice.example.com/repo")
	if err != nil {
		t.Fatalf("ViewRepo() error = %v", err)
	}
	if info.OwnerDID() != "did:plc:me" {
		t.Errorf("OwnerDID() = %q", info.OwnerDID())
	}
	if info.RepoDID != "did:plc:repo" || info.Knot != "knot1.tangled.sh" {
		t.Errorf("unexpected repo info: %+v", info)
	}
}

func TestCLI_RunJSON_InvalidOutput(t *testing.T) {
	exec := func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
		return &cmd.Result{Stdout: "not json"}, nil
	}
	cli := NewCLI(exec)
	if _, err := cli.AuthStatus(context.Background()); err == nil || !strings.Contains(err.Error(), "parse tg output") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestFakeForge_ImplementsSyncer(t *testing.T) {
	var f forge.Forge = NewFakeForge()
	if _, ok := f.(forge.ReviewSyncer); !ok {
		t.Fatal("FakeForge should implement forge.ReviewSyncer")
	}
	var c forge.Forge = NewClient("", "up", nil, nil)
	if _, ok := c.(forge.ReviewSyncer); !ok {
		t.Fatal("Client should implement forge.ReviewSyncer")
	}
}
