package tangled

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeWeb serves a repository Atom feed and PR pages like the Tangled appview.
type fakeWeb struct {
	t        *testing.T
	srv      *httptest.Server
	feed     atomic.Value // string
	pages    map[int]string
	feedHits atomic.Int32
	pageHits atomic.Int32
}

func newFakeWeb(t *testing.T, owner, repo string, pages map[int]string) *fakeWeb {
	t.Helper()
	fw := &fakeWeb{t: t, pages: pages}
	fw.feed.Store("")
	mux := http.NewServeMux()
	prefix := "/" + owner + "/" + repo
	mux.HandleFunc(prefix+"/feed.atom", func(w http.ResponseWriter, r *http.Request) {
		fw.feedHits.Add(1)
		if r.URL.Query().Get("include") != "pulls" {
			t.Errorf("feed request missing include=pulls: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprint(w, fw.feed.Load().(string))
	})
	mux.HandleFunc(prefix+"/pulls/", func(w http.ResponseWriter, r *http.Request) {
		fw.pageHits.Add(1)
		var n int
		if _, err := fmt.Sscanf(strings.TrimPrefix(r.URL.Path, prefix+"/pulls/"), "%d", &n); err != nil {
			http.NotFound(w, r)
			return
		}
		body, ok := fw.pages[n]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	})
	fw.srv = httptest.NewServer(mux)
	t.Cleanup(fw.srv.Close)
	return fw
}

func (fw *fakeWeb) setFeed(entries ...string) {
	fw.feed.Store(`<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>t</title>` +
		strings.Join(entries, "") + `</feed>`)
}

// feedEntry renders a feed entry for path, relative to the repo (e.g. "pulls/3").
func feedEntry(base, path, title string) string {
	return fmt.Sprintf(`<entry><title>%s</title><updated>2026-09-07T14:22:45Z</updated><id>tag:x</id><link href="%s/%s"></link></entry>`, title, base, path)
}

// prPage renders a PR page whose own at-uri has the given rkey, plus extra markup.
func prPage(rkey, extra string) string {
	return `<html><body><div id="at-uri-panel"><span data-aturi="at://did:plc:owner/sh.tangled.repo.pull/` + rkey + `">at://did:plc:owner/sh.tangled.repo.pull/` + rkey + `</span></div>` + extra + `</body></html>`
}

func newTestWebClient(fw *fakeWeb) *webClient {
	w := newWebClient(fw.srv.URL)
	w.sleep = func(time.Duration) {}
	return w
}

var testRef = &RepoRef{Host: "tangled.org", Owner: "alice.example.com", Name: "repo"}

func TestPullPageURL_TitleHintChecksMatchingPageFirst(t *testing.T) {
	fw := newFakeWeb(t, "alice.example.com", "repo", map[int]string{
		1: prPage("3aaa", ""),
		2: prPage("3bbb", ""),
		3: prPage("3ccc", ""),
	})
	base := fw.srv.URL + "/alice.example.com/repo"
	fw.setFeed(
		feedEntry(base, "pulls/3/round/1/", "[PR #3] Third (round #1)"),
		feedEntry(base, "pulls/3", "[PR #3] Third"),
		feedEntry(base, "pulls/2", "[PR #2] Second"),
		feedEntry(base, "pulls/1", "[PR #1] First"),
	)
	w := newTestWebClient(fw)

	got, err := w.pullPageURL(context.Background(), testRef, "3bbb", "Second", 1)
	if err != nil {
		t.Fatalf("pullPageURL() error = %v", err)
	}
	if want := base + "/pulls/2"; got != want {
		t.Errorf("pullPageURL() = %q, want %q", got, want)
	}
	if hits := fw.pageHits.Load(); hits != 1 {
		t.Errorf("expected exactly 1 page fetch with a title hint, got %d", hits)
	}
}

func TestPullPageURL_ScansNewestFirstWithoutHint(t *testing.T) {
	fw := newFakeWeb(t, "alice.example.com", "repo", map[int]string{
		1: prPage("3aaa", ""),
		2: prPage("3bbb", ""),
		3: prPage("3ccc", ""),
	})
	base := fw.srv.URL + "/alice.example.com/repo"
	fw.setFeed(
		feedEntry(base, "pulls/3", "[PR #3] Third"),
		feedEntry(base, "pulls/2", "[PR #2] Second"),
		feedEntry(base, "pulls/1", "[PR #1] First"),
	)
	w := newTestWebClient(fw)

	got, err := w.pullPageURL(context.Background(), testRef, "3aaa", "", 1)
	if err != nil {
		t.Fatalf("pullPageURL() error = %v", err)
	}
	if want := base + "/pulls/1"; got != want {
		t.Errorf("pullPageURL() = %q, want %q", got, want)
	}
	if hits := fw.pageHits.Load(); hits != 3 {
		t.Errorf("expected 3 page fetches scanning newest first, got %d", hits)
	}
}

func TestPullPageURL_RetriesUntilFeedCatchesUp(t *testing.T) {
	fw := newFakeWeb(t, "alice.example.com", "repo", map[int]string{
		4: prPage("3ddd", ""),
	})
	base := fw.srv.URL + "/alice.example.com/repo"
	fw.setFeed() // empty: not ingested yet
	w := newTestWebClient(fw)
	var slept int
	w.sleep = func(time.Duration) {
		slept++
		if slept == 2 {
			fw.setFeed(feedEntry(base, "pulls/4", "[PR #4] Fourth"))
		}
	}

	got, err := w.pullPageURL(context.Background(), testRef, "3ddd", "Fourth", 5)
	if err != nil {
		t.Fatalf("pullPageURL() error = %v", err)
	}
	if want := base + "/pulls/4"; got != want {
		t.Errorf("pullPageURL() = %q, want %q", got, want)
	}
	if slept != 2 {
		t.Errorf("expected 2 backoff sleeps, got %d", slept)
	}
}

func TestPullPageURL_NotFound(t *testing.T) {
	fw := newFakeWeb(t, "alice.example.com", "repo", map[int]string{
		1: prPage("3aaa", ""),
	})
	base := fw.srv.URL + "/alice.example.com/repo"
	fw.setFeed(feedEntry(base, "pulls/1", "[PR #1] First"))
	w := newTestWebClient(fw)

	_, err := w.pullPageURL(context.Background(), testRef, "3zzz", "", 2)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
	if hits := fw.feedHits.Load(); hits != 2 {
		t.Errorf("expected 2 feed fetches for 2 attempts, got %d", hits)
	}
}

func TestPullPageURL_IgnoresRkeyOutsideDataAturi(t *testing.T) {
	// A sibling PR's page may mention our rkey in a stack or comment, but only
	// the data-aturi attribute identifies the page's own record.
	fw := newFakeWeb(t, "alice.example.com", "repo", map[int]string{
		1: prPage("3aaa", `<a href="https://pdsls.dev/at://did:plc:owner/sh.tangled.repo.pull/3bbb">sibling</a>`),
		2: prPage("3bbb", ""),
	})
	base := fw.srv.URL + "/alice.example.com/repo"
	fw.setFeed(
		feedEntry(base, "pulls/1", "[PR #1] First"),
		feedEntry(base, "pulls/2", "[PR #2] Second"),
	)
	w := newTestWebClient(fw)

	got, err := w.pullPageURL(context.Background(), testRef, "3bbb", "", 1)
	if err != nil {
		t.Fatalf("pullPageURL() error = %v", err)
	}
	if want := base + "/pulls/2"; got != want {
		t.Errorf("pullPageURL() = %q, want %q", got, want)
	}
}

func TestPullPageURL_DIDOwnerAndMissingPage(t *testing.T) {
	ref := &RepoRef{Host: "tangled.org", Owner: "did:plc:abc", Name: "repo"}
	fw := newFakeWeb(t, "did:plc:abc", "repo", map[int]string{
		2: prPage("3bbb", ""),
		// PR 3 is in the feed but its page 404s (e.g. deleted), so it must be skipped.
	})
	base := fw.srv.URL + "/did:plc:abc/repo"
	fw.setFeed(
		feedEntry(base, "pulls/3", "[PR #3] Gone"),
		feedEntry(base, "pulls/2", "[PR #2] Second"),
	)
	w := newTestWebClient(fw)

	got, err := w.pullPageURL(context.Background(), ref, "3bbb", "", 1)
	if err != nil {
		t.Fatalf("pullPageURL() error = %v", err)
	}
	if want := base + "/pulls/2"; got != want {
		t.Errorf("pullPageURL() = %q, want %q", got, want)
	}
}

func TestPullPageURL_FeedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	w := newWebClient(srv.URL)
	w.sleep = func(time.Duration) {}

	_, err := w.pullPageURL(context.Background(), testRef, "3aaa", "", 1)
	if err == nil || !strings.Contains(err.Error(), "repository feed") {
		t.Fatalf("expected feed error, got %v", err)
	}
}

func TestPullPageURL_ContextCancelledDuringRetry(t *testing.T) {
	fw := newFakeWeb(t, "alice.example.com", "repo", nil)
	fw.setFeed()
	w := newTestWebClient(fw)
	ctx, cancel := context.WithCancel(context.Background())
	w.sleep = func(time.Duration) { cancel() }

	_, err := w.pullPageURL(ctx, testRef, "3aaa", "", 3)
	if err == nil {
		t.Fatal("expected error after cancellation")
	}
}
