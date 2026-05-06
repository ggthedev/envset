package audit

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/experimentalist/envset/pkg/placer"
)

// Auditor runs a series of checks against a registry of config files.
type Auditor struct {
	Registry *placer.Registry
}

// New creates an auditor backed by the provided registry.
func New(reg *placer.Registry) *Auditor {
	return &Auditor{Registry: reg}
}

// Run executes all checks against every existing file in the registry and
// returns a consolidated Report.
func (a *Auditor) Run() Report {
	var findings []Finding

	// Read every file once into memory keyed by path.
	files := map[string][]string{}
	blockCtxs := map[string]*BlockContext{}
	for _, f := range a.Registry.Files {
		if !f.Exists {
			continue
		}
		lines, err := readLines(f.Path)
		if err != nil {
			continue
		}
		files[f.Path] = lines
		blockCtxs[f.Path] = NewBlockContext(lines)
	}

	// Run per-file checks
	for _, cf := range a.Registry.Files {
		if !cf.Exists {
			continue
		}
		lines := files[cf.Path]
		bctx := blockCtxs[cf.Path]

		findings = append(findings, checkBrokenPath(cf.Path, lines)...)
		findings = append(findings, checkMisplacedPathExports(cf, lines)...)
		findings = append(findings, checkSourceChainMismatch(cf, lines, a.Registry)...)
		findings = append(findings, checkDuplicateCompinit(cf, lines)...)
		findings = append(findings, checkSlowInteractiveInits(cf, lines, bctx)...)
	}

	// Cross-file checks
	findings = append(findings, checkDuplicateKeysAcrossFiles(a.Registry, files)...)

	return Report{Findings: findings}
}

// ---------- Individual checks ----------

// checkBrokenPath flags PATH= assignments that nest unquoted echo or have
// obviously broken quoting that silently corrupts $PATH.
func checkBrokenPath(path string, lines []string) []Finding {
	var out []Finding
	// Pattern: PATH="echo ... — literal echo prepended to PATH
	echoBug := regexp.MustCompile(`(?:export\s+)?PATH\s*=\s*"\s*echo\s+`)
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "#") {
			continue
		}
		if echoBug.MatchString(trim) {
			out = append(out, Finding{
				Severity:   SevError,
				File:       path,
				Line:       i + 1,
				Code:       trim,
				Title:      "Broken PATH assignment",
				Detail:     "The literal word 'echo' is being prepended to PATH because of nested unescaped double quotes. Every shell sourcing this file silently corrupts $PATH.",
				Suggestion: `Replace with a working form, e.g.: export PATH="$(dirname "$(which node)"):$PATH"  — or remove this line if Volta/fnm already manages node on PATH.`,
				Fix:        &FindingFix{Edits: []FixEdit{{File: path, Line: i + 1}}},
			})
		}
	}
	return out
}

// checkMisplacedPathExports flags PATH-mutating exports placed in interactive-only files.
func checkMisplacedPathExports(cf placer.ConfigFile, lines []string) []Finding {
	if cf.Scope != placer.ScopeInteractive {
		return nil
	}
	var out []Finding
	pathMutate := regexp.MustCompile(`(?:export\s+)?PATH\s*=\s*"[^"]*\$PATH[^"]*"`)
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		// Skip commented lines
		if strings.HasPrefix(trim, "#") {
			continue
		}
		if pathMutate.MatchString(trim) {
			out = append(out, Finding{
				Severity:   SevWarn,
				File:       cf.Path,
				Line:       i + 1,
				Code:       trim,
				Title:      "PATH export in interactive-only file",
				Detail:     fmt.Sprintf("This PATH modification only affects interactive shells. Scripts, cron jobs, and GUI app shell-outs run from %s won't see it.", cf.Name),
				Suggestion: "Move to ~/.zshenv (or to ~/.gg_system_exports once that file is sourced from .zshenv).",
			})
		}
	}
	return out
}

