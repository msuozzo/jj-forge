package forge

import (
	"context"
	"fmt"
	"strings"
)

// ForgeType represents the type of forge hosting a repository.
type ForgeType int

const (
	ForgeTypeUnknown ForgeType = iota
	ForgeTypeGitHub
	ForgeTypeSSM
)

// DetectForge determines the forge type from a git remote URL.
// It parses the URL once, then checks for SSM and github.com.
func DetectForge(ctx context.Context, url string) (ForgeType, error) {
	info, err := ParseGitURL(url)
	if err != nil {
		return ForgeTypeUnknown, fmt.Errorf("could not parse URL: %w", err)
	}
	if strings.HasSuffix(info.Host, ".sourcemanager.dev") {
		return ForgeTypeSSM, nil
	}
	if info.Host == "github.com" {
		return ForgeTypeGitHub, nil
	}
	return ForgeTypeUnknown, nil
}
