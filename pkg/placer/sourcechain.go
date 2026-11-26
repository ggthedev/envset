package placer

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// sourceLineRe matches `source <path>` and `. <path>` directives.
// Group 1 is the referenced path (still possibly using ~ or $HOME).
var sourceLineRe = regexp.MustCompile(`^\s*(?:source|\.)\s+(\S+)`)

// BuildSourceChain scans every existing file in the registry for `source <path>`
// directives and populates two derived fields on each ConfigFile:
//
//   - SourcedFrom: absolute paths of registered files that source THIS file.
//   - EffectiveScope: the broadest scope that actually reaches this file in
//     the shell-startup graph.
//
// EffectiveScope equals the declared Scope when a file is loaded directly by
// the shell (e.g. .zshenv, .zshrc, .zprofile) or is not sourced from anywhere
// the registry knows about. It diverges when, for example, ~/.gg_system_exports
// is declared all_sessions but is only sourced from .zprofile (login scope) —
// in that case EffectiveScope is login, signalling a broken source-chain.
func (r *Registry) BuildSourceChain() {
	// Index files by absolute path for O(1) lookup.
	byPath := map[string]*ConfigFile{}
	for i := range r.Files {
		byPath[r.Files[i].Path] = &r.Files[i]
	}

	// Reset any previously computed data so this method is idempotent.
	for i := range r.Files {
		r.Files[i].SourcedFrom = nil
		r.Files[i].sourcedAt = nil
		r.Files[i].EffectiveScope = ""
	}

	// Pass 1: parse `source` lines and record reverse edges (parent → child).
	for i := range r.Files {
		parent := &r.Files[i]
		if !parent.Exists {
			continue
		}
		refs, err := parseSourceLines(parent.Path, r.Home)
		if err != nil {
			continue
		}
		for _, ref := range refs {
			if child, ok := byPath[ref.path]; ok {
				if child.sourcedAt == nil {
					child.sourcedAt = map[string]int{}
				}
				if _, seen := child.sourcedAt[parent.Path]; !seen {
					child.SourcedFrom = append(child.SourcedFrom, parent.Path)
				}
				child.sourcedAt[parent.Path] = ref.line
			}
		}
	}

	// Pass 2: compute EffectiveScope by walking the source-chain ancestors.
	// Use a fresh visited set per file so independent walks don't interfere.
	for i := range r.Files {
		r.Files[i].EffectiveScope = computeEffectiveScope(&r.Files[i], byPath, map[string]bool{})
	}
}

// sourceRef is one `source <path>` directive and the line it is on.
type sourceRef struct {
	path string
	line int
}

// parseSourceLines reads a file once and returns every absolute path
// referenced by a `source <path>` or `. <path>` directive (with ~ / $HOME
// expanded). Comment lines are ignored.
func parseSourceLines(path, home string) ([]sourceRef, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []sourceRef
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		trim := strings.TrimSpace(sc.Text())
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		m := sourceLineRe.FindStringSubmatch(trim)
		if m == nil {
			continue
		}
		ref := strings.Trim(m[1], `"';`)
		out = append(out, sourceRef{path: expandHomePath(ref, home), line: line})
	}
	return out, sc.Err()
}

// computeEffectiveScope returns the broadest scope from which f is reachable.
// For files not sourced from anything (or sourced only from unknown files),
// the declared Scope is used. visited prevents infinite recursion on cycles.
func computeEffectiveScope(f *ConfigFile, byPath map[string]*ConfigFile, visited map[string]bool) Scope {
	if visited[f.Path] {
		return f.Scope // cycle protection
	}
	visited[f.Path] = true

	if len(f.SourcedFrom) == 0 {
		return f.Scope
	}

	best := ScopeUnknown
	for _, parentPath := range f.SourcedFrom {
		parent, ok := byPath[parentPath]
		if !ok {
			continue
		}
		parentEff := computeEffectiveScope(parent, byPath, visited)
		if scopeBroaderOrEqual(parentEff, best) {
			best = parentEff
		}
	}
	if best == ScopeUnknown {
		return f.Scope
	}
	return best
}

// scopeRank orders scopes from narrowest (1) to broadest (3). Higher = reaches
// more shells. Used by the placer to penalise broken source-chains and by the
// audit pass to flag scope mismatches.
func scopeRank(s Scope) int {
	switch s {
	case ScopeInteractive:
		return 1
	case ScopeLogin:
		return 2
	case ScopeAllSessions:
		return 3
	}
	return 0
}

// scopeBroaderOrEqual returns true if a reaches at least as many shells as b.
func scopeBroaderOrEqual(a, b Scope) bool {
	return scopeRank(a) >= scopeRank(b)
}

// scopeBroader returns true if a reaches strictly more shells than b.
func scopeBroader(a, b Scope) bool {
	return scopeRank(a) > scopeRank(b)
}

// expandHomePath expands a leading "~/" or "$HOME/" to the supplied home dir.
func expandHomePath(p, home string) string {
	if home == "" {
		return p
	}
	switch {
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return home + p[1:]
	case strings.HasPrefix(p, "$HOME/"):
		return home + p[5:]
	}
	return p
}
