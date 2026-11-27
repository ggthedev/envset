package placer

import (
	"fmt"
	"github.com/experimentalist/envset/pkg/logger"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/experimentalist/envset/pkg/fsutil"
	"github.com/experimentalist/envset/pkg/targets"
)

// Definition is a file's effective (last active) definition of a key.
type Definition struct {
	File ConfigFile
	Line int
	Text string // the trimmed line, without its trailing comment
	// Conditional is true when the line is inside a block or holds more than
	// this definition.
	Conditional bool
}

// Definitions returns, for every registered file that actively defines key,
// its last definition. Commented-out lines do not count. PATH is additive
// ("another entry", never "the existing value"), so it has none.
func (r *Registry) Definitions(key string) []Definition {
	defer logger.Enter()()
	if key == "" || key == "PATH" {
		return nil
	}
	var out []Definition
	for _, f := range r.Files {
		if !f.Exists {
			continue
		}
		t, err := targets.Factory(f.Name, r.Home)
		if err != nil {
			continue
		}
		entries, err := t.List(key)
		if err != nil {
			continue
		}
		for i := len(entries) - 1; i >= 0; i-- {
			if !entries[i].IsComment && !entries[i].Inert {
				text := strings.TrimSuffix(strings.TrimRight(entries[i].RawLine, "\r"), entries[i].Comment)
				out = append(out, Definition{File: f, Line: entries[i].LineNum, Text: strings.TrimSpace(text), Conditional: entries[i].Block || entries[i].Compound})
				break
			}
		}
	}
	return out
}

// loadPos is a point in shell startup: the startup file the shell reads (root)
// and the order within it: root rank, then line numbers down the source chain.
type loadPos struct {
	root  ConfigFile
	order []int
}

// rootRank is the order in which the shell itself reads a startup file.
// ok is false for files the shell only reaches through `source`.
func rootRank(f ConfigFile) (rank int, ok bool) {
	defer logger.Enter()()
	switch f.Name {
	case "launchd": // session environment, in place before any shell starts
		return 0, true
	case "zshenv":
		return 10, true
	case "zprofile", "bash_profile", "profile":
		return 20, true
	case "zshrc", "bashrc", "config_fish":
		return 30, true
	}
	return 0, false
}

// positions returns every way the shell reaches line `line` of f.
func (r *Registry) positions(f ConfigFile, line int, visited map[string]bool) []loadPos {
	defer logger.Enter()()
	if visited[f.Path] {
		return nil
	}
	visited[f.Path] = true
	defer delete(visited, f.Path)

	var out []loadPos
	if rank, ok := rootRank(f); ok && r.shellReads(f) {
		out = append(out, loadPos{root: f, order: []int{rank, line}})
	}
	for _, parentPath := range f.SourcedFrom {
		for _, parent := range r.Files {
			if parent.Path != parentPath {
				continue
			}
			for _, p := range r.positions(parent, f.sourcedAt[parentPath], visited) {
				order := append(slices.Clone(p.order), line)
				out = append(out, loadPos{root: p.root, order: order})
			}
		}
	}
	return out
}

// shellReads reports whether the shell loads this primary file on its own.
// bash login shells read only the first of .bash_profile, .bash_login, .profile.
func (r *Registry) shellReads(f ConfigFile) bool {
	defer logger.Enter()()
	if f.Name != "profile" {
		return true
	}
	for _, n := range []string{".bash_profile", ".bash_login"} {
		if _, err := os.Stat(filepath.Join(r.Home, n)); err == nil {
			return false
		}
	}
	return true
}

// sameShell reports whether one shell process can read both startup files.
// A zsh login terminal reads all zsh files; bash reads .bash_profile or
// .bashrc but not both (unless one sources the other, which shows up as a
// shared root); launchd values are inherited by every shell.
func sameShell(a, b ConfigFile) bool {
	defer logger.Enter()()
	if a.Path == b.Path || a.Name == "launchd" || b.Name == "launchd" {
		return true
	}
	return strings.HasPrefix(a.Name, "z") && strings.HasPrefix(b.Name, "z")
}

