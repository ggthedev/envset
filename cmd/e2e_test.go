package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "envset-e2e-")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "envset")
	build := exec.Command("go", "build", "-o", bin, "..")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("building envset: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	stdout, stderr string
	code           int
}

// run executes envset in an isolated HOME with stdin=/dev/null and no TTY.
// Any command still running after 5s is a hang and fails the test.
func run(t *testing.T, home string, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, bin, args...)
	c.Env = []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"USER=" + os.Getenv("USER"),
		"ENVSET_SHELL=zsh",
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"ENVSET_LOG_DIR=" + filepath.Join(home, "logs"),
	}
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	err := c.Run()
	if ctx.Err() != nil {
		t.Fatalf("envset %v hung (killed after 5s)\nstderr: %s", args, errb.String())
	}
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{out.String(), errb.String(), code}
}

func TestMissingFlagsFailFastWithoutTTY(t *testing.T) {
	home := t.TempDir()
	for _, args := range [][]string{
		{"add", "--print-source"},
		{"add", "--key", "FOO"},
		{"add", "--yes"},
		{"remove", "--key", "FOO"},
		{"list"},
	} {
		r := run(t, home, args...)
		if r.code == 0 {
			t.Errorf("%v: exit 0, want failure", args)
		}
		if r.stdout != "" {
			t.Errorf("%v: stdout must stay empty for eval, got %q", args, r.stdout)
		}
		if !strings.Contains(r.stderr, "required") {
			t.Errorf("%v: stderr %q lacks a 'required' hint", args, r.stderr)
		}
	}
}

func TestPrintSourceEmitsOnlyTheSourceLine(t *testing.T) {
	home := t.TempDir()
	r := run(t, home, "add", "--key", "FOO", "--value", "bar", "--target", "zshenv", "--print-source")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	want := "source " + filepath.Join(home, ".zshenv") + "\n"
	if r.stdout != want {
		t.Fatalf("stdout = %q, want %q", r.stdout, want)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".zshenv")); string(b) != "export FOO=bar\n" {
		t.Fatalf(".zshenv = %q", b)
	}
}

func TestReloadHintNamesTheTarget(t *testing.T) {
	home := t.TempDir()
	r := run(t, home, "add", "--key", "FOO", "--value", "bar", "--target", "zshrc")
	if !strings.Contains(r.stderr, "envset reload --target zshrc") || strings.Contains(r.stderr, "<name>") {
		t.Fatalf("reload hint: %q", r.stderr)
	}
}

func TestAutoPlacementUsesZshenvForPATHAndJavaHome(t *testing.T) {
	home := t.TempDir()
	for _, args := range [][]string{
		{"add", "--key", "JAVA_HOME", "--value", "/opt/jdk", "--yes", "--dry-run"},
		{"add", "--type", "path_append", "--value", "/opt/x/bin", "--yes", "--dry-run"},
	} {
		r := run(t, home, args...)
		if r.code != 0 || !strings.Contains(r.stdout, "Auto-placed: zshenv") {
			t.Errorf("%v: code=%d stdout=%q stderr=%q", args, r.code, r.stdout, r.stderr)
		}
	}
}

func TestAuditNeverPromptsWithoutTTY(t *testing.T) {
	home := t.TempDir()
	// A duplicate compinit gives audit something auto-fixable.
	const rc = "compinit\ncompinit\n"
	os.WriteFile(filepath.Join(home, ".zshrc"), []byte(rc), 0o644)

	r := run(t, home, "audit", "--dry-run")
	if !strings.Contains(r.stdout, "+ # envset: compinit") {
		t.Fatalf("audit --dry-run showed no fix diff: %s", r.stdout)
	}
	if strings.Contains(r.stdout+r.stderr, "choice") {
		t.Fatalf("audit --dry-run prompted: %s", r.stdout)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".zshrc")); string(b) != rc {
		t.Fatalf("audit --dry-run modified .zshrc: %q", b)
	}

	r = run(t, home, "audit", "--fix")
	if r.code == 0 || !strings.Contains(r.stderr, "--yes") {
		t.Fatalf("audit --fix without TTY: code=%d stderr=%q", r.code, r.stderr)
	}
}

func TestShellFlag(t *testing.T) {
	home := t.TempDir()
	r := run(t, home, "--shell", "bash", "detect")
	if r.code != 0 || !strings.Contains(r.stdout, "Placing for:         bash (from --shell flag)") {
		t.Fatalf("detect --shell bash: %q %q", r.stdout, r.stderr)
	}
	if r := run(t, home, "--shell", "sh", "detect"); r.code == 0 {
		t.Fatal("--shell sh accepted")
	}
}
