// Package logger provides an audit-trail logger for the envset CLI.
//
// All log entries are written to a file only; nothing is mirrored to the
// terminal so program output (stdout/stderr) stays clean. Verbose mode adds
// per-method TRACE detail to the same file. Use `tail -f` on the log path
// (printed once on stderr at startup when --verbose) to watch flow live.
//
// Each CLI invocation is delimited by an 80-char rule and an RFC3339 timestamp
// so multiple runs in the same file remain easy to scan.
package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Logger is the audit-trail writer. There is normally one global instance
// owned by the CLI; access it via the package-level convenience functions.
type Logger struct {
	file     *os.File
	verbose  bool
	started  time.Time
	path     string
	disabled bool // true if Init failed; turns all calls into no-ops
}

var global *Logger

// Init opens the log file (creating parents as needed) and writes a run banner.
// argv should be os.Args. Failure to open the log file is non-fatal: a warning
// is printed to stderr and subsequent calls become no-ops.
func Init(verbose bool, argv []string) {
	path := ResolveLogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "envset: cannot create log dir %s: %v (logging disabled)\n", filepath.Dir(path), err)
		global = &Logger{disabled: true}
		return
	}
	rotate(path, MaxLogSize, LogGenerations)
	// The log records commands and file paths: keep it private to the user.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "envset: cannot open log file %s: %v (logging disabled)\n", path, err)
		global = &Logger{disabled: true}
		return
	}
	_ = f.Chmod(0o600) // tighten logs created by older versions (0644)

	global = &Logger{
		file:    f,
		verbose: verbose,
		started: time.Now(),
		path:    path,
	}
	global.writeBanner(argv)

	// One-line hint so the user knows where to tail when --verbose is on.
	// We deliberately do NOT mirror every log entry to the terminal: program
	// output (stdout/stderr) must stay clean for piping/scripting.
	if verbose {
		fmt.Fprintf(os.Stderr, "[envset] verbose log: %s  (tail -f to watch)\n", path)
	}
}

// Close finalises the current run: writes a "DONE" line + elapsed time and a
// closing rule, then closes the underlying file. Safe to call multiple times.
func Close() {
	if global == nil || global.disabled || global.file == nil {
		return
	}
	fmt.Fprintf(global.file, "%s [DONE ] elapsed=%s\n",
		ts(), time.Since(global.started).Round(time.Millisecond))
	fmt.Fprintln(global.file, strings.Repeat("=", 80))
	fmt.Fprintln(global.file)
	_ = global.file.Close()
	global.file = nil
}

// Path returns the absolute path of the active log file (empty if disabled).
func Path() string {
	if global == nil || global.disabled {
		return ""
	}
	return global.path
}

// Verbose reports whether trace logging is enabled.
func Verbose() bool {
	return global != nil && global.verbose
}

// ---------- writers ----------

// Step records a high-level pipeline boundary, e.g. "auto-placement".
func Step(name string) { write("STEP", "%s", name) }

// Info writes an always-on entry.
func Info(format string, a ...any) { write("INFO", format, a...) }

// Input records the parsed inputs for the current command.
func Input(format string, a ...any) { write("INPUT", format, a...) }

// QA records a wizard question and the user's answer.
func QA(question, answer string) { write("Q&A", "%s -> %s", question, answer) }

// Decision records a non-trivial choice the placer/auditor made.
func Decision(format string, a ...any) { write("DECIDE", format, a...) }

// Result writes the final outcome line for a command.
func Result(format string, a ...any) { write("RESULT", format, a...) }

// Trace writes a verbose-only flow line. No-op when verbose is off.
func Trace(format string, a ...any) {
	if global == nil || global.disabled || !global.verbose {
		return
	}
	write("TRACE", format, a...)
}

// Warn writes a warning entry (always on).
func Warn(format string, a ...any) { write("WARN", format, a...) }

// ---------- internals ----------

// callerSkip is the number of stack frames between runtime.Caller and the
// user's real call site:
//
//	runtime.Caller (in callerInfo)  ← skip 0
//	callerInfo                       ← skip 1
//	write                            ← skip 2
//	Info/Step/QA/Trace/etc.          ← skip 3 ← what we want
const callerSkip = 3

func write(level, format string, a ...any) {
	if global == nil || global.disabled || global.file == nil {
		return
	}
	msg := fmt.Sprintf(format, a...)
	// Caller column is fixed-width so messages align in `tail`/grep output.
	line := fmt.Sprintf("%s [%-6s] %-44s %s\n", ts(), level, callerInfo(callerSkip), msg)
	_, _ = global.file.WriteString(line)
}

func ts() string { return time.Now().Format("15:04:05.000") }

// callerInfo returns "<short-path>:<line> <pkg.Func>" for the call site
// `skip` frames above this function, e.g. "pkg/ops/ops.go:42 ops.Add".
//
// The file path is trimmed to start at the repo's root marker (.../envset/...)
// so it remains stable regardless of where the binary was built. The function
// name is reduced to the final package + symbol component for readability.
func callerInfo(skip int) string {
	pc, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "?:0 ?"
	}

	// Trim absolute path: keep everything after "/envset/" so the result is a
	// repo-relative path like "pkg/ops/ops.go".
	if idx := strings.LastIndex(file, "/envset/"); idx >= 0 {
		file = file[idx+len("/envset/"):]
	} else {
		file = filepath.Base(file)
	}

	fnName := "?"
	if fn := runtime.FuncForPC(pc); fn != nil {
		fnName = fn.Name()
		// Drop module path: "github.com/.../pkg/ops.Add" -> "ops.Add".
		if i := strings.LastIndex(fnName, "/"); i >= 0 {
			fnName = fnName[i+1:]
		}
	}

	return fmt.Sprintf("%s:%d %s", file, line, fnName)
}

func (l *Logger) writeBanner(argv []string) {
	fmt.Fprintln(l.file, strings.Repeat("=", 80))
	fmt.Fprintf(l.file, "RUN  %s  pid=%d\n", time.Now().Format(time.RFC3339), os.Getpid())
	fmt.Fprintf(l.file, "cmd  %s\n", strings.Join(RedactArgs(argv), " "))
	fmt.Fprintf(l.file, "opts verbose=%v\n", l.verbose)
	fmt.Fprintln(l.file, strings.Repeat("=", 80))
}

// RedactArgs returns argv with every --value argument replaced by
// [redacted]. Values are never written to the banner: the INPUT line logs
// them separately, already redacted when the key looks like a secret.
func RedactArgs(argv []string) []string {
	out := make([]string, len(argv))
	copy(out, argv)
	for i := 0; i < len(out); i++ {
		a := out[i]
		switch {
		case a == "--":
			return out
		case a == "--value" && i+1 < len(out):
			out[i+1] = "[redacted]"
			i++
		case strings.HasPrefix(a, "--value="):
			out[i] = "--value=[redacted]"
		}
	}
	return out
}

// Log rotation: when envset.log exceeds MaxLogSize at startup it becomes
// envset.log.1 (older generations shift up; the oldest is dropped).
const (
	MaxLogSize     = 5 << 20 // 5 MiB
	LogGenerations = 3
)

func rotate(path string, maxSize int64, gens int) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < maxSize {
		return
	}
	os.Remove(fmt.Sprintf("%s.%d", path, gens))
	for i := gens - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", path, i), fmt.Sprintf("%s.%d", path, i+1))
	}
	os.Rename(path, path+".1")
}
