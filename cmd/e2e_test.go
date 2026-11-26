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

func TestSecretsNeverReachTheLog(t *testing.T) {
	home := t.TempDir()
	run(t, home, "-v", "add", "--key", "GITHUB_TOKEN", "--value", "ghp_e2e_secret", "--target", "zshenv", "--dry-run")
	run(t, home, "add", "--key", "MY_API_KEY", "--value=sk_e2e_secret", "--target", "zshenv", "--dry-run")
	// Even a non-secret-looking key never has its value in the cmd banner.
	run(t, home, "add", "--key", "PLAIN", "--value", "banner_e2e_value", "--target", "zshenv", "--dry-run")

	logDir := filepath.Join(home, "logs")
	entries, err := os.ReadDir(logDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("no log written: %v", err)
	}
	for _, e := range entries {
		p := filepath.Join(logDir, e.Name())
		b, _ := os.ReadFile(p)
		for _, secret := range []string{"ghp_e2e_secret", "sk_e2e_secret"} {
			if strings.Contains(string(b), secret) {
				t.Errorf("%s leaked into %s", secret, e.Name())
			}
		}
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(ln, "cmd ") && strings.Contains(ln, "banner_e2e_value") {
				t.Errorf("value in cmd banner: %s", ln)
			}
		}
		if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v, want 0600", e.Name(), fi.Mode().Perm())
		}
	}
}

func TestReadOnlyAuditWritesNoConfig(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, ".zshrc"), []byte("export EDITOR=vi\n"), 0o644)
	for _, args := range [][]string{{"audit"}, {"audit", "--dry-run"}} {
		r := run(t, home, args...)
		if r.code != 0 {
			t.Fatalf("%v: %s", args, r.stderr)
		}
		if _, err := os.Stat(filepath.Join(home, ".config", "envset")); !os.IsNotExist(err) {
			t.Fatalf("%v created ~/.config/envset", args)
		}
	}
}

func TestMalformedCustomRulesWarnButAuditRuns(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".config", "envset")
	os.MkdirAll(cfg, 0o755)
	bad := "{ broken"
	os.WriteFile(filepath.Join(cfg, "custom.json"), []byte(bad), 0o644)
	os.WriteFile(filepath.Join(home, ".zshrc"), []byte("compinit\ncompinit\n"), 0o644)

	r := run(t, home, "audit")
	if !strings.Contains(r.stderr, "custom.json is not valid JSON") {
		t.Fatalf("no warning: %q", r.stderr)
	}
	if !strings.Contains(r.stdout, "compinit") {
		t.Fatalf("audit did not run: %q", r.stdout)
	}
	run(t, home, "audit", "--fix", "--yes")
	if b, _ := os.ReadFile(filepath.Join(cfg, "custom.json")); string(b) != bad {
		t.Fatalf("malformed custom.json was overwritten: %q", b)
	}
}

func TestReloadAllEmitsOnlyStartupFiles(t *testing.T) {
	home := t.TempDir()
	for _, f := range []string{".zshrc", ".zshenv", ".gg_system_exports", ".bashrc"} {
		os.WriteFile(filepath.Join(home, f), []byte("x=1\n"), 0o644)
	}
	run(t, home, "add", "--key", "G", "--value", "1", "--target", "launchd", "--dry-run")
	r := run(t, home, "reload")
	want := "source " + filepath.Join(home, ".zshenv") + "\nsource " + filepath.Join(home, ".zshrc") + "\n"
	if r.stdout != want {
		t.Fatalf("reload = %q, want %q", r.stdout, want)
	}
}

func TestListHidesControlFlow(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, ".zshenv"), []byte("if true; then\n  export A=1\nelse\n  export A=2\nfi\n"), 0o644)
	r := run(t, home, "list", "--target", "zshenv")
	for _, bad := range []string{": if true", ": else", ": fi"} {
		if strings.Contains(r.stdout, bad) {
			t.Errorf("list shows %q:\n%s", bad, r.stdout)
		}
	}
	if !strings.Contains(r.stdout, "line 2: export A=1") || !strings.Contains(r.stdout, "line 4: export A=2") {
		t.Errorf("list lost definitions:\n%s", r.stdout)
	}
}

func TestAddWarnsWhenAnotherDefinitionWins(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, ".zshrc"), []byte("export K=rc\n"), 0o644)
	r := run(t, home, "add", "--key", "K", "--value", "env", "--target", "zshenv")
	if !strings.Contains(r.stderr, "interactive shells will use that value") || strings.Contains(r.stderr, "shadowed") {
		t.Fatalf("stderr: %q", r.stderr)
	}
	r = run(t, home, "add", "--key", "K", "--value", "rc", "--yes")
	if !strings.Contains(r.stdout, "already set") || strings.Contains(r.stdout, "will be updated") {
		t.Fatalf("identical add: %q", r.stdout)
	}
}

// A refused line is shown on the terminal, but never a secret on it, even when
// the requested key is harmless. Nothing from it reaches the log.
func TestRefusedLineHidesOtherSecrets(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, ".zshrc"), []byte("export K=1 OTHER_TOKEN=hush_e2e\n"), 0o644)
	r := run(t, home, "-v", "add", "--key", "K", "--value", "2", "--target", "zshrc")
	if r.code == 0 || !strings.Contains(r.stderr, "line 1: [redacted]") || strings.Contains(r.stderr+r.stdout, "hush_e2e") {
		t.Fatalf("code=%d stderr=%q", r.code, r.stderr)
	}
	b, _ := os.ReadFile(filepath.Join(home, "logs", "envset.log"))
	if strings.Contains(string(b), "hush_e2e") {
		t.Fatal("secret in log")
	}
}
