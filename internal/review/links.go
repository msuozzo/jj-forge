package review

import (
	"fmt"
	"strings"
)

// PRLink represents a link to a related pull request.
type PRLink struct {
	Number int
	URL    string
}

// FormatPRLinks renders a links section for PR descriptions.
// Returns "" if both parents and children are empty.
func FormatPRLinks(parents, children []PRLink) string {
	if len(parents) == 0 && len(children) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "---")
	if len(parents) > 0 {
		var refs []string
		for _, p := range parents {
			refs = append(refs, fmt.Sprintf("[#%d](%s)", p.Number, p.URL))
		}
		lines = append(lines, "Parents: "+strings.Join(refs, ", "))
	}
	if len(children) > 0 {
		var refs []string
		for _, c := range children {
			refs = append(refs, fmt.Sprintf("[#%d](%s)", c.Number, c.URL))
		}
		lines = append(lines, "Children: "+strings.Join(refs, ", "))
	}
	return strings.Join(lines, "\n")
}

// StripPRLinks removes a managed links section from the end of a PR body.
// A links section is identified by a trailing block that starts with "---"
// and contains only "Parents:" and/or "Children:" lines after it.
func StripPRLinks(body string) string {
	trimmed := strings.TrimRight(body, "\n\r\t ")
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")

	// Scan backwards looking for a links block.
	// Valid links block: optional Children: line, optional Parents: line, then "---".
	i := len(lines) - 1
	for i >= 0 {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "Parents: ") || strings.HasPrefix(line, "Children: ") {
			i--
			continue
		}
		if line == "---" {
			// Found the separator — this is the start of a links block.
			// Strip from the separator onwards, and also any trailing blank lines before it.
			result := strings.TrimRight(strings.Join(lines[:i], "\n"), "\n\r\t ")
			return result
		}
		// Not a links section line — stop scanning.
		break
	}
	// No links section found, return original (trimmed).
	return trimmed
}

// SetPRLinks strips any existing links section and appends new links.
func SetPRLinks(body string, parents, children []PRLink) string {
	stripped := StripPRLinks(body)
	linksSection := FormatPRLinks(parents, children)
	if linksSection == "" {
		return stripped
	}
	if stripped == "" {
		return linksSection
	}
	return stripped + "\n\n" + linksSection
}
