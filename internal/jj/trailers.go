package jj

import (
	"fmt"
	"strings"
)

const ForgeParentKey = "forge-parent"

// UpdateForgeParent adds or updates the forge-parent trailer in the description.
// It ensures that the trailer is placed in the trailer block at the end of the description.
func UpdateForgeParent(description, parentID string) string {
	lines := strings.Split(strings.TrimRight(description, "\n"), "\n")
	trailer := fmt.Sprintf("%s: %s", ForgeParentKey, parentID)

	// Check if it already exists and update it
	for i, line := range lines {
		if strings.HasPrefix(strings.ToLower(line), strings.ToLower(ForgeParentKey)+":") {
			lines[i] = trailer
			return strings.Join(lines, "\n") + "\n"
		}
	}

	// If it doesn't exist, we need to append it.
	// We should try to find if there's already a trailer block or if we need to start one.
	// For simplicity in this MVP, we'll append it with a blank line if the description is non-empty.
	if len(lines) == 1 && lines[0] == "" {
		return trailer + "\n"
	}

	// If the last line looks like a trailer AND it's not the only line, just append.
	// Otherwise, add a blank line.
	lastLine := lines[len(lines)-1]
	if len(lines) > 1 && isTrailer(lastLine) {
		return strings.Join(lines, "\n") + "\n" + trailer + "\n"
	}

	return strings.Join(lines, "\n") + "\n\n" + trailer + "\n"
}

// RemoveForgeParent removes the forge-parent trailer from the description.
func RemoveForgeParent(description string) string {
	lines := strings.Split(strings.TrimRight(description, "\n"), "\n")
	var newLines []string
	for _, line := range lines {
		if !strings.HasPrefix(strings.ToLower(line), strings.ToLower(ForgeParentKey)+":") {
			newLines = append(newLines, line)
		}
	}
	
	// Clean up trailing empty lines if we removed the last trailer and left a blank line.
	res := strings.Join(newLines, "\n")
	return strings.TrimRight(res, "\n") + "\n"
}

func isTrailer(line string) bool {
	// Simple heuristic: "Key: Value"
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return false
	}
	key := strings.TrimSpace(parts[0])
	// Keys don't usually have spaces
	return key != "" && !strings.Contains(key, " ")
}
