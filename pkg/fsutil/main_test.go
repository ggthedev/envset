package fsutil

import (
	"os"
	"testing"
)

// TestMain keeps lock files and backups out of the real ~/.config/envset.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "envset-fsutil-home-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", dir)
	os.Unsetenv("XDG_CONFIG_HOME")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
