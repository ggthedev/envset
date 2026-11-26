package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLogPath_Default(t *testing.T) {
	// Isolate: clear env vars and set HOME to a tempdir
	t.Setenv(EnvLogFile, "")
	t.Setenv(EnvLogDir, "")
	t.Setenv("XDG_CONFIG_HOME", "")
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	got := ResolveLogPath()
	want := filepath.Join(tmp, ".config", "envset", "logs", "envset.log")
	if got != want {
		t.Errorf("default path = %s, want %s", got, want)
	}
}

func TestResolveLogPath_EnvLogFileWins(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv(EnvLogFile, "~/custom/logs/x.log")
	t.Setenv(EnvLogDir, "")

	got := ResolveLogPath()
	want := filepath.Join(tmp, "custom", "logs", "x.log")
	if got != want {
		t.Errorf("env file path = %s, want %s", got, want)
	}
}

func TestResolveLogPath_EnvLogDirAppendsDefaultName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv(EnvLogFile, "")
	t.Setenv(EnvLogDir, "~/mylogs")

	got := ResolveLogPath()
	want := filepath.Join(tmp, "mylogs", "envset.log")
	if got != want {
		t.Errorf("env dir path = %s, want %s", got, want)
	}
}

func TestResolveLogPath_ConfigDotEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv(EnvLogFile, "")
	t.Setenv(EnvLogDir, "")

	cfgDir := filepath.Join(tmp, ".config", "envset")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, ".env"),
		[]byte("ENVSET_LOG_DIR=\"~/audit-logs\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ResolveLogPath()
	want := filepath.Join(tmp, "audit-logs", "envset.log")
	if got != want {
		t.Errorf("dotenv path = %s, want %s", got, want)
	}
}

func TestInitClose_WritesBannerAndDoneMarker(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv(EnvLogFile, filepath.Join(tmp, "out.log"))

	Init(false, []string{"envset", "add", "--key", "FOO"})
	Step("write")
	Info("inputs key=FOO")
	Result("ok")
	Close()

	b, err := os.ReadFile(filepath.Join(tmp, "out.log"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)

	for _, want := range []string{
		"RUN ", "cmd  envset add", "STEP",
		"write", "INFO", "RESULT", "DONE", strings.Repeat("=", 80),
	} {
		if !strings.Contains(content, want) {
			t.Errorf("expected log to contain %q; got:\n%s", want, content)
		}
	}
}

func TestTraceOnlyWhenVerbose(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv(EnvLogFile, filepath.Join(tmp, "out.log"))

	Init(false, []string{"envset"})
	Trace("should-not-appear")
	Close()

	b, _ := os.ReadFile(filepath.Join(tmp, "out.log"))
	if strings.Contains(string(b), "should-not-appear") {
		t.Errorf("trace line leaked into non-verbose log")
	}

	// Now verbose
	t.Setenv(EnvLogFile, filepath.Join(tmp, "v.log"))
	Init(true, []string{"envset"})
	Trace("verbose-marker")
	Close()

	b, _ = os.ReadFile(filepath.Join(tmp, "v.log"))
	if !strings.Contains(string(b), "verbose-marker") {
		t.Errorf("trace line missing in verbose log")
	}
}

func TestCallerInfoIncluded(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv(EnvLogFile, filepath.Join(tmp, "caller.log"))

	Init(false, []string{"envset"})
	Step("hello") // recorded line — caller should be this test function
	Close()

	b, err := os.ReadFile(filepath.Join(tmp, "caller.log"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)

	// Must include both the file and the function name of the caller
	if !strings.Contains(content, "logger_test.go:") {
		t.Errorf("caller file missing from log:\n%s", content)
	}
	if !strings.Contains(content, "logger.TestCallerInfoIncluded") {
		t.Errorf("caller function missing from log:\n%s", content)
	}
}

func TestCallerInfoFromTrace(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv(EnvLogFile, filepath.Join(tmp, "trace.log"))

	Init(true, []string{"envset"})
	Trace("via-trace")
	Close()

	b, _ := os.ReadFile(filepath.Join(tmp, "trace.log"))
	if !strings.Contains(string(b), "logger_test.go:") {
		t.Errorf("Trace lines missing caller info:\n%s", string(b))
	}
}

func TestNoOpWhenNotInitialised(t *testing.T) {
	// Reset global to nil
	Close()
	global = nil
	// These must not panic
	Step("x")
	Info("y")
	QA("q", "a")
	Result("z")
	Trace("t")
}

func TestResolveLogPath_XDGAndNoCwdDotEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv(EnvLogFile, "")
	t.Setenv(EnvLogDir, "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))

	// A project .env in the cwd must not redirect the log.
	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, ".env"), []byte("ENVSET_LOG_DIR=/tmp/hijack\n"), 0o644)
	wd, _ := os.Getwd()
	os.Chdir(proj)
	defer os.Chdir(wd)

	got := ResolveLogPath()
	want := filepath.Join(tmp, "xdg", "envset", "logs", "envset.log")
	if got != want {
		t.Errorf("path = %s, want %s", got, want)
	}
}

func TestRedactArgs(t *testing.T) {
	in := []string{"envset", "add", "--key", "GITHUB_TOKEN", "--value", "ghp_secret", "--value=ghp_two", "--target", "zshenv"}
	got := strings.Join(RedactArgs(in), " ")
	if strings.Contains(got, "ghp_") {
		t.Fatalf("secret leaked: %s", got)
	}
	if !strings.Contains(got, "--value [redacted] --value=[redacted] --target zshenv") {
		t.Fatalf("unexpected: %s", got)
	}
	if in[5] != "ghp_secret" {
		t.Fatal("input slice was modified")
	}
}

func TestInitBannerRedactsAndFileIsPrivate(t *testing.T) {
	tmp := t.TempDir()
	log := filepath.Join(tmp, "out.log")
	t.Setenv(EnvLogFile, log)
	os.WriteFile(log, []byte("old\n"), 0o644) // as created by older versions

	Init(false, []string{"envset", "add", "--key", "FOO_TOKEN", "--value", "s3cr3t"})
	Close()

	b, _ := os.ReadFile(log)
	if strings.Contains(string(b), "s3cr3t") {
		t.Fatalf("banner leaked value:\n%s", b)
	}
	fi, _ := os.Stat(log)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("log mode %v, want 0600", fi.Mode().Perm())
	}
}

func TestRotate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "envset.log")
	for i, content := range []string{"gen1", "gen2", "gen3", "gen4"} {
		os.WriteFile(p, []byte(content+strings.Repeat("x", 20)), 0o600)
		rotate(p, 10, 3)
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("round %d: log not rotated", i)
		}
	}
	// Newest rotated is .1, oldest kept is .3; gen1 was dropped.
	for gen, want := range map[int]string{1: "gen4", 2: "gen3", 3: "gen2"} {
		b, _ := os.ReadFile(fmt.Sprintf("%s.%d", p, gen))
		if !strings.HasPrefix(string(b), want) {
			t.Errorf(".%d = %q, want %s…", gen, b, want)
		}
	}
	if _, err := os.Stat(p + ".4"); err == nil {
		t.Error("kept more than 3 generations")
	}
	// Small files are left alone.
	os.WriteFile(p, []byte("tiny"), 0o600)
	rotate(p, 10, 3)
	if _, err := os.Stat(p); err != nil {
		t.Error("small log was rotated")
	}
}