// checkSourceChainMismatch flags `source <other-file>` lines where the sourced
// file is registered with a broader scope than the sourcing file
// (e.g. all-sessions content sourced from a login-only file).
func checkSourceChainMismatch(cf placer.ConfigFile, lines []string, reg *placer.Registry) []Finding {
	var out []Finding
	sourceLine := regexp.MustCompile(`^\s*(?:source|\.)\s+(\S+)`)
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "#") {
			continue
		}
		m := sourceLine.FindStringSubmatch(trim)
		if m == nil {
			continue
		}
		referenced := expandHome(m[1], reg.Home)
		// Match against registry by absolute path
		for _, f := range reg.Files {
			if f.Path != referenced {
				continue
			}
			if scopeBroaderThan(f.Scope, cf.Scope) {
				out = append(out, Finding{
					Severity: SevWarn,
					File:     cf.Path,
					Line:     i + 1,
					Code:     trim,
					Title:    "Sourced file scope is narrower than its content's intent",
					Detail: fmt.Sprintf(
						"%s is meant for %s scope, but it is sourced from %s which is %s scope. Variables in %s won't reach non-%s shells.",
						f.Name, f.Scope, cf.Name, cf.Scope, f.Name, cf.Scope,
					),
					Suggestion: fmt.Sprintf("Move `source %s` from %s into ~/.zshenv so its variables reach every shell.", m[1], cf.Name),
				})
			}
		}
	}
	return out
}

// checkDuplicateCompinit flags multiple compinit invocations in the same file.
func checkDuplicateCompinit(cf placer.ConfigFile, lines []string) []Finding {
	if cf.Scope != placer.ScopeInteractive {
		return nil
	}
	var hits []int
	compinit := regexp.MustCompile(`(?:^|[^[:alnum:]_])compinit(?:\s|$|\s+-)`)
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, "autoload") {
			continue
		}
		if compinit.MatchString(ln) {
			hits = append(hits, i+1)
		}
	}
	// Also account for OMZ implicitly running compinit
	hasOMZ := false
	for _, ln := range lines {
		if strings.Contains(ln, "oh-my-zsh.sh") {
			hasOMZ = true
			break
		}
	}
	threshold := 1
	if hasOMZ {
		threshold = 0 // OMZ already runs compinit; any additional call is a dup
	}
	if len(hits) > threshold {
		first := hits[0]
		// Build edits for the extra compinit hits that should be commented out.
		var compFix *FindingFix
		if extraHits := hits[threshold:]; len(extraHits) > 0 {
			edits := make([]FixEdit, len(extraHits))
			for k, lineNo := range extraHits {
				edits[k] = FixEdit{File: cf.Path, Line: lineNo}
			}
			compFix = &FindingFix{Edits: edits}
		}
		out := []Finding{{
			Severity: SevWarn,
			File:     cf.Path,
			Line:     first,
			Code:     "compinit",
			Title:    "Duplicate compinit invocation",
			Detail: fmt.Sprintf(
				"compinit runs %d time(s) at line(s) %v%s. Each call rescans completion functions and adds ~10–30ms to shell startup.",
				len(hits), hits,
				ternary(hasOMZ, " (Oh My Zsh already runs compinit)", ""),
			),
			Suggestion: "Move any custom fpath= additions BEFORE the OMZ source line so a single compinit picks them up, then remove the trailing compinit -C call.",
			Fix:        compFix,
		}}
		return out
	}
	return nil
}

// checkSlowInteractiveInits flags `eval $(slow-tool ...)` and similar patterns
// in interactive files that aren't wrapped in a lazy-load function or otherwise
// guarded (case branches, cache-prime ifs).
func checkSlowInteractiveInits(cf placer.ConfigFile, lines []string, bctx *BlockContext) []Finding {
	if cf.Scope != placer.ScopeInteractive {
		return nil
	}
	// Tools known to take >5ms when their init is eval'd
	slowTools := []string{
		"rbenv init", "pyenv init", "nodenv init", "fnm env",
		"direnv hook", "oh-my-posh init", "starship init",
		"zoxide init", "atuin init", "mcfly init",
		"thefuck --alias", "navi widget", "tv init",
	}
	var out []Finding
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "#") {
			continue
		}
		for _, tool := range slowTools {
			if !strings.Contains(trim, tool) {
				continue
			}
			// If this line is inside or near a function definition that names
			// the tool, treat it as already lazy-loaded.
			if isLazyWrapped(lines, i, firstWord(tool)) {
				continue
			}
			// Inside a `case ... esac` branch — terminal-conditional, not eager every shell.
			if bctx != nil && bctx.InCase(i+1) {
				continue
			}
			// Inside an `if [[ ! -f $cache ]]` cache-prime guard — runs once on cache miss.
			if bctx != nil && bctx.InCachePrimeGuard(i+1) {
				continue
			}
			out = append(out, Finding{
				Severity: SevInfo,
				File:     cf.Path,
				Line:     i + 1,
				Code:     trim,
				Title:    fmt.Sprintf("Eager init of %q in interactive scope", firstWord(tool)),
				Detail: fmt.Sprintf(
					"%s runs every interactive shell launch and adds 5–50ms to startup. If you don't use %s in every shell, lazy-load it.",
					firstWord(tool), firstWord(tool),
				),
				Suggestion: fmt.Sprintf("Wrap with a lazy-load function (see rbenv/pyenv pattern in your .zshrc) or run `envset lazy %s`.", firstWord(tool)),
			})
			break
		}
	}
	return out
}

