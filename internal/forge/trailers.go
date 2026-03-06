package forge

import (
	"strings"

	"github.com/msuozzo/jj-forge/internal/jj"
)

// ParentTrailerKey is the trailer key for tracking parent changes in the forge workflow.
const ParentTrailerKey = "forge-parent"

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
		if jj.TrailerRegex.MatchString(line) {
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

// UpdateParentTrailers replaces all forge-parent trailers in the description
// with trailers for the given parent IDs.
func UpdateParentTrailers(description string, parentIDs []string) string {
	body, trailers, hasTrailers := splitDescriptionAndTrailers(description)

	// Remove all existing forge-parent trailers, then add one per parent.
	newTrailers := jj.RemoveTrailer(trailers, ParentTrailerKey)
	for _, id := range parentIDs {
		newTrailers = jj.AddTrailer(newTrailers, ParentTrailerKey, id)
	}

	// Reconstruct the description
	if body == "" && !hasTrailers {
		// Empty description case
		return jj.FormatTrailers(newTrailers) + "\n"
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
