package audit

// Severity classifies the importance of a finding.
type Severity string

const (
	SevError Severity = "error"   // breaks shells / wrong behavior
	SevWarn  Severity = "warning" // perf regression / wrong scope
	SevInfo  Severity = "info"    // cleanup / minor optimization
)

// Finding is a single issue detected in a config file.
type Finding struct {
	Severity   Severity
	File       string // absolute path
	Line       int    // 1-indexed; 0 if file-level
	Code       string // offending line content (trimmed)
	Title      string // short title
	Detail     string // explanation of the problem
	Suggestion string // concrete fix
}

// Report is the output of an audit run.
type Report struct {
	Findings []Finding
}

// CountBySeverity returns how many findings of each severity exist.
func (r Report) CountBySeverity() map[Severity]int {
	m := map[Severity]int{SevError: 0, SevWarn: 0, SevInfo: 0}
	for _, f := range r.Findings {
		m[f.Severity]++
	}
	return m
}
