package actionsstatus

import (
	"fmt"
	"regexp"
	"strings"
)

// filterPattern is one compiled entry of a branches or paths filter.
type filterPattern struct {
	negative bool // The pattern started with "!"
	re       *regexp.Regexp
}

// compileFilter compiles a workflow filter list, such as the value of
// on.pull_request.paths, in order.
func compileFilter(patterns []string) ([]filterPattern, error) {
	var out []filterPattern
	for _, p := range patterns {
		if p == "" || p == "!" {
			return nil, fmt.Errorf("empty filter pattern")
		}
		negative := strings.HasPrefix(p, "!")
		re, err := filterRegexp(strings.TrimPrefix(p, "!"))
		if err != nil {
			return nil, err
		}
		out = append(out, filterPattern{negative: negative, re: re})
	}
	return out, nil
}

// matches reports whether name is selected by the filter. As in GitHub, the
// last pattern that matches decides, so a later "!pattern" excludes a name
// that an earlier pattern included.
func matches(filter []filterPattern, name string) bool {
	selected := false
	for _, p := range filter {
		if p.re.MatchString(name) {
			selected = !p.negative
		}
	}
	return selected
}

// filterRegexp compiles a GitHub filter pattern, as described in the "Filter
// pattern cheat sheet" of the Actions workflow syntax docs:
//
//   - "*" matches any run of characters except "/"
//   - "**" matches any run of characters, and a leading "**/" also matches
//     no directory at all
//   - "?" and "+" match zero or one, and one or more, of the preceding
//     character
//   - "[...]" matches one listed character or range of letters or digits
//   - "\" makes the next character literal
func filterRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			switch {
			case strings.HasPrefix(pattern[i:], "**/"):
				b.WriteString("(?:.*/)?")
				i += 2
			case strings.HasPrefix(pattern[i:], "**"):
				b.WriteString(".*")
				i++
			default:
				b.WriteString("[^/]*")
			}
		case '?', '+':
			if i == 0 {
				b.WriteString(regexp.QuoteMeta(string(c))) // Nothing to repeat.
			} else {
				b.WriteByte(c)
			}
		case '[':
			end := strings.IndexByte(pattern[i+1:], ']')
			if end < 0 || !classRE.MatchString(pattern[i+1:i+1+end]) {
				return nil, fmt.Errorf("invalid character class in filter pattern %q", pattern)
			}
			b.WriteString(pattern[i : i+end+2])
			i += end + 1
		case '\\':
			if i+1 == len(pattern) {
				return nil, fmt.Errorf("filter pattern %q ends in a backslash", pattern)
			}
			i++
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		default:
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// classRE matches the inside of a "[...]" that GitHub accepts: letters,
// digits and ranges between them.
var classRE = regexp.MustCompile(`^([a-zA-Z0-9](-[a-zA-Z0-9])?)+$`)
