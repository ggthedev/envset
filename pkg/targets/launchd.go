package targets

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/experimentalist/envset/pkg/fsutil"
)

// LaunchdTarget manages macOS launchd environment variable settings.
// It writes launchctl setenv commands to ~/.config/envset/launchd.sh
// and ensures a launchd agent plist at ~/Library/LaunchAgents/com.envset.environment.plist
// runs it on user login.
type LaunchdTarget struct {
	name      string
	path      string // ~/.config/envset/launchd.sh
	plistPath string // ~/Library/LaunchAgents/com.envset.environment.plist
	homeDir   string
}

// NewLaunchdTarget creates a new LaunchdTarget.
func NewLaunchdTarget(home string) (*LaunchdTarget, error) {
	path := filepath.Join(fsutil.ConfigDirFor(home), "launchd.sh")
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.envset.environment.plist")
	return &LaunchdTarget{
		name:      "launchd",
		path:      path,
		plistPath: plistPath,
		homeDir:   home,
	}, nil
}

func (l *LaunchdTarget) Name() string { return l.name }
func (l *LaunchdTarget) Path() string { return l.path }
func (l *LaunchdTarget) Exists() bool {
	_, err := os.Stat(l.path)
	return err == nil
}

// runLaunchctl executes launchctl; tests replace it so they never touch the
// live login session.
var runLaunchctl = func(args ...string) error {
	return exec.Command("launchctl", args...).Run()
}

// Preview returns the command string that would be written to launchd.sh.
func (l *LaunchdTarget) Preview(key string, value string, varType VarType, quote QuoteMode) (string, error) {
	switch varType {
	case VarTypePathAppend, VarTypePathPrepend:
		v, err := encodePosixPathElem(value, quote)
		if err != nil {
			return "", err
		}
		if varType == VarTypePathAppend {
			return fmt.Sprintf(`launchctl setenv PATH "$PATH:%s"`, v), nil
		}
		return fmt.Sprintf(`launchctl setenv PATH "%s:$PATH"`, v), nil
	case VarTypeEval, VarTypeSource, VarTypeFishSet:
		return "", fmt.Errorf("type %s is not supported for launchd", varType)
	default:
		if err := ValidateKey(key); err != nil {
			return "", err
		}
		v, err := encodePosix(value, quote, true)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("launchctl setenv %s %s", key, v), nil
	}
}

func launchdPrefixes(key string) (active, commented string) {
	return fmt.Sprintf("launchctl setenv %s ", key), fmt.Sprintf("# envset: launchctl setenv %s ", key)
}

// Add appends or updates an environment variable in launchd.sh and active session.
func (l *LaunchdTarget) Add(key string, value string, varType VarType, opts WriteOptions) (AddResult, error) {
	newLine, err := l.Preview(key, value, varType, opts.Quote)
	if err != nil {
		return AddResult{}, err
	}
	res := AddResult{Lines: []string{newLine}}

	tf, err := readTextFile(l.path)
	if err != nil {
		return res, err
	}
	if !tf.existed {
		tf.lines = []string{"#!/bin/sh"}
		tf.trailingNL = true
	}

	prefix, commentPrefix := launchdPrefixes(key)
	if varType == VarTypePathAppend || varType == VarTypePathPrepend {
		prefix, commentPrefix = launchdPrefixes("PATH")
	}
	res.Outcome = Appended
	for i, ln := range tf.lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, prefix) || strings.HasPrefix(trimmed, commentPrefix) {
			if trimmed == newLine {
				res.Outcome = Unchanged
			} else {
				res.Outcome = Updated
				res.LineNum = i + 1
				res.Previous = ln
				tf.lines[i] = leadingWS(ln) + newLine
			}
			break
		}
	}
	if res.Outcome == Appended {
		tf.appendLines(newLine)
	}
	if res.Outcome == Unchanged || opts.DryRun {
		return res, nil
	}

	res.Backup, err = CommitFile(l.path, "sh", tf.raw, tf.existed, tf.bytes(), 0o755)
	if err != nil {
		return res, fmt.Errorf("writing launchd script: %w", err)
	}
	if err := l.ensurePlist(); err != nil {
		return res, fmt.Errorf("creating launchd plist agent: %w", err)
	}
	l.applyImmediately(key, value, varType)
	return res, nil
}

