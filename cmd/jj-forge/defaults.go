package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/msuozzo/jj-forge/internal/jj"
)

// resolveDefaultRev determines the default revision when the user doesn't
// specify one. If @ has a description, it returns "@". If @ is anonymous
// (no description) and empty, it returns "@-". If @ is anonymous but has
// file changes, it returns an error prompting the user to describe or
// specify explicitly.
func resolveDefaultRev(ctx context.Context, client jj.Client) (string, error) {
	rev, err := client.Rev(ctx, "@")
	if err != nil {
		return "", fmt.Errorf("failed to resolve working copy: %w", err)
	}
	if strings.TrimSpace(rev.Description) != "" {
		return "@", nil
	}
	if !rev.IsEmpty {
		return "", fmt.Errorf("working copy has uncommitted changes without a description; run 'jj describe' or pass an explicit revision")
	}
	return "@-", nil
}
