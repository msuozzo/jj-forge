package ui

import "testing"

func TestColor_ANSICode(t *testing.T) {
	tests := []struct {
		color Color
		want  int
	}{
		{Default, -1},
		{Black, 30},
		{Red, 31},
		{Green, 32},
		{Yellow, 33},
		{Blue, 34},
		{Magenta, 35},
		{Cyan, 36},
		{White, 37},
		{BrightBlack, 90},
		{BrightRed, 91},
		{BrightGreen, 92},
		{BrightYellow, 93},
		{BrightBlue, 94},
		{BrightMagenta, 95},
		{BrightCyan, 96},
		{BrightWhite, 97},
	}
	for _, tt := range tests {
		if got := tt.color.ansiCode(); got != tt.want {
			t.Errorf("Color(%d).ansiCode() = %d, want %d", tt.color, got, tt.want)
		}
	}
}

func TestStyle_Start(t *testing.T) {
	tests := []struct {
		name  string
		style Style
		want  string
	}{
		{"zero style", Style{}, ""},
		{"bold only", Style{Bold: true}, "\x1b[1m"},
		{"dim only", Style{Dim: true}, "\x1b[2m"},
		{"underline only", Style{Underline: true}, "\x1b[4m"},
		{"red fg", Style{Fg: Red}, "\x1b[31m"},
		{"bold red", Style{Fg: Red, Bold: true}, "\x1b[1;31m"},
		{"bold blue", Style{Fg: Blue, Bold: true}, "\x1b[1;34m"},
		{"bright magenta", Style{Fg: BrightMagenta}, "\x1b[95m"},
		{"bold dim underline cyan", Style{Fg: Cyan, Bold: true, Dim: true, Underline: true}, "\x1b[1;2;4;36m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.style.Start(); got != tt.want {
				t.Errorf("Style.Start() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStyle_Wrap(t *testing.T) {
	tests := []struct {
		name  string
		style Style
		text  string
		want  string
	}{
		{"zero style passes through", Style{}, "hello", "hello"},
		{"bold wraps", Style{Bold: true}, "hello", "\x1b[1mhello\x1b[0m"},
		{"red bold wraps", Style{Fg: Red, Bold: true}, "Error:", "\x1b[1;31mError:\x1b[0m"},
		{"green wraps", Style{Fg: Green}, "<VALUE>", "\x1b[32m<VALUE>\x1b[0m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.style.Wrap(tt.text); got != tt.want {
				t.Errorf("Style.Wrap(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestReset(t *testing.T) {
	if got := Reset(); got != "\x1b[0m" {
		t.Errorf("Reset() = %q, want %q", got, "\x1b[0m")
	}
}
