package review

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/msuozzo/jj-forge/internal/jj"
)

// fullCommitID expands a jj.Rev's abbreviated commit ID for forges, which
// compare commits by full ID.
func fullCommitID(ctx context.Context, jjClient jj.Client, id string) (string, error) {
	result, err := jjClient.Run(ctx, "log", "--no-graph", "-r", id, "-T", "commit_id")
	if err != nil {
		return "", fmt.Errorf("failed to resolve commit %s: %w", id, err)
	}
	return strings.TrimSpace(result.Stdout), nil
}

// isUploaded checks if a change has been pushed to the remote.
// It verifies that the remote bookmark {remote}/push-{changeID} exists.
func isUploaded(rev *jj.Rev, remote string) bool {
	expectedBookmark := fmt.Sprintf("%s/push-%s", remote, rev.ID)
	return slices.Contains(rev.RemoteBookmarks, expectedBookmark)
}

// splitTitleBody splits a commit description into title and body.
// The title is the first line, and the body is everything after that.
func splitTitleBody(description string) (title, body string) {
	lines := strings.Split(strings.TrimSpace(description), "\n")
	if len(lines) == 0 {
		return "", ""
	}
	title = strings.TrimSpace(lines[0])
	if len(lines) > 1 {
		// Join remaining lines and trim leading/trailing whitespace
		body = strings.TrimSpace(strings.Join(lines[1:], "\n"))
	}
	return title, body
}
