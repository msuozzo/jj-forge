package forge

import (
	"regexp"
	"strings"

	"github.com/msuozzo/jj-forge/internal/jj"
)

// ParentTrailerKey is the trailer key for tracking parent changes in the forge workflow.
const ParentTrailerKey = "forge-parent"

// trailerRegex matches valid trailer lines: "Key: Value"
// Keys must be alphanumeric with hyphens only (matching jj and git conventions).
// This is a copy of the regex from jj package for internal use.
var trailerRegex = regexp.MustCompile(`^([a-zA-Z0-9-]+) *: *(.*)$`)

// splitDescriptionAndTrailers splits a description into body and trailer parts.
// Returns (body, trailers, hasTrailers). If no trailers found, returns (description trimmed, nil, false).
func splitDescriptionAndTrailers(description string) (string, []jj.Trailer, bool) {
	trailers := jj.ParseDescriptionTrailers(description)
	if len(trailers) == 0 {
		// No trailers found, return the description trimmed of trailing whitespace
		return strings.TrimRight(description, " \t\n\r"), nil, false
	}

	// Find where the trailer block starts by reverse-scanning
	trimmed := strings.TrimRight(description, " \t\n\r")
	if trimmed == "" {
		return "", trailers, true
	}

	lines := strings.Split(trimmed, "\n")

	// Count trailer lines from the end (including multiline continuations)
	trailerLineCount := 0
	inTrailer := false
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if trailerRegex.MatchString(line) {
			inTrailer = true
			trailerLineCount++
		} else if inTrailer && strings.HasPrefix(line, " ") {
			// Continuation line
			trailerLineCount++
		} else if strings.TrimSpace(line) == "" && inTrailer {
			// Blank line before trailers
			break
		} else if inTrailer {
			// End of trailer block
			break
		}
	}

	// Split at the trailer boundary
	bodyLineCount := len(lines) - trailerLineCount
	if bodyLineCount < 0 {
		bodyLineCount = 0
	}

	bodyLines := lines[:bodyLineCount]
	body := strings.TrimRight(strings.Join(bodyLines, "\n"), " \t\n\r")

	return body, trailers, true
}

// UpdateParentTrailer adds or updates the forge-parent trailer in the description.
// It ensures that the trailer is placed in the trailer block at the end of the description.
func UpdateParentTrailer(description, parentID string) string {
	body, trailers, hasTrailers := splitDescriptionAndTrailers(description)

	// Use SetTrailer to add or update the forge-parent trailer
	newTrailers := jj.SetTrailer(trailers, ParentTrailerKey, parentID)

	// Reconstruct the description
	if body == "" && !hasTrailers {
		// Empty description case
		return jj.FormatTrailer(jj.Trailer{Key: ParentTrailerKey, Value: parentID}) + "\n"
	}

	if body == "" {
		// Only trailers, no body
		return jj.FormatTrailers(newTrailers) + "\n"
	}

	// Body + blank line + trailers
	return body + "\n\n" + jj.FormatTrailers(newTrailers) + "\n"
}

// RemoveParentTrailer removes the forge-parent trailer from the description.
func RemoveParentTrailer(description string) string {
	body, trailers, hasTrailers := splitDescriptionAndTrailers(description)

	if !hasTrailers {
		// No trailers found, return as-is
		return description
	}

	// Remove forge-parent trailers
	newTrailers := jj.RemoveTrailer(trailers, ParentTrailerKey)

	// Reconstruct the description
	if len(newTrailers) == 0 {
		// No trailers left, just return the body
		if body == "" {
			return "\n"
		}
		return body + "\n"
	}

	if body == "" {
		// Only trailers, no body
		return jj.FormatTrailers(newTrailers) + "\n"
	}

	// Body + blank line + trailers
	return body + "\n\n" + jj.FormatTrailers(newTrailers) + "\n"
}
