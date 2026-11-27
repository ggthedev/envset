package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

// Nothing typed as a value, and no line of the user's files, may reach the
// log at any level: every argument and file line below holds the canary.
func TestValuesNeverReachTheLog(t *testing.T) {
	const canary = "zzCANARYzz"
	for _, verbose := range []bool{false, true} {
		home := t.TempDir()
		rc := filepath.Join(home, ".zshrc")
		os.WriteFile(rc, []byte("compinit # "+canary+"\ncompinit # "+canary+"\n"+
			"export PATH=$PATH:/"+canary+" :/x\n"+
			"if true; then\n  export INBLOCK="+canary+"\nfi\n"+
			"export OPENAI_KEY="+canary+"\n"+
			"eval \"$("+canary+" init zsh)\"\n"), 0o644)

		exec := func(args ...string) result {
			if verbose {
				args = append([]string{"-v"}, args...)
			}
			return run(t, home, args...)
		}
		for _, args := range [][]string{
			{"add", "--key", "PLAIN", "--value", canary, "--target", "zshrc"},
			{"add", "--key", "PLAIN", "--value=" + canary + "2", "--target", "zshrc"},
			{"add", "--key", "PLAIN", "--value", canary, "--yes", "--dry-run"},
			{"add", "--key", "GITHUB_TOKEN", "--value", canary, "--literal", "--target", "zshenv"},
			{"add", "--key", "DB", "--value", canary, "--target", "launchd", "--dry-run"},
			{"add", "--type", "path_append", "--value", "/" + canary, "--target", "zshenv"},
			{"add", "--key", "K=" + canary, "--value", "x", "--target", "zshrc"},
			{"add", "--key", "K=" + canary, "--value", "x", "--yes"},
			{"add", "--key", "BAD", "--value", "$(" + canary + ")", "--target", "zshrc"},
			// Types that do not use the key do not validate it either.
			{"add", "--type", "eval", "--key", "K=" + canary, "--value", "x", "--target", "zshrc"},
			{"add", "--type", "source", "--key", "K=" + canary, "--value", "/x", "--target", "zshrc"},
			{"add", "--type", "eval", "--key", "K=" + canary, "--value", "x", "--target", "zshrc"},
			{"add", "--type", "source", "--key", "K=" + canary, "--value", "/y", "--yes"},
			{"add", "--key", "BAD", "--value", "\"" + canary, "--raw", "--target", "zshrc"}, // would not parse
			{"add", "--key", "INBLOCK", "--value", canary, "--target", "zshrc"},             // refused
			{"add", canary, canary},
			{"add", "--" + canary},
			{"add", "-kK=" + canary}, // a valid name would be logged: names are not values
			{canary},
			{"remove", "--key", "PLAIN", "--value", canary + "-absent", "--target", "zshrc"},
			{"remove", "--key", "K=" + canary, "--target", "zshrc"},
			{"remove", "--key", "INBLOCK", "--target", "zshrc"},
			{"remove", "--key", "PLAIN", "--target", "zshrc"},
			{"list", "--target", "zshrc"},
			{"list", "--key", "K=" + canary, "--target", "zshrc"},
			{"lazy", "pyenv", "--init-cmd", canary + " init", "--write"},
			{"lazy", "x;" + canary},
			{"audit"},
			{"audit", "--dry-run"},
			{"audit", "--fix", "--yes"},
			{"restore", "--target", "zshrc", "--list"},
			{"restore", "--target", "zshrc"},
			{"reload"},
			{"detect"},
		} {
			exec(args...)
		}

		logPath := filepath.Join(home, "logs", "envset.log")
		b, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		log := string(b)
		for _, ln := range strings.Split(log, "\n") {
			if strings.Contains(ln, canary) {
				t.Errorf("verbose=%v: leaked: %s", verbose, ln)
			}
		}
		// The default log says what happened and where the backups are.
		for _, want := range []string{" INFO ", " ERROR ", "backup saved: ", "end exit=1", "end exit=0"} {
			if !strings.Contains(log, want) {
				t.Errorf("verbose=%v: log lacks %q", verbose, want)
			}
		}
		if got := strings.Contains(log, " DEBUG "); got != verbose {
			t.Errorf("verbose=%v: DEBUG lines present = %v", verbose, got)
		}
		if fi, _ := os.Stat(logPath); fi.Mode().Perm() != 0o600 {
			t.Errorf("log mode %v, want 0600", fi.Mode().Perm())
		}
	}
}

// The terminal still gets the detail that the log leaves out.
func TestTerminalKeepsDetailTheLogOmits(t *testing.T) {
	home := t.TempDir()
	r := run(t, home, "add", "--key", "K=oops", "--value", "x", "--target", "zshrc")
	if r.code == 0 || !strings.Contains(r.stderr, `"K=oops"`) {
		t.Fatalf("code=%d stderr=%q", r.code, r.stderr)
	}
}

// -v traces calls into the log and leaves stdout alone.
func TestVerboseTracesCallsAndKeepsStdoutClean(t *testing.T) {
	home := t.TempDir()
	r := run(t, home, "-v", "add", "--key", "FOO", "--value", "bar", "--target", "zshenv", "--print-source")
	if want := "source " + filepath.Join(home, ".zshenv") + "\n"; r.stdout != want {
		t.Fatalf("stdout = %q, want %q", r.stdout, want)
	}
	b, _ := os.ReadFile(filepath.Join(home, "logs", "envset.log"))
	for _, want := range []string{
		"ops.Add", "targets.(*ShellTarget).Add", "fsutil.Lock", "targets.CommitFile",
		"fsutil.WriteFileAtomic", " enter", " exit after ", "syntax check with ", "atomic write: ",
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("verbose log lacks %q", want)
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

func TestParallelProcessesDoNotLoseWrites(t *testing.T) {
	home := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := exec.Command(bin, "add", fmt.Sprintf("--key=P%d", i), "--value=v", "--target=zshrc")
			c.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "ENVSET_SHELL=zsh",
				"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "ENVSET_LOG_DIR=" + filepath.Join(home, "logs")}
			if out, err := c.CombinedOutput(); err != nil {
				t.Errorf("add %d: %v: %s", i, err, out)
			}
		}(i)
	}
	wg.Wait()
	b, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if n := strings.Count(string(b), "export P"); n != 25 {
		t.Fatalf("%d of 25 writes survived", n)
	}
}

func TestVersionCommandAndFlag(t *testing.T) {
	home := t.TempDir()
	for _, args := range [][]string{{"version"}, {"--version"}} {
		r := run(t, home, args...)
		if r.code != 0 || !strings.HasPrefix(r.stdout, "envset ") || !strings.Contains(r.stdout, runtime.GOOS+"/"+runtime.GOARCH) || strings.Count(r.stdout, "\n") != 1 {
			t.Errorf("%v: code=%d stdout=%q stderr=%q", args, r.code, r.stdout, r.stderr)
		}
	}
	if r := run(t, home, "version", "extra"); r.code == 0 {
		t.Error("stray argument accepted")
	}
}
