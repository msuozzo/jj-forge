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

// linkDisplayURL rewrites GitHub PR URLs to use redirect.github.com so that
// links in PR descriptions don't create "mention" notifications on the linked
// PRs. GitHub treats redirect.github.com links as valid redirects to the real
// PR but does not generate cross-reference events for them.
func linkDisplayURL(url string) string {
	return strings.Replace(url, "https://github.com/", "https://redirect.github.com/", 1)
}

// FormatPRLinks renders a links section for PR descriptions.
// Returns "" if both parents and children are empty.
func FormatPRLinks(parents, children []PRLink) string {
	if len(parents) == 0 && len(children) == 0 {
		return ""
	}
	var lines []string
	if len(parents) > 0 {
		var refs []string
		for _, p := range parents {
			refs = append(refs, fmt.Sprintf("[#%d](%s)", p.Number, linkDisplayURL(p.URL)))
		}
		lines = append(lines, "> Parents: "+strings.Join(refs, ", "))
	}
	if len(children) > 0 {
		var refs []string
		for _, c := range children {
			refs = append(refs, fmt.Sprintf("[#%d](%s)", c.Number, linkDisplayURL(c.URL)))
		}
		lines = append(lines, "> Children: "+strings.Join(refs, ", "))
	}
	return strings.Join(lines, "\n")
}

// StripPRLinks removes a managed links section from the end of a PR body.
// A links section is identified by a trailing block of blockquote lines
// containing only "> Parents:" and/or "> Children:" entries.
func StripPRLinks(body string) string {
	trimmed := strings.TrimRight(body, "\n\r\t ")
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")

	// Scan backwards looking for a links block.
	i := len(lines) - 1
	foundLink := false
	for i >= 0 {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "> Parents: ") || strings.HasPrefix(line, "> Children: ") {
			i--
			foundLink = true
			continue
		}
		break
	}
	if foundLink {
		result := strings.TrimRight(strings.Join(lines[:i+1], "\n"), "\n\r\t ")
		return result
	}
	// No links section found, return original (trimmed).
	return trimmed
}

// SetPRLinks strips any existing links section and appends new links.
// Returns the original body unchanged if no links added or removed.
func SetPRLinks(body string, parents, children []PRLink) string {
	stripped := StripPRLinks(body)
	linksSection := FormatPRLinks(parents, children)
	var result string
	if linksSection == "" {
		result = stripped
	} else if stripped == "" {
		result = linksSection
	} else {
		result = stripped + "\n\n" + linksSection
	}
	if strings.TrimRight(result, "\n\r\t ") == strings.TrimRight(body, "\n\r\t ") {
		return body
	}
	return result
}