// narrower returns whichever scope reaches fewer shells.
func narrower(a, b Scope) Scope {
	defer logger.Enter()()
	if a == ScopeSession {
		return b
	}
	if b == ScopeSession || scopeRank(a) <= scopeRank(b) {
		return a
	}
	return b
}

// Relation says which of two definitions a shell ends up with.
type Relation int

const (
	Unrelated Relation = iota // no shell is known to load both
	NewWins                   // the new line is read later
	OtherWins                 // the other definition is read later
)

// Compare relates a value written to target (at targetLine, or appended when
// targetLine is 0) to another definition. For OtherWins, shells names the
// shells in which the other value is the one used.
func (r *Registry) Compare(target ConfigFile, targetLine int, other Definition) (rel Relation, shells Scope) {
	defer logger.Enter()()
	if targetLine == 0 {
		targetLine = math.MaxInt
	}
	mine := r.positions(target, targetLine, map[string]bool{})
	theirs := r.positions(other.File, other.Line, map[string]bool{})
	for _, t := range theirs {
		for _, m := range mine {
			if !sameShell(m.root, t.root) {
				continue
			}
			if slices.Compare(t.order, m.order) > 0 {
				return OtherWins, narrower(m.root.Scope, t.root.Scope)
			}
			rel = NewWins
		}
	}
	return rel, ScopeUnknown
}

// DuplicateNotes explains, in user terms, how writing key to target interacts
// with the definitions of key in other files.
func (r *Registry) DuplicateNotes(target ConfigFile, key string) []string {
	defer logger.Enter()()
	defs := r.Definitions(key)
	targetLine := 0
	for _, d := range defs {
		if d.File.Path == target.Path {
			targetLine = d.Line // updated in place
		}
	}
	var notes []string
	for _, d := range defs {
		if d.File.Path == target.Path {
			continue
		}
		where, here := fsutil.Tilde(d.File.Path), fsutil.Tilde(target.Path)
		switch rel, shells := r.Compare(target, targetLine, d); rel {
		case OtherWins:
			notes = append(notes, fmt.Sprintf(
				"%s is also set in %s (line %d), which loads after %s: %s will use that value, not this one. To change it there, use --target %s.",
				key, where, d.Line, here, shellsPhrase(shells), d.File.Name))
		case NewWins:
			notes = append(notes, fmt.Sprintf(
				"%s is also set in %s (line %d); the value in %s loads later and takes precedence.",
				key, where, d.Line, here))
		default:
			notes = append(notes, fmt.Sprintf(
				"%s is also set in %s (line %d); envset can't tell whether the same shell loads both.",
				key, where, d.Line))
		}
	}
	return notes
}

func shellsPhrase(s Scope) string {
	defer logger.Enter()()
	switch s {
	case ScopeLogin:
		return "login shells"
	case ScopeInteractive:
		return "interactive shells"
	}
	return "your shells"
}

// StartupFiles returns the existing files the user's shell reads on its own,
// in load order. Files they source are not listed: sourcing the parent loads
// them. launchd.sh is never included (it is not shell startup).
func (r *Registry) StartupFiles() []string {
	defer logger.Enter()()
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	in := func(rel string) string { return filepath.Join(r.Home, rel) }

	var out []string
	add := func(p string) {
		if exists(p) {
			out = append(out, p)
		}
	}
	switch r.Shell {
	case "bash":
		login := ""
		for _, n := range []string{".bash_profile", ".bash_login", ".profile"} {
			if exists(in(n)) {
				login = in(n)
				out = append(out, login)
				break
			}
		}
		if f, err := r.FindByName("bashrc"); err == nil && !slices.Contains(f.SourcedFrom, login) {
			add(f.Path)
		}
	case "fish":
		add(in(filepath.Join(".config", "fish", "config.fish")))
	default: // zsh
		add(in(".zshenv"))
		add(in(".zprofile"))
		add(in(".zshrc"))
	}
	return out
}
