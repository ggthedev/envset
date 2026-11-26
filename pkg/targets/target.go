package targets

import (
	"fmt"
	"path/filepath"
	"strings"
)

// VarType represents how a variable should be set.
type VarType string

const (
	VarTypeExport              VarType = "export"
	VarTypeExportAndPathAppend VarType = "export_and_path_append"
	VarTypeEval                VarType = "eval"
	VarTypeSource              VarType = "source"
	VarTypePathAppend          VarType = "path_append"
	VarTypePathPrepend         VarType = "path_prepend"
	VarTypeFishSet             VarType = "fish_set" // set -x for fish
)

// Entry represents a single line entry in a config file.
type Entry struct {
	Key       string
	Value     string
	VarType   VarType
	RawLine   string
	LineNum   int
	IsComment bool
}

// WriteOptions controls Add.
type WriteOptions struct {
	DryRun bool      // compute the change but do not write
	Quote  QuoteMode // how the value is encoded (default: QuoteExpand)
}

// RemoveOptions controls Remove.
type RemoveOptions struct {
	Comment bool   // comment the line(s) out instead of deleting them
	DryRun  bool   // compute the change but do not write
	Value   string // only match entries whose value contains this (required for PATH)
}

// AddOutcome says what Add did (or would do, in dry-run).
type AddOutcome string

const (
	Appended  AddOutcome = "appended"  // new line(s) added at the end
	Updated   AddOutcome = "updated"   // an existing definition was replaced in place
	Unchanged AddOutcome = "unchanged" // identical definition already present; nothing written
)

// AddResult describes the change made by Add.
type AddResult struct {
	Outcome  AddOutcome
	Lines    []string // the line(s) written
	LineNum  int      // for Updated: 1-indexed line that was replaced
	Previous string   // for Updated: the line that was replaced
	Backup   string   // backup of the previous file contents ("" if none / dry-run)
}

// RemoveResult describes the change made by Remove.
type RemoveResult struct {
	Affected []Entry // lines deleted or commented out
	Skipped  int     // matching lines that were already commented out (comment mode)
	Backup   string  // backup of the previous file contents ("" if none / dry-run)
}

// Target defines the interface for configuration file targets.
type Target interface {
	Name() string
	Path() string
	Exists() bool
	// Add inserts or updates an entry. Adding an identical entry is a no-op.
	Add(key string, value string, varType VarType, opts WriteOptions) (AddResult, error)
	// Remove deletes (or comments out) the entries for key.
	Remove(key string, opts RemoveOptions) (RemoveResult, error)
	// List returns all entries matching the given key (empty means all).
	List(key string) ([]Entry, error)
	// Preview returns what would be written (may span several lines).
	Preview(key string, value string, varType VarType, quote QuoteMode) (string, error)
}

// Factory returns a Target by name.
func Factory(name string, home string) (Target, error) {
	switch name {
	case "zshrc", "bashrc", "bash_profile", "zprofile", "profile", "zshenv", "bash_login", "config_fish", "gg_system_exports", "gg_system_interactive":
		return NewShellTarget(name, home)
	case "launchd":
		return NewLaunchdTarget(home)
	default:
		// Try custom path resolution if it looks like a path
		if strings.Contains(name, "/") || strings.Contains(name, ".") || strings.Contains(name, "\\") {
			path := name
			if strings.HasPrefix(path, "~/") {
				path = filepath.Join(home, path[2:])
			} else if strings.HasPrefix(path, "$HOME/") {
				path = filepath.Join(home, path[5:])
			}
			shell := "zsh"
			if strings.HasSuffix(path, "config.fish") || strings.HasSuffix(path, ".fish") {
				shell = "fish"
			}
			return &ShellTarget{
				name:  filepath.Base(path),
				path:  path,
				shell: shell,
			}, nil
		}
		return nil, fmt.Errorf("unknown target %q (known: %s, launchd, or a file path)", name, strings.Join(AllShellTargets(), ", "))
	}
}

// AllShellTargets returns all supported shell target names.
func AllShellTargets() []string {
	return []string{
		"zshrc", "bashrc", "bash_profile", "zprofile", "profile", "zshenv", "bash_login", "config_fish",
	}
}

// VarTypeFromString parses a var type from string.
func VarTypeFromString(s string) (VarType, error) {
	switch s {
	case "export", "":
		return VarTypeExport, nil
	case "export_and_path_append":
		return VarTypeExportAndPathAppend, nil
	case "eval":
		return VarTypeEval, nil
	case "source":
		return VarTypeSource, nil
	case "path_append":
		return VarTypePathAppend, nil
	case "path_prepend":
		return VarTypePathPrepend, nil
	case "fish_set":
		return VarTypeFishSet, nil
	default:
		return "", fmt.Errorf("invalid var type %q", s)
	}
}

// VarTypeDescription returns a human-readable description.
func VarTypeDescription(vt VarType) string {
	switch vt {
	case VarTypeExport:
		return "export FOO=bar"
	case VarTypeExportAndPathAppend:
		return "export FOO=/dir, PATH=$PATH:$FOO"
	case VarTypeEval:
		return "eval $(command)"
	case VarTypeSource:
		return "source /path/to/file"
	case VarTypePathAppend:
		return "PATH=$PATH:/new/path"
	case VarTypePathPrepend:
		return "PATH=/new/path:$PATH"
	case VarTypeFishSet:
		return "set -x FOO bar (fish shell)"
	default:
		return string(vt)
	}
}
