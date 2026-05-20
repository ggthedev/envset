package targets

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ShellTarget manages a shell configuration file.
type ShellTarget struct {
	name    string
	path    string
	shell   string // zsh, bash, fish
	exists  bool
}

// NewShellTarget creates a shell target by name and home directory.
func NewShellTarget(name, home string) (*ShellTarget, error) {
	var path string
	var shell string

	switch name {
	case "zshrc":
		path = filepath.Join(home, ".zshrc")
		shell = "zsh"
	case "zprofile":
		path = filepath.Join(home, ".zprofile")
		shell = "zsh"
	case "zshenv":
		path = filepath.Join(home, ".zshenv")
		shell = "zsh"
	case "bashrc":
		path = filepath.Join(home, ".bashrc")
		shell = "bash"
	case "bash_profile":
		path = filepath.Join(home, ".bash_profile")
		shell = "bash"
	case "profile":
		path = filepath.Join(home, ".profile")
		shell = "bash"
	case "bash_login":
		path = filepath.Join(home, ".bash_login")
		shell = "bash"
	case "config_fish":
		path = filepath.Join(home, ".config", "fish", "config.fish")
		shell = "fish"
	case "gg_system_exports":
		path = filepath.Join(home, ".gg_system_exports")
		shell = "zsh"
	case "gg_system_interactive":
		path = filepath.Join(home, ".gg_system_interactive")
		shell = "zsh"
	default:
		return nil, fmt.Errorf("unsupported shell target: %s", name)
	}

	_, err := os.Stat(path)
	exists := err == nil

	return &ShellTarget{
		name:   name,
		path:   path,
		shell:  shell,
		exists: exists,
	}, nil
}

func (s *ShellTarget) Name() string { return s.name }
func (s *ShellTarget) Path() string  { return s.path }
func (s *ShellTarget) Exists() bool { return s.exists }

// Add inserts a new variable entry. If dryRun is true, no file is modified.
func (s *ShellTarget) Add(key, value string, varType VarType, dryRun bool) error {
	line, err := s.formatLine(key, value, varType)
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Printf("[DRY-RUN] Would append to %s:\n%s\n", s.path, line)
		return nil
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", s.path, err)
	}
	defer f.Close()

	// Add a newline before the entry if file is non-empty and doesn't end with newline.
	stat, err := os.Stat(s.path)
	if err == nil && stat.Size() > 0 {
		// Read last byte to check for trailing newline
		f2, err := os.Open(s.path)
		if err == nil {
			f2.Seek(-1, os.SEEK_END)
			buf := make([]byte, 1)
			f2.Read(buf)
			f2.Close()
			if buf[0] != '\n' {
				f.WriteString("\n")
			}
		}
	}

	_, err = f.WriteString(line + "\n")
	if err != nil {
		return fmt.Errorf("failed to write to %s: %w", s.path, err)
	}

	s.exists = true
	return nil
}

// Remove deletes or comments out entries matching key.
func (s *ShellTarget) Remove(key string, comment bool, dryRun bool) error {
	if !s.exists {
		return fmt.Errorf("file does not exist: %s", s.path)
	}

	entries, err := s.List(key)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		return fmt.Errorf("no entries found for key %q in %s", key, s.path)
	}

	if dryRun {
		action := "delete"
		if comment {
			action = "comment out"
		}
		fmt.Printf("[DRY-RUN] Would %s %d line(s) in %s\n", action, len(entries), s.path)
		for _, e := range entries {
			fmt.Printf("  line %d: %s\n", e.LineNum, strings.TrimSpace(e.RawLine))
		}
		return nil
	}

	lines, err := s.readAllLines()
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.LineNum < 1 || e.LineNum > len(lines) {
			continue
		}
		if comment {
			lines[e.LineNum-1] = "# " + lines[e.LineNum-1]
		} else {
			lines[e.LineNum-1] = ""
		}
	}

	// Write back, filtering out empty lines for deletion case
	var out []string
	for _, l := range lines {
		if l != "" || comment {
			out = append(out, l)
		}
	}

	content := strings.Join(out, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	return os.WriteFile(s.path, []byte(content), 0644)
}

// List returns entries matching key (empty key returns all).
func (s *ShellTarget) List(key string) ([]Entry, error) {
	if !s.exists {
		return nil, nil
	}

	f, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", s.path, err)
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		entry := s.parseLine(line, lineNum)
		if key == "" || entry.Key == key {
			entries = append(entries, entry)
		}
	}

	return entries, scanner.Err()
}

func (s *ShellTarget) Preview(key, value string, varType VarType) (string, error) {
	return s.formatLine(key, value, varType)
}

func (s *ShellTarget) readAllLines() ([]string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(data), "\n"), nil
}

func (s *ShellTarget) formatLine(key, value string, varType VarType) (string, error) {
	switch varType {
	case VarTypeExport:
		return fmt.Sprintf("export %s=%s", key, s.quoteValue(value)), nil
	case VarTypeEval:
		return fmt.Sprintf("eval %s", value), nil
	case VarTypeSource:
		return fmt.Sprintf("source %s", value), nil
	case VarTypePathAppend:
		return fmt.Sprintf("export PATH=\"$PATH:%s\"", value), nil
	case VarTypePathPrepend:
		return fmt.Sprintf("export PATH=\"%s:$PATH\"", value), nil
	case VarTypeFishSet:
		if s.shell != "fish" {
			return "", fmt.Errorf("fish_set is only valid for fish shell configs")
		}
		return fmt.Sprintf("set -x %s %s", key, value), nil
	default:
		return "", fmt.Errorf("unsupported var type %q", varType)
	}
}

func (s *ShellTarget) quoteValue(v string) string {
	// Simple quoting: if value contains spaces or special chars, use double quotes.
	if strings.ContainsAny(v, " \\t\n\r\"'$&|;<>") {
		return fmt.Sprintf("\"%s\"", strings.ReplaceAll(v, "\"", "\\\""))
	}
	return v
}

func (s *ShellTarget) parseLine(line string, lineNum int) Entry {
	entry := Entry{
		RawLine: line,
		LineNum: lineNum,
	}

	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		entry.IsComment = true
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
	}

	// Try export KEY=value
	if strings.HasPrefix(trimmed, "export ") {
		rest := strings.TrimPrefix(trimmed, "export ")
		if idx := strings.Index(rest, "="); idx > 0 {
			entry.Key = rest[:idx]
			entry.Value = strings.Trim(strings.TrimSpace(rest[idx+1:]), `"`)
			entry.VarType = VarTypeExport
		}
		return entry
	}

	// Try set -x KEY value (fish)
	if strings.HasPrefix(trimmed, "set -x ") {
		parts := strings.Fields(trimmed)
		if len(parts) >= 3 {
			entry.Key = parts[2]
			if len(parts) >= 4 {
				entry.Value = strings.Join(parts[3:], " ")
			}
			entry.VarType = VarTypeFishSet
		}
		return entry
	}

	// Try eval ...
	if strings.HasPrefix(trimmed, "eval ") {
		entry.VarType = VarTypeEval
		entry.Value = strings.TrimSpace(strings.TrimPrefix(trimmed, "eval "))
		return entry
	}

	// Try source ...
	if strings.HasPrefix(trimmed, "source ") {
		entry.VarType = VarTypeSource
		entry.Value = strings.TrimSpace(strings.TrimPrefix(trimmed, "source "))
		return entry
	}

	// Try PATH=...
	if strings.HasPrefix(trimmed, "PATH=") || strings.HasPrefix(trimmed, "export PATH=") {
		entry.VarType = VarTypePathAppend
		return entry
	}

	return entry
}
