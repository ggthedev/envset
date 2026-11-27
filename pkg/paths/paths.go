// Package paths locates envset's own directory. It imports nothing from
// envset, so that every other package, the logger included, can use it.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigDir returns envset's config directory:
// $XDG_CONFIG_HOME/envset if set, otherwise ~/.config/envset.
func ConfigDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "envset"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "envset"), nil
}
