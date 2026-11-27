package audit

import (
	"os"
	"testing"
)

// TestMain keeps fix backups and lock files out of the real ~/.config/envset.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "envset-audit-backups-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("ENVSET_BACKUP_DIR", dir)
	// Lock files live under the config directory: give it a temporary HOME.
	os.Setenv("HOME", dir)
	os.Unsetenv("XDG_CONFIG_HOME")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
