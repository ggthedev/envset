package audit

import "github.com/ggthedev/envset/pkg/logger"

// Severity classifies the importance of a finding.
type Severity string

const (
	SevError Severity = "error"   // breaks shells / wrong behavior
	SevWarn  Severity = "warning" // perf regression / wrong scope
	SevInfo  Severity = "info"    // cleanup / minor optimization
)

// FixEdit is a single line-level comment-out operation.
type FixEdit struct {
	File string // absolute path
	Line int    // 1-indexed
}

// FindingFix carries the machine-readable fix for a Finding.
// All automatic fixes are comment-out operations (safe & reversible).
// A nil Fix means no automatic fix exists for this finding.
type FindingFix struct {
	Edits []FixEdit
}

// Finding is a single issue detected in a config file.
type Finding struct {
	Severity   Severity
	File       string // absolute path
	Line       int    // 1-indexed; 0 if file-level
	Code       string // offending line content (trimmed)
	Title      string // short title
	Detail     string // explanation of the problem
	Suggestion string // concrete fix

	// Fix carries the machine-readable automatic fix, or nil if no safe
	// automatic fix exists. All auto-fixes comment out the offending line(s).
	Fix *FindingFix

	// Rule-system fields. Set by each check; consumed by the rules package
	// to match against user-confirmed exceptions in custom.json.
	CheckID      string // stable id, e.g. "misplaced-path-export"
	Subject      string // hybrid match key — tool name, env var, key, etc. Empty if file-level.
	ApproxImpact string // human-readable impact, e.g. "approx +5–50ms shell startup"

	// Acknowledged is set true after applying user-confirmed custom rules.
	// Acknowledged findings are still rendered, but in a separate section.
	Acknowledged   bool
	OverrideReason string // free-text reason captured at override time
}

// Fixable reports whether this finding has a safe automatic fix.
func (f Finding) Fixable() bool {
	defer logger.Enter()()
	return f.Fix != nil && len(f.Fix.Edits) > 0
}

// Report is the output of an audit run.
type Report struct {
	Findings []Finding
}

// CountBySeverity returns how many findings of each severity exist.
func (r Report) CountBySeverity() map[Severity]int {
	defer logger.Enter()()
	m := map[Severity]int{SevError: 0, SevWarn: 0, SevInfo: 0}
	for _, f := range r.Findings {
		m[f.Severity]++
	}
	return m
}
