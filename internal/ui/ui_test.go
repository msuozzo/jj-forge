package ui

import (
	"bytes"
	"testing"
)

func TestUI_Styled_ColorEnabled(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorAlways)

	got := u.Styled("error_heading", "Error:")
	want := "\x1b[1;31mError:\x1b[0m"
	if got != want {
		t.Errorf("Styled(error_heading) = %q, want %q", got, want)
	}
}

func TestUI_Styled_ColorDisabled(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorNever)

	got := u.Styled("error_heading", "Error:")
	want := "Error:"
	if got != want {
		t.Errorf("Styled(error_heading) = %q, want %q", got, want)
	}
}

func TestUI_Styled_UnknownLabel(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorAlways)

	got := u.Styled("nonexistent_label", "text")
	want := "text"
	if got != want {
		t.Errorf("Styled(nonexistent) = %q, want %q", got, want)
	}
}

func TestUI_IsColor(t *testing.T) {
	var buf bytes.Buffer

	always := New(&buf, ColorAlways)
	if !always.IsColor() {
		t.Error("ColorAlways: IsColor() = false, want true")
	}

	never := New(&buf, ColorNever)
	if never.IsColor() {
		t.Error("ColorNever: IsColor() = true, want false")
	}

	// ColorAuto with non-terminal writer should be false
	auto := New(&buf, ColorAuto)
	if auto.IsColor() {
		t.Error("ColorAuto with bytes.Buffer: IsColor() = true, want false")
	}
}

func TestUI_Write(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorNever)

	n, err := u.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 5 {
		t.Errorf("Write() = %d, want 5", n)
	}
	if buf.String() != "hello" {
		t.Errorf("buffer = %q, want %q", buf.String(), "hello")
	}
}

func TestUI_Style(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorAlways)

	s := u.Style("error_heading")
	if s.Fg != Red || !s.Bold {
		t.Errorf("Style(error_heading) = %+v, want Red Bold", s)
	}

	s = u.Style("nonexistent")
	if s != (Style{}) {
		t.Errorf("Style(nonexistent) = %+v, want zero Style", s)
	}
}
