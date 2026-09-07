package tangled

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TODO(upstream): Tangled's appview assigns pull request numbers when it
// ingests a record and exposes them only through HTML pages and Atom feeds.
// No XRPC or JSON endpoint maps a pull record key to its number. The number
// was removed from the record itself in tangled.org/core commit 7147495e
// ("remove pull id from the pull record and instead generate ids purely
// appview side"). This file reconstructs the mapping from the repository's
// Atom feed plus the PR page's "data-aturi" attribute. Replace it with a
// structured lookup once upstream adds the at-uri to feed entries or a
// resolve-by-record-key route.

// httpDoer abstracts *http.Client for test injection.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// errPullNotFound is returned when no candidate PR page carries the record key.
var errPullNotFound = errors.New("pull request not found on the Tangled web UI")

const (
	// maxCandidatePages bounds how many PR pages are fetched per attempt.
	maxCandidatePages = 8
	// maxPageBytes bounds how much of a PR page is read when searching for the at-uri.
	maxPageBytes = 4 << 20
	// createLookupAttempts is how many times CreateReview polls for a freshly
	// created PR to appear in the feed (the appview ingests from the firehose).
	createLookupAttempts = 5
)

// pullLinkRegex matches an Atom entry link for a PR page (not a round page).
var pullLinkRegex = regexp.MustCompile(`/pulls/(\d+)/?$`)

// webClient resolves numbered pull request pages on the Tangled web UI.
type webClient struct {
	baseURL string // e.g. https://tangled.org
	http    httpDoer
	sleep   func(time.Duration)
}

func newWebClient(baseURL string) *webClient {
	return &webClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 15 * time.Second},
		sleep:   time.Sleep,
	}
}

// pullPageURL returns the web URL of the pull request with the given record
// key, e.g. https://tangled.org/owner/repo/pulls/42.
//
// Candidates come from the repository's Atom feed (newest first, entries whose
// title matches titleHint checked first). Each candidate page is fetched and
// matched on its at-uri. With attempts > 1 the lookup is retried with a
// linear backoff to absorb ingest lag after creation.
func (w *webClient) pullPageURL(ctx context.Context, ref *RepoRef, rkey, titleHint string, attempts int) (string, error) {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			default:
			}
			w.sleep(time.Duration(attempt) * time.Second)
		}
		numbers, err := w.feedPullNumbers(ctx, ref, titleHint)
		if err != nil {
			lastErr = err
			continue
		}
		if len(numbers) > maxCandidatePages {
			numbers = numbers[:maxCandidatePages]
		}
		lastErr = errPullNotFound
		for _, n := range numbers {
			found, err := w.pageHasPull(ctx, ref, n, rkey)
			if err != nil {
				lastErr = err
				continue
			}
			if found {
				return w.pullURL(ref, n), nil
			}
		}
	}
	return "", lastErr
}

func (w *webClient) repoURL(ref *RepoRef) string {
	return fmt.Sprintf("%s/%s/%s", w.baseURL, ref.Owner, ref.Name)
}

func (w *webClient) pullURL(ref *RepoRef, number int) string {
	return fmt.Sprintf("%s/pulls/%d", w.repoURL(ref), number)
}

// atomFeed is the subset of an Atom document needed to enumerate PR entries.
type atomFeed struct {
	Entries []struct {
		Title string `xml:"title"`
		Links []struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

// feedPullNumbers lists PR numbers from the repository feed, newest first.
// Entries whose title is exactly "[PR #N] <titleHint>" are moved to the front.
func (w *webClient) feedPullNumbers(ctx context.Context, ref *RepoRef, titleHint string) ([]int, error) {
	body, err := w.get(ctx, w.repoURL(ref)+"/feed.atom?include=pulls", "application/atom+xml")
	if err != nil {
		return nil, fmt.Errorf("failed to read repository feed: %w", err)
	}
	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("failed to parse repository feed: %w", err)
	}
	seen := make(map[int]bool)
	var preferred, others []int
	for _, entry := range feed.Entries {
		for _, link := range entry.Links {
			m := pullLinkRegex.FindStringSubmatch(link.Href)
			if m == nil || strings.Contains(link.Href, "/round/") {
				continue
			}
			n, err := strconv.Atoi(m[1])
			if err != nil || seen[n] {
				continue
			}
			seen[n] = true
			if titleHint != "" && entry.Title == fmt.Sprintf("[PR #%d] %s", n, titleHint) {
				preferred = append(preferred, n)
			} else {
				others = append(others, n)
			}
		}
	}
	return append(preferred, others...), nil
}

// pageHasPull reports whether the PR page for number renders the given record
// key as its own at-uri, which the page marks with a data-aturi attribute.
// Other occurrences (comments, stacks) are ignored.
func (w *webClient) pageHasPull(ctx context.Context, ref *RepoRef, number int, rkey string) (bool, error) {
	body, err := w.get(ctx, w.pullURL(ref, number), "text/html")
	if err != nil {
		var statusErr *httpStatusError
		if errors.As(err, &statusErr) && statusErr.code == http.StatusNotFound {
			return false, nil
		}
		return false, fmt.Errorf("failed to read PR page #%d: %w", number, err)
	}
	pattern := regexp.MustCompile(`data-aturi="at://[^"]*/sh\.tangled\.repo\.pull/` + regexp.QuoteMeta(rkey) + `"`)
	return pattern.Match(body), nil
}

// httpStatusError reports a non-2xx response.
type httpStatusError struct {
	code int
	url  string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("GET %s returned HTTP %d", e.url, e.code)
}

// get fetches a URL and returns up to maxPageBytes of its body.
func (w *webClient) get(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "jj-forge")
	resp, err := w.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &httpStatusError{code: resp.StatusCode, url: url}
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
}
