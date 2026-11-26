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
	name  string
	path  string
	shell string // zsh, bash, fish
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

	return &ShellTarget{name: name, path: path, shell: shell}, nil
}

func (s *ShellTarget) Name() string { return s.name }
func (s *ShellTarget) Path() string { return s.path }
func (s *ShellTarget) Exists() bool {
	_, err := os.Stat(s.path)
	return err == nil
}

// keyed reports whether entries of this type are identified by their key, so
// adding the same key again updates the existing line instead of appending.
// PATH is never keyed: replacing someone's PATH line would drop their entries.
func keyed(key string, vt VarType) bool {
	if key == "PATH" {
		return false
	}
	switch vt {
	case VarTypeExport, VarTypeExportAndPathAppend, VarTypeFishSet:
		return true
	}
	return false
}

// companionPathLines are the forms of the PATH line written by
// export_and_path_append for key.
func companionPathLines(key string) []string {
	return []string{
		fmt.Sprintf(`export PATH="$PATH:$%s"`, key),
		fmt.Sprintf(`export PATH="$PATH:${%s}"`, key),
	}
}

func isCompanion(trimmed, key string) bool {
	for _, c := range companionPathLines(key) {
		if trimmed == c {
			return true
		}
	}
	return false
}

// Add inserts a new entry, updates an existing definition of the same key in
// place, or does nothing if an identical line is already active.
func (s *ShellTarget) Add(key, value string, varType VarType, opts WriteOptions) (AddResult, error) {
	newLines, err := s.formatLines(key, value, varType, opts.Quote)
	if err != nil {
		return AddResult{}, err
	}
	tf, err := readTextFile(s.path)
	if err != nil {
		return AddResult{}, err
	}
	res := AddResult{Lines: newLines}

	active := func(i int) (Entry, string) {
		e := s.parseLine(tf.lines[i], i+1)
		return e, strings.TrimSpace(tf.lines[i])
	}
	hasActiveLine := func(want string) bool {
		for i := range tf.lines {
			if e, t := active(i); !e.IsComment && t == want {
				return true
			}
		}
		return false
	}

	if keyed(key, varType) {
		last := -1
		for i := range tf.lines {
			if e, _ := active(i); !e.IsComment && e.Key == key && e.VarType == entryTypeFor(varType) {
				last = i // the last definition is the one that wins
			}
		}
		switch {
		case last < 0:
			tf.appendLines(newLines...)
			res.Outcome = Appended
		case strings.TrimSpace(tf.lines[last]) == newLines[0]:
			res.Outcome = Unchanged
		default:
			res.Outcome = Updated
			res.LineNum = last + 1
			res.Previous = tf.lines[last]
			tf.lines[last] = leadingWS(tf.lines[last]) + newLines[0]
		}
		// export_and_path_append: make sure the companion PATH line exists.
		if varType == VarTypeExportAndPathAppend && res.Outcome != Appended {
			if !hasActiveLine(newLines[1]) && !hasActiveLine(companionPathLines(key)[1]) {
				idx := last + 1
				tf.lines = append(tf.lines[:idx], append([]string{newLines[1]}, tf.lines[idx:]...)...)
				if res.Outcome == Unchanged {
					res.Outcome = Updated
					res.LineNum = idx + 1
				}
			}
		}
	} else {
		all := true
		for _, l := range newLines {
			if !hasActiveLine(l) {
				all = false
			}
		}
		if all {
			res.Outcome = Unchanged
		} else {
			tf.appendLines(newLines...)
			res.Outcome = Appended
		}
	}

	if res.Outcome == Unchanged || opts.DryRun {
		return res, nil
	}
	res.Backup, err = CommitFile(s.path, s.shell, tf.raw, tf.existed, tf.bytes(), 0o644)
	return res, err
}

// entryTypeFor maps a write type to the type parseLine reports for its line.
func entryTypeFor(vt VarType) VarType {
	if vt == VarTypeExportAndPathAppend {
		return VarTypeExport
	}
	return vt
}

