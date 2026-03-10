package ssm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/msuozzo/jj-forge/internal/forge"
)

const testRepoName = "projects/my-project/locations/us-central1/repositories/my-repo"
const testHTMLURL = "https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo"

// mockHTTPDoer implements httpDoer for testing.
type mockHTTPDoer struct {
	handler func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	return m.handler(req)
}

// jsonResponse creates an *http.Response with the given status code and JSON body.
func jsonResponse(status int, body interface{}) *http.Response {
	b, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(string(b))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func TestGetReview_Success(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet {
				t.Errorf("unexpected method: %s", req.Method)
			}
			wantSuffix := testRepoName + "/pullRequests/42"
			if !strings.HasSuffix(req.URL.Path, "/"+wantSuffix) && !strings.Contains(req.URL.String(), wantSuffix) {
				t.Errorf("unexpected URL: %s", req.URL.String())
			}
			return jsonResponse(200, pullRequestJSON{
				Name:  testRepoName + "/pullRequests/42",
				Title: "Test PR",
				Body:  "Test body",
				State: "OPEN",
			}), nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	review, err := client.GetReview(context.Background(), "", 42)
	if err != nil {
		t.Fatalf("GetReview() error = %v", err)
	}
	if review.Number != 42 {
		t.Errorf("expected number 42, got %d", review.Number)
	}
	if review.Title != "Test PR" {
		t.Errorf("expected title 'Test PR', got %q", review.Title)
	}
	if review.Body != "Test body" {
		t.Errorf("expected body 'Test body', got %q", review.Body)
	}
	if review.State != forge.ReviewStateOpen {
		t.Errorf("expected state open, got %s", review.State)
	}
	if review.URL != testHTMLURL+"/pulls/42" {
		t.Errorf("expected URL %s, got %s", testHTMLURL+"/pulls/42", review.URL)
	}
}

func TestGetReview_Error(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			return jsonResponse(404, map[string]string{"error": "not found"}), nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	_, err := client.GetReview(context.Background(), "", 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDefaultBranch_Success(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet {
				t.Errorf("unexpected method: %s", req.Method)
			}
			return jsonResponse(200, repositoryJSON{
				InitialConfig: struct {
					DefaultBranch string `json:"defaultBranch"`
				}{DefaultBranch: "develop"},
			}), nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	branch, err := client.DefaultBranch(context.Background(), "")
	if err != nil {
		t.Fatalf("DefaultBranch() error = %v", err)
	}
	if branch != "develop" {
		t.Errorf("expected 'develop', got %q", branch)
	}
}

func TestDefaultBranch_FallbackToMain(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			return jsonResponse(200, repositoryJSON{
				InitialConfig: struct {
					DefaultBranch string `json:"defaultBranch"`
				}{DefaultBranch: ""},
			}), nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	branch, err := client.DefaultBranch(context.Background(), "")
	if err != nil {
		t.Fatalf("DefaultBranch() error = %v", err)
	}
	if branch != "main" {
		t.Errorf("expected 'main' fallback, got %q", branch)
	}
}

func TestFormatID(t *testing.T) {
	client := newClientForTest(nil, testRepoName, testHTMLURL)

	id := client.FormatID(42)
	if id != "pr/42" {
		t.Errorf("FormatID(42) = %q, want %q", id, "pr/42")
	}
}

func TestParseID(t *testing.T) {
	client := newClientForTest(nil, testRepoName, testHTMLURL)

	tests := []struct {
		id      string
		want    int
		wantErr bool
	}{
		{"pr/42", 42, false},
		{"42", 42, false},
		{"pr/0", 0, false},
		{"abc", 0, true},
	}

	for _, tt := range tests {
		got, err := client.ParseID(tt.id)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseID(%q) = %d, want %d", tt.id, got, tt.want)
		}
	}
}

func TestFormatHeadBranch(t *testing.T) {
	client := newClientForTest(nil, testRepoName, testHTMLURL)

	branch, err := client.FormatHeadBranch(context.Background(), nil, "og", "aaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("FormatHeadBranch() error = %v", err)
	}
	if branch != "push-aaaaaaaaaaaa" {
		t.Errorf("FormatHeadBranch() = %q, want %q", branch, "push-aaaaaaaaaaaa")
	}
}

func TestSupportsForks(t *testing.T) {
	client := newClientForTest(nil, testRepoName, testHTMLURL)

	if client.SupportsForks() {
		t.Error("SSM client should not support forks")
	}
}

func TestNormalizeRepoURL(t *testing.T) {
	client := newClientForTest(nil, testRepoName, testHTMLURL)

	got, err := client.NormalizeRepoURL("https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo.git")
	if err != nil {
		t.Fatalf("NormalizeRepoURL() error = %v", err)
	}
	want := "https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo"
	if got != want {
		t.Errorf("NormalizeRepoURL() = %q, want %q", got, want)
	}
}

func TestMapSSMState(t *testing.T) {
	tests := []struct {
		state string
		want  forge.ReviewState
	}{
		{"OPEN", forge.ReviewStateOpen},
		{"MERGED", forge.ReviewStateMerged},
		{"CLOSED", forge.ReviewStateClosed},
		{"STATE_UNSPECIFIED", forge.ReviewStateClosed},
	}

	for _, tt := range tests {
		got := mapSSMState(tt.state)
		if got != tt.want {
			t.Errorf("mapSSMState(%v) = %v, want %v", tt.state, got, tt.want)
		}
	}
}

func TestParsePRNumber(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"valid", "projects/p/locations/l/repositories/r/pullRequests/42", 42, false},
		{"zero", "projects/p/locations/l/repositories/r/pullRequests/0", 0, false},
		{"invalid", "invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePRNumber(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parsePRNumber(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parsePRNumber(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestFindReview_Found(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			return jsonResponse(200, listPullRequestsResponse{
				PullRequests: []pullRequestJSON{
					{
						Name:  testRepoName + "/pullRequests/7",
						Title: "Other PR",
						Head:  branchRef{Ref: "other-branch"},
						State: "OPEN",
					},
					{
						Name:  testRepoName + "/pullRequests/10",
						Title: "Target PR",
						Body:  "Found it",
						Head:  branchRef{Ref: "push-abc123"},
						State: "OPEN",
					},
				},
			}), nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	review, err := client.FindReview(context.Background(), "", "push-abc123")
	if err != nil {
		t.Fatalf("FindReview() error = %v", err)
	}
	if review == nil {
		t.Fatal("expected review, got nil")
	}
	if review.Number != 10 {
		t.Errorf("expected number 10, got %d", review.Number)
	}
	if review.Title != "Target PR" {
		t.Errorf("expected title 'Target PR', got %q", review.Title)
	}
}

