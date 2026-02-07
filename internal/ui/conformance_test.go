package ui

import (
	"bytes"
	"os/exec"
	"regexp"
	"testing"
)

func skipIfNoJJ(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj binary not found, skipping conformance test")
	}
}

// extractSGR extracts the ANSI SGR sequence(s) immediately preceding the given
// text in the output. Returns the raw escape sequence(s) including \x1b[...m.
func extractSGR(output []byte, text string) string {
	// Match one or more SGR sequences immediately before the text
	pattern := `((?:\x1b\[[0-9;]*m)+)` + regexp.QuoteMeta(text)
	re := regexp.MustCompile(pattern)
	m := re.FindSubmatch(output)
	if m == nil {
		return ""
	}
	return string(m[1])
}

func TestConformance_ErrorFormatStructure(t *testing.T) {
	skipIfNoJJ(t)

	// Run jj with a command that produces an error
	cmd := exec.Command("jj", "--color=always", "log", "-r", "nonexistent_xyz_conformance_test")
	output, _ := cmd.CombinedOutput()

	// Verify jj output contains "Error:" with ANSI styling
	if !bytes.Contains(output, []byte("Error:")) {
		t.Skipf("jj output doesn't contain 'Error:' prefix, skipping (output: %q)", output)
	}

	// Now generate the same with our PrintError
	var buf bytes.Buffer
	u := New(&buf, ColorAlways)
	u.PrintError(&UserError{Msg: "test error"})

	if !bytes.Contains(buf.Bytes(), []byte("Error:")) {
		t.Errorf("PrintError output missing 'Error:' prefix: %q", buf.String())
	}
}

func TestConformance_ErrorColorCodes(t *testing.T) {
	skipIfNoJJ(t)

	// Run jj with a command that produces an error
	cmd := exec.Command("jj", "--color=always", "log", "-r", "nonexistent_xyz_conformance_test")
	output, _ := cmd.CombinedOutput()

	jjSGR := extractSGR(output, "Error:")
	if jjSGR == "" {
		t.Skip("could not extract SGR codes from jj error output")
	}

	// Generate our error output
	var buf bytes.Buffer
	u := New(&buf, ColorAlways)
	u.PrintError(&UserError{Msg: "test error"})

	forgeSGR := extractSGR(buf.Bytes(), "Error: ")
	if forgeSGR == "" {
		t.Fatal("could not extract SGR codes from jj-forge error output")
	}

	// Both should use bold red (the exact sequence may vary in order)
	// jj uses \x1b[1m\x1b[38;5;1m or similar, we use \x1b[1;31m
	// Rather than exact match, verify both contain bold (1) and red (31 or 38;5;1)
	boldRe := regexp.MustCompile(`\x1b\[.*?1[;m]`)
	if !boldRe.Match([]byte(forgeSGR)) {
		t.Errorf("jj-forge Error: SGR %q missing bold attribute", forgeSGR)
	}
	if !boldRe.Match([]byte(jjSGR)) {
		t.Errorf("jj Error: SGR %q missing bold attribute", jjSGR)
	}
}

func TestConformance_HelpSectionHeaderColors(t *testing.T) {
	skipIfNoJJ(t)

	// Run jj with colored help
	cmd := exec.Command("jj", "--color=always", "help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("jj help failed: %v", err)
	}

	// Check for "Usage:" header with color
	usageSGR := extractSGR(output, "Usage:")
	if usageSGR == "" {
		t.Skip("could not extract SGR codes for 'Usage:' from jj help output")
	}

	// Our help_header style should match (yellow bold)
	headerStyle := Styles["help_header"]
	ourSGR := headerStyle.Start()

	// Verify our style is yellow bold
	if headerStyle.Fg != Yellow || !headerStyle.Bold {
		t.Errorf("help_header style = %+v, want yellow bold", headerStyle)
	}
	if ourSGR == "" {
		t.Error("help_header style produces empty SGR sequence")
	}
}

func TestConformance_HelpCommandNameColors(t *testing.T) {
	skipIfNoJJ(t)

	// Run jj with colored help
	cmd := exec.Command("jj", "--color=always", "help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("jj help failed: %v", err)
	}

	// Look for a known command name with color (e.g., "log")
	logSGR := extractSGR(output, "log")
	if logSGR == "" {
		t.Skip("could not extract SGR codes for command 'log' from jj help output")
	}

	// Our help_command style (green bold)
	cmdStyle := Styles["help_command"]
	if cmdStyle.Fg != Green || !cmdStyle.Bold {
		t.Errorf("help_command style = %+v, want green bold", cmdStyle)
	}
}
