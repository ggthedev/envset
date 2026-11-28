package cmd_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// lookShell finds a real shell. A missing one skips the test, unless
// ENVSET_TEST_REQUIRE_SHELLS=1: the test workflow installs all of them.
func lookShell(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		if os.Getenv("ENVSET_TEST_REQUIRE_SHELLS") == "1" {
			t.Fatalf("%s is not installed", name)
		}
		t.Skipf("%s not installed", name)
	}
	return path
}

// startShell starts a shell the way a terminal or script would, so that it
// reads its own startup files from home, and returns what script prints.
func startShell(t *testing.T, shell, home, script string, flags ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, shell, append(flags, "-c", script)...)
	c.Dir = home
	// Nothing inherited: no ZDOTDIR, BASH_ENV or variables of the real user.
	c.Env = []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"USER=" + os.Getenv("USER"),
		"TERM=dumb",
	}
	out, err := c.Output()
	if err != nil {
		t.Fatalf("%s %v -c %q: %v", shell, flags, script, err)
	}
	return string(out)
}

// A variable added to a startup file is there in a new shell of the kind that
// reads that file, is gone after remove, and is back after restore.
func TestNewShellsSeeAddRemoveRestore(t *testing.T) {
	const value = `two  words, a "quote" and a \ backslash`
	for _, c := range []struct {
		shell, target string
		flags         []string
	}{
		{"zsh", "zshenv", nil}, // a script or cron job
		{"zsh", "zshrc", []string{"-i"}},
		{"zsh", "zprofile", []string{"-l"}},
		{"bash", "bashrc", []string{"-i"}},
		{"bash", "bash_profile", []string{"-l"}},
		{"bash", "profile", []string{"-l"}},
		{"fish", "config_fish", nil},
	} {
		t.Run(c.shell+"/"+c.target, func(t *testing.T) {
			shell := lookShell(t, c.shell)
			home := t.TempDir()
			envset := func(args ...string) {
				t.Helper()
				args = append([]string{"--shell", c.shell}, append(args, "--target", c.target)...)
				if r := run(t, home, args...); r.code != 0 {
					t.Fatalf("envset %v: exit %d: %s", args, r.code, r.stderr)
				}
			}
			check := func(step, want string) {
				t.Helper()
				got := startShell(t, shell, home, `printf '<%s>' "$ENVSET_PROBE"`, c.flags...)
				if !strings.HasSuffix(got, "<"+want+">") {
					t.Errorf("after %s the shell prints %q, want <%s>", step, got, want)
				}
			}
			envset("add", "--key", "ENVSET_PROBE", "--value", value)
			check("add", value)
			envset("remove", "--key", "ENVSET_PROBE")
			check("remove", "")
			envset("restore")
			check("restore", value)
		})
	}
}

// Without --target, PATH entries reach scripts as well as terminals, and
// EDITOR reaches interactive shells.
func TestAutoPlacementReachesRealZsh(t *testing.T) {
	zsh := lookShell(t, "zsh")
	home := t.TempDir()
	for _, args := range [][]string{
		{"add", "--type", "path_append", "--value", "/opt/envset-probe/bin", "--yes"},
		{"add", "--key", "EDITOR", "--value", "envset-probe-editor", "--yes"},
	} {
		if r := run(t, home, args...); r.code != 0 {
			t.Fatalf("envset %v: exit %d: %s", args, r.code, r.stderr)
		}
	}
	for _, flags := range [][]string{nil, {"-i"}, {"-l"}, {"-l", "-i"}} {
		got := startShell(t, zsh, home, `print -r -- ":$PATH:"`, flags...)
		if !strings.Contains(got, ":/opt/envset-probe/bin:") {
			t.Errorf("zsh %v: PATH lacks the entry: %q", flags, got)
		}
	}
	got := startShell(t, zsh, home, `print -r -- "$EDITOR"`, "-i")
	if !strings.Contains(got, "envset-probe-editor") {
		t.Errorf("interactive zsh: EDITOR = %q", got)
	}
}
