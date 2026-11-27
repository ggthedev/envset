package targets

import (
	"os"
	"testing"
)

// launchctlCalls records what the code under test asked launchctl to do.
var launchctlCalls [][]string

// TestMain isolates tests from the real machine: HOME and backups are
// temporary, and launchctl is never actually executed (it would change the live login session).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "envset-targets-backups-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("ENVSET_BACKUP_DIR", dir)
	// Lock files live under the config directory: give it a temporary HOME.
	os.Setenv("HOME", dir)
	os.Unsetenv("XDG_CONFIG_HOME")
	runLaunchctl = func(args ...string) error {
		launchctlCalls = append(launchctlCalls, args)
		return nil
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