func TestFindReview_NotFound(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			return jsonResponse(200, listPullRequestsResponse{
				PullRequests: []pullRequestJSON{
					{
						Name: testRepoName + "/pullRequests/1",
						Head: branchRef{Ref: "other-branch"},
					},
				},
			}), nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	review, err := client.FindReview(context.Background(), "", "push-nonexistent")
	if err != nil {
		t.Fatalf("FindReview() error = %v", err)
	}
	if review != nil {
		t.Errorf("expected nil, got %+v", review)
	}
}

func TestCreateReview_Success(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Errorf("unexpected method: %s", req.Method)
			}
			// Return an LRO that is immediately done
			return jsonResponse(200, lroResponse{
				Name: "projects/my-project/operations/op-1",
				Done: true,
				Response: mustMarshal(pullRequestJSON{
					Name:  testRepoName + "/pullRequests/99",
					Title: "New PR",
				}),
			}), nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	result, err := client.CreateReview(context.Background(), "", forge.ReviewCreateParams{
		Title:      "New PR",
		Body:       "PR body",
		FromBranch: "push-abc",
		ToBranch:   "main",
	})
	if err != nil {
		t.Fatalf("CreateReview() error = %v", err)
	}
	if result.Number != 99 {
		t.Errorf("expected number 99, got %d", result.Number)
	}
	if result.URL != testHTMLURL+"/pulls/99" {
		t.Errorf("expected URL %s, got %s", testHTMLURL+"/pulls/99", result.URL)
	}
}

