package logger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// readRun initialises a log, runs body, closes and returns the file's text.
func readRun(t *testing.T, verbose bool, argv []string, body func()) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	log := filepath.Join(tmp, "out.log")
	t.Setenv(EnvLogFile, log)

	Init(verbose, argv)
	body()
	Close(0)

	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var lineRe = regexp.MustCompile(`^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d\.\d{3} (ERROR|WARN |INFO |DEBUG) \[\d+\] \S+:\d+ \S+ +\S.*$`)

func TestEveryLineIsSelfContained(t *testing.T) {
	got := readRun(t, true, []string{"envset", "add", "--key", "FOO"}, func() {
		Info("two\nlines\rhere")
		Warn("careful")
		Error(errors.New("boom"))
		Debug("detail")
	})
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 6 { // start, four above, end
		t.Fatalf("%d lines:\n%s", len(lines), got)
	}
	for _, l := range lines {
		if !lineRe.MatchString(l) {
			t.Errorf("malformed line: %q", l)
		}
	}
	for _, want := range []string{"start envset ", `two\nlines\rhere`, "end exit=0 elapsed=", "ERROR", "boom"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q:\n%s", want, got)
		}
	}
}

func traced() { defer Enter()() }

func TestDebugAndTraceOnlyWhenVerbose(t *testing.T) {
	body := func() { Debug("debug-marker"); traced(); Info("info-marker") }

	quiet := readRun(t, false, []string{"envset"}, body)
	if strings.Contains(quiet, "DEBUG") || strings.Contains(quiet, "debug-marker") {
		t.Errorf("debug lines in the default log:\n%s", quiet)
	}
	if !strings.Contains(quiet, "info-marker") {
		t.Errorf("info line missing:\n%s", quiet)
	}

	loud := readRun(t, true, []string{"envset"}, body)
	for _, want := range []string{"debug-marker", "logger.traced", " enter", " exit after "} {
		if !strings.Contains(loud, want) {
			t.Errorf("verbose log lacks %q:\n%s", want, loud)
		}
	}
}

func TestCallerInfoIncluded(t *testing.T) {
	got := readRun(t, true, []string{"envset"}, func() { Info("a"); Warn("b"); Debug("c"); Error(errors.New("d")) })
	// Each writer names the function that called it, not the logger.
	if n := strings.Count(got, "logger.TestCallerInfoIncluded"); n != 4 {
		t.Errorf("caller named %d times, want 4:\n%s", n, got)
	}
}

func TestNoOpWhenNotInitialised(t *testing.T) {
	// Reset global to nil
	Close(0)
	global = nil
	// These must not panic
	Info("y")
	Warn("w")
	Debug("d")
	Error(errors.New("e"))
	Enter()()
}

func TestSafeArgs(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/x/bin/envset add --key GITHUB_TOKEN --value s3cr3t --target zshenv",
			"envset add --key [redacted] --value [redacted] --target zshenv"},
		{"envset add --value=s3cr3t -ks3cr3t -t zshrc --type=export --dry-run",
			"envset add --value=[redacted] [redacted] -t zshrc --type=export --dry-run"},
		{"envset add s3cr3t add --passwrd s3cr3t --value -s3cr3t",
			"envset add [redacted] [redacted] [redacted] [redacted] --value [redacted]"},
		{"envset lazy s3cr3t --init-cmd s3cr3t --write", "envset lazy [redacted] --init-cmd [redacted] --write"},
		{"envset remove --comment=s3cr3t -- s3cr3t", "envset remove --comment=[redacted] -- [redacted]"},
		{"envset s3cr3t", "envset [redacted]"},
	}
	for _, c := range cases {
		in := strings.Fields(c.in)
		got := strings.Join(SafeArgs(in), " ")
		if got != c.want {
			t.Errorf("SafeArgs(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
		if strings.Join(in, " ") != c.in {
			t.Error("input slice was modified")
		}
	}
}

func TestSafeKeyAndPrivate(t *testing.T) {
	for in, want := range map[string]string{"FOO_1": "FOO_1", "": "", "FOO=s3cr3t": "[invalid]", "1X": "[invalid]"} {
		if got := SafeKey(in); got != want {
			t.Errorf("SafeKey(%q) = %q, want %q", in, got, want)
		}
	}
	inner := Private("bad name", errors.New(`bad name "s3cr3t"`))
	wrapped := fmt.Errorf("adding: %w", inner)
	if got := LogMessage(wrapped); got != "adding: bad name" {
		t.Errorf("LogMessage = %q", got)
	}
	if !strings.Contains(wrapped.Error(), "s3cr3t") {
		t.Error("the terminal message lost its detail")
	}
	if got := LogMessage(errors.New("plain")); got != "plain" {
		t.Errorf("LogMessage = %q", got)
	}
}

func TestInitFileIsPrivate(t *testing.T) {
	tmp := t.TempDir()
	log := filepath.Join(tmp, "out.log")
	t.Setenv(EnvLogFile, log)
	os.WriteFile(log, []byte("old\n"), 0o644) // as created by older versions

	Init(false, []string{"envset"})
	Close(0)

	fi, _ := os.Stat(log)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("log mode %v, want 0600", fi.Mode().Perm())
	}
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

// Without a home directory there is nowhere to log; never write into the cwd.
func TestNoLogWithoutHome(t *testing.T) {
	for _, k := range []string{"HOME", EnvLogFile, EnvLogDir, "XDG_CONFIG_HOME"} {
		t.Setenv(k, "")
	}
	proj := t.TempDir()
	wd, _ := os.Getwd()
	os.Chdir(proj)
	defer os.Chdir(wd)

	Init(false, []string{"envset", "detect"})
	Info("x")
	Close(0)
	if entries, _ := os.ReadDir(proj); len(entries) != 0 {
		t.Fatalf("created %v in the working directory", entries)
	}
}
