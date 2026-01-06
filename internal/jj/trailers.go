package jj

import (
	"fmt"
	"regexp"
	"strings"
)

// Trailer represents a git trailer (key-value pair in commit messages).
// Trailers are structured metadata appended to commit messages, like:
//
//	Co-authored-by: Name <email@example.com>
//	Signed-off-by: Name <email@example.com>
type Trailer struct {
	Key   string // Trailer key (e.g., "Signed-off-by")
	Value string // Trailer value, may contain newlines for multiline values
}

// TrailerErrorKind represents the kind of trailer parsing error.
type TrailerErrorKind int

const (
	// TrailerErrorBlankLine indicates a blank line was found in a trailer block.
	TrailerErrorBlankLine TrailerErrorKind = iota
	// TrailerErrorNonTrailerLine indicates a non-trailer line was found in a trailer block.
	TrailerErrorNonTrailerLine
)

// TrailerParseError represents an error that occurred while parsing trailers.
type TrailerParseError struct {
	Kind    TrailerErrorKind
	Message string
}

func (e *TrailerParseError) Error() string {
	return e.Message
}

// trailerRegex matches valid trailer lines: "Key: Value"
// Keys must be alphanumeric with hyphens only (matching jj and git conventions).
var trailerRegex = regexp.MustCompile(`^([a-zA-Z0-9-]+) *: *(.*)$`)

func isTrailer(line string) bool {
	// Simple heuristic: "Key: Value"
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return false
	}
	key := strings.TrimSpace(parts[0])
	// Keys don't usually have spaces
	return key != "" && !strings.Contains(key, " ")
}

// isGitTrailer returns true if the line is a recognized git trailer.
// Git trailers bypass the requirement for a blank line before trailers.
func isGitTrailer(line string) bool {
	// Check for cherry-pick line first (not a standard trailer format)
	if strings.HasPrefix(line, "(cherry picked from commit ") {
		return true
	}

	// Check for Signed-off-by (case-insensitive)
	matches := trailerRegex.FindStringSubmatch(line)
	if len(matches) > 0 {
		key := strings.ToLower(matches[1])
		return key == "signed-off-by"
	}

	return false
}

// parseTrailersImpl is the core parsing implementation that parses trailers in reverse.
// It returns:
//   - trailers: parsed trailer list (in original order)
//   - foundBlank: whether a blank line was encountered
//   - foundGitTrailer: whether a git trailer (Signed-off-by, cherry-pick) was found
//   - nonTrailerLine: the first non-trailer line encountered (if any)
func parseTrailersImpl(body string) ([]Trailer, bool, bool, string) {
	// Trim trailing whitespace and split into lines
	trimmed := strings.TrimRight(body, " \t\n\r")
	if trimmed == "" {
		return nil, false, false, ""
	}

	lines := strings.Split(trimmed, "\n")

	// Parse in reverse order (from end of message)
	var trailers []Trailer
	var multilineValue []string
	var foundBlank bool
	var foundGitTrailer bool
	var nonTrailerLine string

	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]

		if strings.HasPrefix(line, " ") {
			// Continuation line for multiline trailer value
			multilineValue = append(multilineValue, line)
		} else if matches := trailerRegex.FindStringSubmatch(line); matches != nil {
			// Valid trailer line
			key := matches[1]
			valueStart := matches[2]

			// Prepend the initial value part, then all continuation lines (reversed)
			multilineValue = append(multilineValue, valueStart)

			// Trim the end of the multiline value (the first continuation line)
			// The start is already trimmed with the regex
			if len(multilineValue) > 0 {
				multilineValue[0] = strings.TrimRight(multilineValue[0], " \t")
			}

			// Reverse the multiline value to get correct order
			for left, right := 0, len(multilineValue)-1; left < right; left, right = left+1, right-1 {
				multilineValue[left], multilineValue[right] = multilineValue[right], multilineValue[left]
			}

			value := strings.Join(multilineValue, "\n")
			multilineValue = nil // Reset for next trailer

			// Check if this is a git trailer
			if strings.ToLower(key) == "signed-off-by" {
				foundGitTrailer = true
			}

			trailers = append(trailers, Trailer{Key: key, Value: value})
		} else if strings.HasPrefix(line, "(cherry picked from commit ") {
			// Cherry-pick line is treated as a git trailer but not parsed
			foundGitTrailer = true
			nonTrailerLine = line
			multilineValue = nil // Ignore any accumulated continuation lines
		} else if strings.TrimSpace(line) == "" {
			// Blank line marks the end of trailer block
			foundBlank = true
			break
		} else {
			// Non-trailer line
			// Ignore this line and any accumulated continuation lines
			multilineValue = nil
			nonTrailerLine = line
		}
	}

	// Reverse trailers to restore original order
	for left, right := 0, len(trailers)-1; left < right; left, right = left+1, right-1 {
		trailers[left], trailers[right] = trailers[right], trailers[left]
	}

	return trailers, foundBlank, foundGitTrailer, nonTrailerLine
}