// Remove deletes or comments out entries for key. Only the matched lines are
// touched; the rest of the file is preserved byte-for-byte.
func (s *ShellTarget) Remove(key string, opts RemoveOptions) (RemoveResult, error) {
	if err := ValidateKey(key); err != nil {
		return RemoveResult{}, err
	}
	if key == "PATH" && opts.Value == "" {
		return RemoveResult{}, fmt.Errorf("refusing to remove every PATH line in %s; pass --value <dir> to pick which entry", s.path)
	}
	tf, err := readTextFile(s.path)
	if err != nil {
		return RemoveResult{}, err
	}
	if !tf.existed {
		return RemoveResult{}, fmt.Errorf("file does not exist: %s", s.path)
	}

	var res RemoveResult
	hit := map[int]bool{}
	for i, line := range tf.lines {
		e := s.parseLine(line, i+1)
		match := e.Key == key && (opts.Value == "" || strings.Contains(e.Value, opts.Value))
		if !match && key != "PATH" {
			t := strings.TrimSpace(line)
			if e.IsComment {
				t = strings.TrimSpace(strings.TrimPrefix(t, "#"))
			}
			match = isCompanion(t, key)
		}
		if !match {
			continue
		}
		if opts.Comment && e.IsComment {
			res.Skipped++
			continue
		}
		hit[i] = true
		res.Affected = append(res.Affected, e)
	}

	if len(res.Affected) == 0 {
		if res.Skipped > 0 {
			return res, nil // already commented out: nothing to do
		}
		if opts.Value != "" {
			return res, fmt.Errorf("no entries for %q containing %q in %s", key, opts.Value, s.path)
		}
		return res, fmt.Errorf("no entries found for key %q in %s", key, s.path)
	}
	if opts.DryRun {
		return res, nil
	}

	out := make([]string, 0, len(tf.lines))
	for i, line := range tf.lines {
		switch {
		case !hit[i]:
			out = append(out, line)
		case opts.Comment:
			out = append(out, leadingWS(line)+"# "+strings.TrimLeft(line, " \t"))
		}
	}
	tf.lines = out
	res.Backup, err = CommitFile(s.path, s.shell, tf.raw, true, tf.bytes(), 0o644)
	return res, err
}

// List returns entries matching key (empty key returns all).
func (s *ShellTarget) List(key string) ([]Entry, error) {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", s.path, err)
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		entry := s.parseLine(scanner.Text(), lineNum)
		if key == "" || entry.Key == key {
			entries = append(entries, entry)
		}
	}
	return entries, scanner.Err()
}

func (s *ShellTarget) Preview(key, value string, varType VarType, quote QuoteMode) (string, error) {
	lines, err := s.formatLines(key, value, varType, quote)
	if err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

func (s *ShellTarget) formatLines(key, value string, varType VarType, quote QuoteMode) ([]string, error) {
	needKey := varType == VarTypeExport || varType == VarTypeExportAndPathAppend || varType == VarTypeFishSet
	if needKey {
		if err := ValidateKey(key); err != nil {
			return nil, err
		}
	}
	switch varType {
	case VarTypeExport:
		v, err := encodePosix(value, quote, false)
		if err != nil {
			return nil, err
		}
		return []string{fmt.Sprintf("export %s=%s", key, v)}, nil
	case VarTypeExportAndPathAppend:
		v, err := encodePosix(value, quote, false)
		if err != nil {
			return nil, err
		}
		return []string{fmt.Sprintf("export %s=%s", key, v), companionPathLines(key)[0]}, nil
	case VarTypeEval:
		// eval's value is a command by definition (e.g. "$(rbenv init -)").
		if err := checkNoNewline(value); err != nil {
			return nil, err
		}
		return []string{"eval " + value}, nil
	case VarTypeSource:
		v, err := encodePosix(value, quote, false)
		if err != nil {
			return nil, err
		}
		return []string{"source " + v}, nil
	case VarTypePathAppend, VarTypePathPrepend:
		v, err := encodePosixPathElem(value, quote)
		if err != nil {
			return nil, err
		}
		if varType == VarTypePathAppend {
			return []string{fmt.Sprintf(`export PATH="$PATH:%s"`, v)}, nil
		}
		return []string{fmt.Sprintf(`export PATH="%s:$PATH"`, v)}, nil
	case VarTypeFishSet:
		if s.shell != "fish" {
			return nil, fmt.Errorf("fish_set is only valid for fish shell configs")
		}
		v, err := encodeFish(value, quote)
		if err != nil {
			return nil, err
		}
		return []string{fmt.Sprintf("set -x %s %s", key, v)}, nil
	default:
		return nil, fmt.Errorf("unsupported var type %q", varType)
	}
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
			entry.Value = unquoteValue(rest[idx+1:])
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
				entry.Value = unquoteValue(strings.Join(parts[3:], " "))
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

	return entry
}
