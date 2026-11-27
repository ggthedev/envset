package logger

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

// Nothing the user typed as a value, and no line of their files, may reach the
// log at any level. These helpers are how callers keep to that.

// Redacted stands in for anything that is not written to the log.
const Redacted = "[redacted]"

var keyName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// SafeKey returns key if it is a plain variable name, else "[invalid]": a
// mistyped "--key NAME=secret" must not put the secret in the log.
func SafeKey(key string) string {
	if key == "" || keyName.MatchString(key) {
		return key
	}
	return "[invalid]"
}

// Hidden returns "[redacted]" for a non-empty string and "" otherwise, so the
// log still shows whether something was given.
func Hidden(s string) string {
	if s == "" {
		return ""
	}
	return Redacted
}

type logSafe interface {
	error
	LogMessage() string
}

type private struct {
	safe string
	err  error
}

func (p *private) Error() string      { return p.err.Error() }
func (p *private) Unwrap() error      { return p.err }
func (p *private) LogMessage() string { return p.safe }

// Private marks err as quoting user content: the terminal shows err in full,
// the log records safe instead.
func Private(safe string, err error) error { return &private{safe: safe, err: err} }

// LogMessage is the text of err as it may be logged, keeping the context added
// by wrapping around a Private error.
func LogMessage(err error) string {
	var s logSafe
	if !errors.As(err, &s) {
		return err.Error()
	}
	if full, inner := err.Error(), s.Error(); strings.Contains(full, inner) {
		return strings.Replace(full, inner, s.LogMessage(), 1)
	}
	return s.LogMessage()
}

// What may appear in the line that starts each run. Everything not listed here
// is redacted, so a new or mistyped argument is hidden by default.
var (
	commandNames = set("add", "remove", "rm", "delete", "del", "list", "detect", "reload",
		"restore", "lazy", "version", "audit", "help", "completion")
	shownFlags  = set("--target", "-t", "--type", "--shell")
	hiddenFlags = set("--key", "-k", "--value", "--init-cmd", "--backup")
	boolFlags   = set("--dry-run", "--comment", "--yes", "--print-source", "--verbose", "-v",
		"--literal", "--raw", "--append", "--list", "--fix", "--write", "--help", "-h", "--version")
)

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// SafeArgs returns argv as it may be logged: the program and command names,
// flag names, and the values of --target, --type and --shell.
func SafeArgs(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	out := []string{filepath.Base(argv[0])}
	seenCommand := false
	// next says what to do with the argument following a flag that takes one.
	next := ""
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		switch next {
		case "show":
			out, next = append(out, a), ""
			continue
		case "hide":
			out, next = append(out, Redacted), ""
			continue
		}
		if a == "--" {
			out = append(out, a)
			for range argv[i+1:] {
				out = append(out, Redacted)
			}
			break
		}
		if !strings.HasPrefix(a, "-") {
			if !seenCommand && commandNames[a] {
				out, seenCommand = append(out, a), true
			} else {
				out = append(out, Redacted)
			}
			continue
		}
		name, val, hasValue := strings.Cut(a, "=")
		switch {
		case shownFlags[name]:
			out = append(out, a)
			if !hasValue {
				next = "show"
			}
		case hiddenFlags[name]:
			if hasValue {
				out = append(out, name+"="+Redacted)
			} else {
				out, next = append(out, name), "hide"
			}
		case boolFlags[name]:
			if hasValue && val != "true" && val != "false" {
				a = name + "=" + Redacted
			}
			out = append(out, a)
		default:
			out = append(out, Redacted)
		}
	}
	return out
}
