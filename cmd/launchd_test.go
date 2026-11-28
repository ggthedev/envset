package cmd_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The launchd target against the real launchctl. It sets a variable in the
// login session, so it runs only when asked for; CI asks on a throwaway Mac.
// The files go to a temporary HOME, and the variable is unset afterwards.
func TestRealLaunchd(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("ENVSET_TEST_LAUNCHD") != "1" {
		t.Skip("set ENVSET_TEST_LAUNCHD=1 on macOS to test against the real launchctl")
	}
	key := fmt.Sprintf("ENVSET_PROBE_%d", os.Getpid())
	const value = `two  words, a "quote" and a \ backslash`

	launchctl := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("launchctl", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("launchctl %s: %v: %s", args[0], err, out)
		}
		return string(out)
	}
	t.Cleanup(func() { exec.Command("launchctl", "unsetenv", key).Run() })
	check := func(step, want string) {
		t.Helper()
		if got := strings.TrimSuffix(launchctl("getenv", key), "\n"); got != want {
			t.Errorf("after %s launchd has %q, want %q", step, got, want)
		}
	}

	home := t.TempDir()
	r := run(t, home, "add", "--key", key, "--value", value, "--target", "launchd")
	if r.code != 0 {
		t.Fatalf("add: exit %d: %s", r.code, r.stderr)
	}
	check("add", value)

	plist := filepath.Join(home, "Library", "LaunchAgents", "com.envset.environment.plist")
	if out, err := exec.Command("plutil", "-lint", plist).CombinedOutput(); err != nil {
		t.Errorf("launch agent is not a valid plist: %v: %s", err, out)
	}

	// What the launch agent does at the next login.
	launchctl("unsetenv", key)
	script := filepath.Join(home, ".config", "envset", "launchd.sh")
	if out, err := exec.Command("sh", script).CombinedOutput(); err != nil {
		t.Fatalf("launchd.sh: %v: %s", err, out)
	}
	check("running launchd.sh", value)

	if r := run(t, home, "remove", "--key", key, "--target", "launchd"); r.code != 0 {
		t.Fatalf("remove: exit %d: %s", r.code, r.stderr)
	}
	check("remove", "")
}
