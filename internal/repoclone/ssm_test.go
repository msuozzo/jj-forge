package repoclone

import (
	"context"
	"strings"
	"testing"
)

func TestSSMRunner_InvalidURLFormat(t *testing.T) {
	runner := NewSSMRunnerWithDeps(nil, &FakePrinter{})
	_, err := runner.Run(context.Background(), Params{
		URL: "https://inst-897099121057.us-central1.sourcemanager.dev/ssci-demos/repo",
	})
	if err == nil {
		t.Fatal("expected error for inst- URL, got nil")
	}
	if !strings.Contains(err.Error(), "not in a git-compatible format") {
		t.Errorf("expected helpful error message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "-git or -ssh subdomain") {
		t.Errorf("expected error to mention -git or -ssh subdomain, got: %v", err)
	}
}
