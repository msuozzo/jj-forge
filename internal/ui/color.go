package ui

import (
	"fmt"
	"strings"
)

// Color represents a named ANSI terminal color.
type Color int

const (
	Default Color = iota
	Black
	Red
	Green
	Yellow
	Blue
	Magenta
	Cyan
	White
	BrightBlack
	BrightRed
	BrightGreen
	BrightYellow
	BrightBlue
	BrightMagenta
	BrightCyan
	BrightWhite
)

// Style describes text styling with foreground color and attributes.
type Style struct {
	Fg        Color
	Bold      bool
	Dim       bool
	Underline bool
}

// ansiCode returns the ANSI SGR foreground code for the color.
// Returns -1 for Default (no foreground override).
func (c Color) ansiCode() int {
	switch {
	case c == Default:
		return -1
	case c >= Black && c <= White:
		return 30 + int(c-Black)
	case c >= BrightBlack && c <= BrightWhite:
		return 90 + int(c-BrightBlack)
	default:
		return -1
	}
}

// Start returns the ANSI SGR start sequence for this style.
// Returns an empty string if the style is zero-valued.
func (s Style) Start() string {
	var codes []string
	if s.Bold {
		codes = append(codes, "1")
	}
	if s.Dim {
		codes = append(codes, "2")
	}
	if s.Underline {
		codes = append(codes, "4")
	}
	if code := s.Fg.ansiCode(); code >= 0 {
		codes = append(codes, fmt.Sprintf("%d", code))
	}
	if len(codes) == 0 {
		return ""
	}
	return fmt.Sprintf("\x1b[%sm", strings.Join(codes, ";"))
}

// Reset returns the ANSI SGR reset sequence.
func Reset() string {
	return "\x1b[0m"
}

// Wrap returns text wrapped in ANSI SGR codes for this style.
// If the style is zero-valued, returns text unchanged.
func (s Style) Wrap(text string) string {
	start := s.Start()
	if start == "" {
		return text
	}
	return start + text + Reset()
}
