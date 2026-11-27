// Package logger writes envset's log.
//
// One self-contained line per event, in a file only: program output
// (stdout/stderr) stays clean. ERROR, WARN and INFO are always written; DEBUG,
// which includes a trace of every function call, only with --verbose.
//
//	2026-11-28 09:12:03.114 INFO  [4812] pkg/ops/ops.go:168 ops.Add   add: updated EDITOR in ~/.zshenv
//
// No values, file lines, command output or raw arguments are logged at any
// level: see redact.go.
//
// Every other package calls into this one, so it must import only leaf packages.
package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/experimentalist/envset/pkg/version"
)

// Logger is the log writer. There is normally one global instance owned by the
// CLI; access it via the package-level convenience functions.
type Logger struct {
	file     *os.File
	verbose  bool
	started  time.Time
	path     string
	disabled bool // true if Init failed; turns all calls into no-ops
}

var global *Logger

// Init opens the log file (creating parents as needed) and logs the start of
// the run. argv should be os.Args. Failure to open the log file is non-fatal: a
// warning is printed to stderr and subsequent calls become no-ops.
func Init(verbose bool, argv []string) {
	path := ResolveLogPath()
	if path == "" {
		global = &Logger{disabled: true}
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "envset: cannot create log dir %s: %v (logging disabled)\n", filepath.Dir(path), err)
		global = &Logger{disabled: true}
		return
	}
	rotateErr := rotate(path, MaxLogSize, LogGenerations)
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
	emit(levelInfo, callerInfo(2), fmt.Sprintf("start envset %s verbose=%v: %s",
		version.String(), verbose, strings.Join(SafeArgs(argv), " ")))
	if rotateErr != nil {
		Warn("log rotation failed: %v", rotateErr)
	}

	// One-line hint so the user knows where to look when --verbose is on.
	// We deliberately do NOT mirror log entries to the terminal: program
	// output (stdout/stderr) must stay clean for piping/scripting.
	if verbose {
		fmt.Fprintf(os.Stderr, "[envset] verbose log: %s  (tail -f to watch)\n", path)
	}
}

// Close logs the end of the run with its exit code and elapsed time, then
// closes the file. Safe to call multiple times.
func Close(exitCode int) {
	if global == nil || global.disabled || global.file == nil {
		return
	}
	emit(levelInfo, callerInfo(2), fmt.Sprintf("end exit=%d elapsed=%s", exitCode, time.Since(global.started).Round(time.Millisecond)))
	_ = global.file.Close()
	global.file = nil
}

// Started reports whether Init has been called.
func Started() bool { return global != nil }

// Path returns the absolute path of the active log file (empty if disabled).
func Path() string {
	if global == nil || global.disabled {
		return ""
	}
	return global.path
}

// Verbose reports whether debug logging is enabled.
func Verbose() bool {
	return global != nil && global.verbose
}

// ---------- writers ----------

const (
	levelError = "ERROR"
	levelWarn  = "WARN"
	levelInfo  = "INFO"
	levelDebug = "DEBUG"
)

// Error records a failure or refusal. An error made with Private is logged by
// its safe message.
func Error(err error) {
	if active() {
		emit(levelError, callerInfo(2), LogMessage(err))
	}
}

// Warn records something that did not stop the command but deserves a look.
func Warn(format string, a ...any) {
	if active() {
		emit(levelWarn, callerInfo(2), fmt.Sprintf(format, a...))
	}
}

// Info records what a command did: always written.
func Info(format string, a ...any) {
	if active() {
		emit(levelInfo, callerInfo(2), fmt.Sprintf(format, a...))
	}
}

// Debug records detail for troubleshooting; a no-op unless verbose.
func Debug(format string, a ...any) {
	if active() && global.verbose {
		emit(levelDebug, callerInfo(2), fmt.Sprintf(format, a...))
	}
}

func noop() {}

// Enter traces a function call when verbose. Every function outside this
// package starts with
//
//	defer logger.Enter()()
//
// which logs the entry now and the exit, with its duration, on return.
func Enter() func() {
	if !active() || !global.verbose {
		return noop
	}
	caller, start := callerInfo(2), time.Now()
	emit(levelDebug, caller, "enter")
	return func() {
		if active() {
			emit(levelDebug, caller, "exit after "+time.Since(start).Round(time.Microsecond).String())
		}
	}
}

// ---------- internals ----------

func active() bool {
	return global != nil && !global.disabled && global.file != nil
}

var oneLine = strings.NewReplacer("\n", `\n`, "\r", `\r`)

// emit writes one line with a single write, so that lines from concurrent runs
// do not interleave.
func emit(level, caller, msg string) {
	// Caller column is fixed-width so messages align in `tail`/grep output.
	line := fmt.Sprintf("%s %-5s [%d] %-44s %s\n",
		time.Now().Format("2006-01-02 15:04:05.000"), level, os.Getpid(), caller, oneLine.Replace(msg))
	_, _ = global.file.WriteString(line)
}

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

// Log rotation: when envset.log exceeds MaxLogSize at startup it becomes
// envset.log.1 (older generations shift up; the oldest is dropped).
const (
	MaxLogSize     = 5 << 20 // 5 MiB
	LogGenerations = 3
)

func rotate(path string, maxSize int64, gens int) error {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < maxSize {
		return nil // no log yet, or still small
	}
	os.Remove(fmt.Sprintf("%s.%d", path, gens))
	for i := gens - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", path, i), fmt.Sprintf("%s.%d", path, i+1)) // older ones may not exist
	}
	return os.Rename(path, path+".1")
}
