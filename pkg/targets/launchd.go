package targets

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	path := filepath.Join(home, ".config", "envset", "launchd.sh")
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

// Preview returns the command string that would be written to launchd.sh.
func (l *LaunchdTarget) Preview(key string, value string, varType VarType) (string, error) {
	escapedVal := strings.ReplaceAll(value, "\"", "\\\"")
	switch varType {
	case VarTypePathAppend:
		return fmt.Sprintf("launchctl setenv PATH \"$PATH:%s\"", escapedVal), nil
	case VarTypePathPrepend:
		return fmt.Sprintf("launchctl setenv PATH \"%s:$PATH\"", escapedVal), nil
	default:
		return fmt.Sprintf("launchctl setenv %s \"%s\"", key, escapedVal), nil
	}
}

// Add appends or updates an environment variable in launchd.sh and active session.
func (l *LaunchdTarget) Add(key string, value string, varType VarType, dryRun bool) error {
	newLine, err := l.Preview(key, value, varType)
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Printf("[DRY-RUN] Would append/update in %s:\n%s\n", l.path, newLine)
		return nil
	}

	// 1. Ensure directories exist
	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return fmt.Errorf("creating directory for script: %w", err)
	}

	// 2. Read lines of script
	var lines []string
	if l.Exists() {
		lines, err = readLines(l.path)
		if err != nil {
			return fmt.Errorf("reading launchd script: %w", err)
		}
	} else {
		lines = []string{"#!/bin/sh"}
	}

	// 3. Find and replace or append
	found := false
	prefix := fmt.Sprintf("launchctl setenv %s ", key)
	commentPrefix := fmt.Sprintf("# envset: launchctl setenv %s ", key)

	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, prefix) || strings.HasPrefix(trimmed, commentPrefix) {
			// Keep indentation if any
			indent := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
			lines[i] = indent + newLine
			found = true
			break
		}
	}

	if !found {
		lines = append(lines, newLine)
	}

	// 4. Write script back
	if err := writeLines(l.path, lines); err != nil {
		return fmt.Errorf("writing launchd script: %w", err)
	}
	_ = os.Chmod(l.path, 0755) // Ensure executable

	// 5. Ensure plist exists
	if err := l.ensurePlist(); err != nil {
		return fmt.Errorf("creating launchd plist agent: %w", err)
	}

	// 6. Apply to the active launchd session immediately
	l.applyImmediately(key, value, varType)

	return nil
}

// Remove comments out or deletes the variable from launchd.sh and active session.
func (l *LaunchdTarget) Remove(key string, comment bool, dryRun bool) error {
	if !l.Exists() {
		return fmt.Errorf("file does not exist: %s", l.path)
	}

	if dryRun {
		fmt.Printf("[DRY-RUN] Would remove key %q from %s (comment=%v)\n", key, l.path, comment)
		return nil
	}

	lines, err := readLines(l.path)
	if err != nil {
		return fmt.Errorf("reading launchd script: %w", err)
	}

	prefix := fmt.Sprintf("launchctl setenv %s ", key)
	found := false
	var out []string

	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, prefix) {
			found = true
			if comment {
				indent := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
				out = append(out, indent+"# envset: "+trimmed)
			}
			// if comment is false, we just skip (delete) it
		} else {
			out = append(out, ln)
		}
	}

	if !found {
		return fmt.Errorf("key %q not found in %s", key, l.path)
	}

	if err := writeLines(l.path, out); err != nil {
		return fmt.Errorf("writing launchd script: %w", err)
	}

	// Remove from the active session immediately
	cmd := exec.Command("launchctl", "unsetenv", key)
	_ = cmd.Run()

	return nil
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
		if strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"") {
			v = v[1 : len(v)-1]
			v = strings.ReplaceAll(v, "\\\"", "\"")
		}

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

	return os.WriteFile(l.plistPath, []byte(plistContent), 0644)
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
	if key == "PATH" {
		_ = os.Setenv("PATH", runVal)
	}

	cmd := exec.Command("launchctl", "setenv", key, runVal)
	_ = cmd.Run()
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

func writeLines(path string, lines []string) error {
	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") && len(lines) > 0 {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0644)
}
