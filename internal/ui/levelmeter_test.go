package ui

import (
	"math"
	"testing"
)

func TestLevelMeter(t *testing.T) {
	tests := []struct {
		name     string
		fraction float64
		want     rune
	}{
		{"zero", 0, ' '},
		{"negative", -0.5, ' '},
		{"nan", math.NaN(), ' '},
		{"tiny positive", 0.001, '▁'},
		{"upper edge of 1/8", 0.125, '▁'},
		{"just past 1/8", 0.126, '▂'},
		{"2/8", 0.25, '▂'},
		{"3/8", 0.375, '▃'},
		{"half", 0.5, '▄'},
		{"5/8", 0.625, '▅'},
		{"6/8", 0.75, '▆'},
		{"7/8", 0.875, '▇'},
		{"just past 7/8", 0.876, '█'},
		{"one", 1.0, '█'},
		{"clamped above one", 1.5, '█'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LevelMeter(tt.fraction)
			if got != tt.want {
				t.Errorf("LevelMeter(%v) = %q, want %q", tt.fraction, got, tt.want)
			}
		})
	}
}
