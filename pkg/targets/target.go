package targets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VarType represents how a variable should be set.
type VarType string

const (
	VarTypeExport     VarType = "export"
	VarTypeEval       VarType = "eval"
	VarTypeSource     VarType = "source"
	VarTypePathAppend VarType = "path_append"
	VarTypePathPrepend VarType = "path_prepend"
	VarTypeFishSet    VarType = "fish_set" // set -x for fish
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

// Target defines the interface for configuration file targets.
// TBD/FUTURE: dotenv, launchd, IDE configs.
type Target interface {
	Name() string
	Path() string
	Exists() bool
	// Add inserts a new entry. dryRun controls whether to write.
	Add(key string, value string, varType VarType, dryRun bool) error
	// Remove deletes an entry by key. If comment is true, it comments out instead.
	Remove(key string, comment bool, dryRun bool) error
	// List returns all entries matching the given key (empty means all).
	List(key string) ([]Entry, error)
	// Preview returns what would be written.
	Preview(key string, value string, varType VarType) (string, error)
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
			_, err := os.Stat(path)
			exists := err == nil
			return &ShellTarget{
				name:   filepath.Base(path),
				path:   path,
				shell:  shell,
				exists: exists,
			}, nil
		}
		// TBD/FUTURE: dotenv, launchd, IDE configs not yet implemented
		return nil, fmt.Errorf("unknown target %q (TBD: dotenv, launchd, IDE configs not yet implemented)", name)
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
