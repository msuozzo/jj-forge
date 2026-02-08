package forge

import (
	"fmt"
	"os"
	"path/filepath"
)

// Dir returns the forge data directory (.jj/forge/) for the given repo root,
// creating it if necessary.
func Dir(repoRoot string) (string, error) {
	p := filepath.Join(repoRoot, ".jj", "forge")
	if err := os.MkdirAll(p, 0755); err != nil {
		return "", fmt.Errorf("creating forge directory: %w", err)
	}
	return p, nil
}
