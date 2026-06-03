package forge

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

type mockHTTPDoer struct {
	resp *http.Response
	err  error
}

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	return m.resp, m.err
}

func TestDetectForgeByHeaders(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		err      error
		wantType ForgeType
		wantErr  bool
	}{
		{
			name:     "github detected via x-github-request-id",
			headers:  http.Header{"X-Github-Request-Id": []string{"abc123"}},
			wantType: ForgeTypeGitHub,
		},
		{
			name:     "gitlab detected via x-gitlab-meta",
			headers:  http.Header{"X-Gitlab-Meta": []string{"some-meta"}},
			wantType: ForgeTypeGitLab,
		},
		{
			name:     "unknown when no recognized headers",
			headers:  http.Header{"X-Custom": []string{"value"}},
			wantType: ForgeTypeUnknown,
		},
		{
			name:     "github takes precedence when both headers present",
			headers:  http.Header{"X-Github-Request-Id": []string{"abc"}, "X-Gitlab-Meta": []string{"def"}},
			wantType: ForgeTypeGitHub,
		},
		{
			name:    "error on HTTP failure",
			err:     errors.New("connection refused"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doer := &mockHTTPDoer{
				resp: &http.Response{
					StatusCode: 200,
					Header:     tt.headers,
					Body:       http.NoBody,
				},
				err: tt.err,
			}
			if tt.err != nil {
				doer.resp = nil
			}
			got, err := DetectForgeByHeaders(context.Background(), "example.com", doer)
			if (err != nil) != tt.wantErr {
				t.Errorf("DetectForgeByHeaders() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantType {
				t.Errorf("DetectForgeByHeaders() = %v, want %v", got, tt.wantType)
			}
		})
	}
}

// panicHTTPDoer fails the test if an HTTP call is made, proving fast paths work.
type panicHTTPDoer struct{ t *testing.T }

func (p *panicHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	p.t.Fatal("unexpected HTTP call")
	return nil, nil
}

func TestDetectForge(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		headers  http.Header
		httpErr  error
		noHTTP   bool // if true, use panicHTTPDoer
		hosts    map[string]string
		wantType ForgeType
		wantErr  bool
	}{
		{
			name:     "SSM URL",
			url:      "https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo.git",
			noHTTP:   true,
			wantType: ForgeTypeSSM,
		},
		{
			name:     "SSM inst- URL (non-git subdomain)",
			url:      "https://inst-897099121057.us-central1.sourcemanager.dev/ssci-demos/repo",
			noHTTP:   true,
			wantType: ForgeTypeSSM,
		},
		{
			name:     "github.com SSH",
			url:      "git@github.com:owner/repo.git",
			noHTTP:   true,
			wantType: ForgeTypeGitHub,
		},
		{
			name:     "github.com HTTPS",
			url:      "https://github.com/owner/repo.git",
			noHTTP:   true,
			wantType: ForgeTypeGitHub,
		},
		{
			name:     "GHES via HTTP headers",
			url:      "git@github.example.com:owner/repo.git",
			headers:  http.Header{"X-Github-Request-Id": []string{"abc123"}},
			wantType: ForgeTypeGitHub,
		},
		{
			name:     "GitLab via HTTP headers",
			url:      "git@gitlab.example.com:owner/repo.git",
			headers:  http.Header{"X-Gitlab-Meta": []string{"some-meta"}},
			wantType: ForgeTypeGitLab,
		},
		{
			name:     "unknown host with no recognized headers",
			url:      "git@unknown.example.com:owner/repo.git",
			headers:  http.Header{},
			wantType: ForgeTypeUnknown,
		},
		{
			name:    "invalid URL",
			url:     "not-a-url",
			noHTTP:  true,
			wantErr: true,
		},
		{
			name:    "HTTP probe error",
			url:     "git@ghes.example.com:owner/repo.git",
			httpErr: errors.New("connection refused"),
			wantErr: true,
		},
		{
			name:     "override github.example.com to github via hosts map",
			url:      "https://github.example.com/owner/repo",
			noHTTP:   true,
			hosts:    map[string]string{"github.example.com": "github"},
			wantType: ForgeTypeGitHub,
		},
		{
			name:     "override github.com to gitlab via hosts map",
			url:      "https://github.com/owner/repo",
			noHTTP:   true,
			hosts:    map[string]string{"github.com": "gitlab"},
			wantType: ForgeTypeGitLab,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doer HTTPDoer
			if tt.noHTTP {
				doer = &panicHTTPDoer{t: t}
			} else {
				m := &mockHTTPDoer{
					resp: &http.Response{
						StatusCode: 200,
						Header:     tt.headers,
						Body:       http.NoBody,
					},
					err: tt.httpErr,
				}
				if tt.httpErr != nil {
					m.resp = nil
				}
				doer = m
			}

			got, err := DetectForge(context.Background(), tt.url, doer, tt.hosts)
			if (err != nil) != tt.wantErr {
				t.Errorf("DetectForge() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantType {
				t.Errorf("DetectForge() = %v, want %v", got, tt.wantType)
			}
		})
	}
}

func TestDetectForgeByHeaders_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DetectForgeByHeaders(ctx, "example.com", DefaultHTTPClient())
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
