package ui

import (
	"io"
	"os"

	"golang.org/x/term"
)

// ColorMode controls when ANSI color codes are emitted.
type ColorMode int

const (
	ColorAuto   ColorMode = iota // Detect from terminal and NO_COLOR
	ColorAlways                  // Always emit color codes
	ColorNever                   // Never emit color codes
)

// UI provides styled text output for terminal display.
type UI struct {
	w     io.Writer
	color bool
}

// New creates a new UI that writes to w. ColorAuto is resolved by checking
// whether w is a terminal and whether NO_COLOR is set.
func New(w io.Writer, mode ColorMode) *UI {
	var color bool
	switch mode {
	case ColorAlways:
		color = true
	case ColorNever:
		color = false
	case ColorAuto:
		color = isColorTerminal(w)
	}
	return &UI{w: w, color: color}
}

// Write implements io.Writer as a plain pass-through.
func (u *UI) Write(p []byte) (int, error) {
	return u.w.Write(p)
}

// Styled returns text wrapped in ANSI codes for the given label.
// If color is disabled or the label is unknown, returns text unchanged.
func (u *UI) Styled(label, text string) string {
	if !u.color {
		return text
	}
	s, ok := Styles[label]
	if !ok {
		return text
	}
	return s.Wrap(text)
}

// Style returns the Style for the given label. Returns a zero Style if the
// label is unknown.
func (u *UI) Style(label string) Style {
	return Styles[label]
}

// IsColor reports whether color output is enabled.
func (u *UI) IsColor() bool {
	return u.color
}

// isColorTerminal reports whether w is a terminal that supports color.
func isColorTerminal(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}