// checkDuplicateKeysAcrossFiles flags the same KEY exported in >1 file.
func checkDuplicateKeysAcrossFiles(reg *placer.Registry, files map[string][]string) []Finding {
	exportRe := regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=`)
	type loc struct {
		Path string
		Line int
	}
	keyLocs := map[string][]loc{}
	for _, cf := range reg.Files {
		if !cf.Exists {
			continue
		}
		for i, ln := range files[cf.Path] {
			trim := strings.TrimSpace(ln)
			if strings.HasPrefix(trim, "#") {
				continue
			}
			m := exportRe.FindStringSubmatch(trim)
			if m == nil {
				continue
			}
			key := m[1]
			if key == "PATH" {
				continue // PATH is intentionally additive across files
			}
			keyLocs[key] = append(keyLocs[key], loc{Path: cf.Path, Line: i + 1})
		}
	}
	var out []Finding
	for key, locs := range keyLocs {
		if len(locs) < 2 {
			continue
		}
		// Deduplicate same-file repeats
		seen := map[string]bool{}
		uniq := locs[:0]
		for _, l := range locs {
			if !seen[l.Path] {
				seen[l.Path] = true
				uniq = append(uniq, l)
			}
		}
		if len(uniq) < 2 {
			continue
		}
		var details []string
		for _, l := range uniq {
			details = append(details, fmt.Sprintf("%s:%d", l.Path, l.Line))
		}
		// Build edits: comment out all but the first occurrence.
		dupEdits := make([]FixEdit, len(uniq)-1)
		for k, l := range uniq[1:] {
			dupEdits[k] = FixEdit{File: l.Path, Line: l.Line}
		}
		out = append(out, Finding{
			Severity:   SevInfo,
			File:       uniq[0].Path,
			Line:       uniq[0].Line,
			Code:       "export " + key + "=",
			Title:      fmt.Sprintf("Key %q defined in multiple files", key),
			Detail:     "Defined in: " + strings.Join(details, ", ") + ". The last-sourced one wins; the others are dead code.",
			Suggestion: "Keep one definition (in the file matching its scope) and remove the others.",
			Fix:        &FindingFix{Edits: dupEdits},
		})
	}
	return out
}

// ---------- helpers ----------

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out, sc.Err()
}

func expandHome(p, home string) string {
	if strings.HasPrefix(p, "~/") {
		return home + p[1:]
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "$HOME/") {
		return home + p[5:]
	}
	return p
}

// scopeBroaderThan returns true if scope a should be visible to MORE shells than b.
//
//	all_sessions > login > interactive
func scopeBroaderThan(a, b placer.Scope) bool {
	rank := map[placer.Scope]int{
		placer.ScopeInteractive: 1,
		placer.ScopeLogin:       2,
		placer.ScopeAllSessions: 3,
	}
	return rank[a] > rank[b]
}

// isLazyWrapped checks whether the line at index i sits inside a zsh function
// body whose name starts with toolName. Heuristic: scan up to 30 lines before
// for a `funcName() {` or `function funcName` declaration.
func isLazyWrapped(lines []string, i int, toolName string) bool {
	start := i - 30
	if start < 0 {
		start = 0
	}
	funcDecl := regexp.MustCompile(fmt.Sprintf(`^\s*(?:function\s+)?%s\s*\(\s*\)\s*{`, regexp.QuoteMeta(toolName)))
	depth := 0
	for j := start; j < i; j++ {
		ln := lines[j]
		if funcDecl.MatchString(ln) {
			depth++
		}
		if strings.Contains(ln, "}") {
			if depth > 0 {
				depth--
			}
		}
	}
	return depth > 0
}

func firstWord(s string) string {
	for i, r := range s {
		if r == ' ' || r == '\t' {
			return s[:i]
		}
	}
	return s
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