// Remove comments out or deletes the variable from launchd.sh and active session.
func (l *LaunchdTarget) Remove(key string, opts RemoveOptions) (RemoveResult, error) {
	if err := ValidateKey(key); err != nil {
		return RemoveResult{}, err
	}
	tf, err := readTextFile(l.path)
	if err != nil {
		return RemoveResult{}, fmt.Errorf("reading launchd script: %w", err)
	}
	if !tf.existed {
		return RemoveResult{}, fmt.Errorf("file does not exist: %s", l.path)
	}

	prefix, commentPrefix := launchdPrefixes(key)
	var res RemoveResult
	out := make([]string, 0, len(tf.lines))
	for i, ln := range tf.lines {
		trimmed := strings.TrimSpace(ln)
		isActive := strings.HasPrefix(trimmed, prefix)
		isCommented := strings.HasPrefix(trimmed, commentPrefix)
		if (isActive || isCommented) && opts.Value != "" && !strings.Contains(trimmed, opts.Value) {
			isActive, isCommented = false, false
		}
		switch {
		case isActive:
			res.Affected = append(res.Affected, Entry{Key: key, RawLine: ln, LineNum: i + 1})
			if opts.Comment {
				out = append(out, leadingWS(ln)+"# envset: "+trimmed)
			}
		case isCommented && opts.Comment:
			res.Skipped++
			out = append(out, ln)
		case isCommented:
			res.Affected = append(res.Affected, Entry{Key: key, RawLine: ln, LineNum: i + 1, IsComment: true})
		default:
			out = append(out, ln)
		}
	}

	if len(res.Affected) == 0 {
		if res.Skipped > 0 {
			return res, nil
		}
		return res, fmt.Errorf("key %q not found in %s", key, l.path)
	}
	if opts.DryRun {
		return res, nil
	}

	tf.lines = out
	res.Backup, err = CommitFile(l.path, "sh", tf.raw, true, tf.bytes(), 0o755)
	if err != nil {
		return res, fmt.Errorf("writing launchd script: %w", err)
	}
	// Unset in the live session too — but never PATH, which would leave GUI
	// apps with no PATH at all until next login.
	if key != "PATH" {
		_ = runLaunchctl("unsetenv", key)
	}
	return res, nil
}

// List parses launchd.sh to list variables.
func (l *LaunchdTarget) List(key string) ([]Entry, error) {
	if !l.Exists() {
		return nil, nil
	}

	lines, err := readLines(l.path)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		isComment := strings.HasPrefix(trimmed, "#")
		rem := trimmed
		if isComment {
			rem = strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			rem = strings.TrimSpace(strings.TrimPrefix(rem, "envset:"))
		}

		if !strings.HasPrefix(rem, "launchctl setenv ") {
			continue
		}

		rem = strings.TrimPrefix(rem, "launchctl setenv ")
		parts := strings.SplitN(rem, " ", 2)
		if len(parts) != 2 {
			continue
		}

		k := parts[0]
		v := parts[1]

		// Unquote value
		v = unquoteValue(v)

		if key != "" && k != key {
			continue
		}

		varType := VarTypeExport
		if k == "PATH" {
			if strings.HasPrefix(v, "$PATH:") {
				varType = VarTypePathAppend
				v = strings.TrimPrefix(v, "$PATH:")
			} else if strings.HasSuffix(v, ":$PATH") {
				varType = VarTypePathPrepend
				v = strings.TrimSuffix(v, ":$PATH")
			}
		}

		entries = append(entries, Entry{
			Key:       k,
			Value:     v,
			VarType:   varType,
			RawLine:   ln,
			LineNum:   i + 1,
			IsComment: isComment,
		})
	}

	return entries, nil
}

// ensurePlist writes the com.envset.environment.plist agent if missing.
func (l *LaunchdTarget) ensurePlist() error {
	if _, err := os.Stat(l.plistPath); err == nil {
		return nil // already exists
	}

	if err := os.MkdirAll(filepath.Dir(l.plistPath), 0755); err != nil {
		return err
	}

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.envset.environment</string>
	<key>ProgramArguments</key>
	<array>
		<string>sh</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, l.path)

	return fsutil.WriteFileAtomic(l.plistPath, []byte(plistContent), 0o644)
}

// applyImmediately runs launchctl setenv to update the active session.
func (l *LaunchdTarget) applyImmediately(key, value string, varType VarType) {
	var runVal string
	switch varType {
	case VarTypePathAppend:
		curr := os.Getenv("PATH")
		runVal = curr + ":" + value
	case VarTypePathPrepend:
		curr := os.Getenv("PATH")
		runVal = value + ":" + curr
	default:
		runVal = value
	}

	// Update our own current process env so it's consistent if queried
	if key == "PATH" || varType == VarTypePathAppend || varType == VarTypePathPrepend {
		key = "PATH"
		_ = os.Setenv("PATH", runVal)
	}

	_ = runLaunchctl("setenv", key, runVal)
}

// Helper functions (defined in shell.go, copy/pasted or reused)
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