func TestMergeReview_Success(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Errorf("unexpected method: %s", req.Method)
			}
			if !strings.Contains(req.URL.String(), ":merge") {
				t.Errorf("expected :merge in URL, got %s", req.URL.String())
			}
			return jsonResponse(200, lroResponse{
				Name: "projects/my-project/operations/op-2",
				Done: true,
			}), nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	err := client.MergeReview(context.Background(), "", 42)
	if err != nil {
		t.Fatalf("MergeReview() error = %v", err)
	}
}

func TestCloseReview_Success(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Errorf("unexpected method: %s", req.Method)
			}
			if !strings.Contains(req.URL.String(), ":close") {
				t.Errorf("expected :close in URL, got %s", req.URL.String())
			}
			return jsonResponse(200, lroResponse{
				Name: "projects/my-project/operations/op-3",
				Done: true,
			}), nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	err := client.CloseReview(context.Background(), "", 42)
	if err != nil {
		t.Fatalf("CloseReview() error = %v", err)
	}
}

func TestUpdateReview_Success(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPatch {
				t.Errorf("unexpected method: %s", req.Method)
			}
			if !strings.Contains(req.URL.String(), "updateMask=body") {
				t.Errorf("expected updateMask=body in URL, got %s", req.URL.String())
			}
			body, _ := io.ReadAll(req.Body)
			var parsed struct {
				Body string `json:"body"`
			}
			json.Unmarshal(body, &parsed)
			if parsed.Body != "updated body" {
				t.Errorf("expected body 'updated body', got %q", parsed.Body)
			}
			return jsonResponse(200, lroResponse{
				Name: "projects/my-project/operations/op-4",
				Done: true,
			}), nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	err := client.UpdateReview(context.Background(), "", 42, "updated body")
	if err != nil {
		t.Fatalf("UpdateReview() error = %v", err)
	}
}

func TestSetupRuleset_Noop(t *testing.T) {
	client := newClientForTest(nil, testRepoName, testHTMLURL)

	err := client.SetupRuleset(context.Background(), "")
	if err != nil {
		t.Fatalf("SetupRuleset() error = %v", err)
	}
}

func TestLROPolling(t *testing.T) {
	callCount := 0
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			callCount++
			if callCount == 1 {
				// First call: initial merge request, returns pending LRO
				return jsonResponse(200, lroResponse{
					Name: "projects/my-project/operations/op-poll",
					Done: false,
				}), nil
			}
			// Second call: poll, returns done
			return jsonResponse(200, lroResponse{
				Name: "projects/my-project/operations/op-poll",
				Done: true,
			}), nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	err := client.MergeReview(context.Background(), "", 42)
	if err != nil {
		t.Fatalf("MergeReview() with polling error = %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls (initial + poll), got %d", callCount)
	}
}

func TestLROError(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			return jsonResponse(200, lroResponse{
				Name:  "projects/my-project/operations/op-err",
				Done:  true,
				Error: &lroError{Code: 400, Message: "merge conflict"},
			}), nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	err := client.MergeReview(context.Background(), "", 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "merge conflict") {
		t.Errorf("expected error to contain 'merge conflict', got %q", err.Error())
	}
}

func TestHTTPError_Structured(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			return jsonResponse(404, apiError{
				Error: struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
					Status  string `json:"status"`
				}{
					Code:    404,
					Message: "Resource 'projects/p/locations/l/repositories/r' was not found",
					Status:  "NOT_FOUND",
				},
			}), nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	_, err := client.GetReview(context.Background(), "", 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Errorf("expected error to contain 'NOT_FOUND', got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "was not found") {
		t.Errorf("expected error to contain 'was not found', got %q", err.Error())
	}
}

func TestHTTPError_Unstructured(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 500,
				Body:       io.NopCloser(strings.NewReader("internal server error")),
				Header:     http.Header{},
			}, nil
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	_, err := client.GetReview(context.Background(), "", 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected error to contain 'HTTP 500', got %q", err.Error())
	}
}

func TestNetworkError(t *testing.T) {
	mock := &mockHTTPDoer{
		handler: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	client := newClientForTest(mock, testRepoName, testHTMLURL)

	_, err := client.GetReview(context.Background(), "", 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// mustMarshal marshals v to json.RawMessage, panicking on error.
func mustMarshal(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return json.RawMessage(b)
}
