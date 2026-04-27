package ui

import "math"

// LevelMeter returns a single rune showing fraction (in [0, 1]) as a vertical
// fill level using the unicode block characters ▁ ▂ ▃ ▄ ▅ ▆ ▇ █.
//
// A fraction of 0 (or negative, or NaN) returns a space, so an empty meter is
// visually distinct from "just started". Each block represents the upper bound
// of its bucket: ▁ covers (0, 1/8], ▂ covers (1/8, 2/8], and so on, so the
// meter only reaches █ at fraction == 1 (or above, which is clamped).
func LevelMeter(fraction float64) rune {
	if math.IsNaN(fraction) || fraction <= 0 {
		return ' '
	}
	if fraction > 1 {
		fraction = 1
	}
	levels := [...]rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	idx := max(int(math.Ceil(fraction*8))-1, 0)
	return levels[idx]
}
