package audit

import (
	"os"
	"testing"
)

// TestMain keeps fix backups out of the real ~/.config/envset.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "envset-audit-backups-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("ENVSET_BACKUP_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
