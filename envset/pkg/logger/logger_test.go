package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLogPath_Default(t *testing.T) {
	// Isolate: clear env vars and set HOME to a tempdir
	t.Setenv(EnvLogFile, "")
	t.Setenv(EnvLogDir, "")
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