// ParseDescriptionTrailers parses trailers from a full commit description.
// It requires either a blank line before the trailers or the presence of
// recognized git trailers (Signed-off-by, cherry-pick lines).
// Returns an empty slice if no valid trailers are found.
func ParseDescriptionTrailers(description string) []Trailer {
	trailers, foundBlank, foundGitTrailer, nonTrailerLine := parseTrailersImpl(description)

	if !foundBlank {
		// No blank line found, meaning single paragraph
		// Can't be a trailer block
		return nil
	}

	if nonTrailerLine != "" && !foundGitTrailer {
		// At least one non-trailer line in the trailer paragraph
		// Only considered valid if there's a git trailer
		return nil
	}

	return trailers
}

// ParseTrailers parses trailers from trailer-only text (strict validation).
// Returns an error if a blank line or non-trailer line is found.
// This function is useful when the input is expected to contain only trailers.
func ParseTrailers(text string) ([]Trailer, error) {
	trailers, foundBlank, _, nonTrailerLine := parseTrailersImpl(text)

	if foundBlank {
		return nil, &TrailerParseError{
			Kind:    TrailerErrorBlankLine,
			Message: "The trailer paragraph can't contain a blank line",
		}
	}

	if nonTrailerLine != "" {
		return nil, &TrailerParseError{
			Kind:    TrailerErrorNonTrailerLine,
			Message: fmt.Sprintf("Invalid trailer line: %s", nonTrailerLine),
		}
	}

	return trailers, nil
}

// FormatTrailer formats a single trailer as "Key: Value".
// Multiline values are preserved with their continuation lines.
func FormatTrailer(t Trailer) string {
	return fmt.Sprintf("%s: %s", t.Key, t.Value)
}

// FormatTrailers formats a slice of trailers as a trailer block.
// Returns the formatted text without leading or trailing newlines.
func FormatTrailers(trailers []Trailer) string {
	if len(trailers) == 0 {
		return ""
	}

	var lines []string
	for _, t := range trailers {
		lines = append(lines, FormatTrailer(t))
	}
	return strings.Join(lines, "\n")
}

// GetTrailer returns the first trailer matching the given key (case-insensitive).
// Returns (nil, false) if no matching trailer is found.
func GetTrailer(trailers []Trailer, key string) (*Trailer, bool) {
	lowerKey := strings.ToLower(key)
	for i := range trailers {
		if strings.ToLower(trailers[i].Key) == lowerKey {
			return &trailers[i], true
		}
	}
	return nil, false
}

// GetAllTrailers returns all trailers matching the given key (case-insensitive).
// Returns an empty slice if no matching trailers are found.
func GetAllTrailers(trailers []Trailer, key string) []Trailer {
	lowerKey := strings.ToLower(key)
	var result []Trailer
	for _, t := range trailers {
		if strings.ToLower(t.Key) == lowerKey {
			result = append(result, t)
		}
	}
	return result
}

// SetTrailer replaces the first matching trailer or appends if not found.
// Key matching is case-insensitive. Returns a new slice.
func SetTrailer(trailers []Trailer, key, value string) []Trailer {
	lowerKey := strings.ToLower(key)
	result := make([]Trailer, len(trailers))
	copy(result, trailers)

	for i := range result {
		if strings.ToLower(result[i].Key) == lowerKey {
			result[i] = Trailer{Key: key, Value: value}
			return result
		}
	}

	// Not found, append
	return append(result, Trailer{Key: key, Value: value})
}

// AddTrailer appends a new trailer to the slice (allows duplicates).
// Returns a new slice with the trailer added.
func AddTrailer(trailers []Trailer, key, value string) []Trailer {
	result := make([]Trailer, len(trailers), len(trailers)+1)
	copy(result, trailers)
	return append(result, Trailer{Key: key, Value: value})
}

// RemoveTrailer removes all trailers matching the given key (case-insensitive).
// Returns a new slice with matching trailers filtered out.
func RemoveTrailer(trailers []Trailer, key string) []Trailer {
	lowerKey := strings.ToLower(key)
	var result []Trailer
	for _, t := range trailers {
		if strings.ToLower(t.Key) != lowerKey {
			result = append(result, t)
		}
	}
	return result
}
