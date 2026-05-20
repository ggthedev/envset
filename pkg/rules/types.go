// Package rules implements the layered rule model used by `envset audit`:
//
//   - Ideals  — best-practice expectations shipped with the binary, written to
//     ~/.config/envset/ideals.json on first run and on version upgrade.
//   - Custom  — per-user acknowledgements recorded interactively by
//     `envset audit --fix`, persisted in ~/.config/envset/custom.json.
//
// Both files are read on every audit. Custom rules cannot suppress findings
// silently — they only "acknowledge" them (the finding is still rendered, but
// in a separate section that calls out the approx impact and the user's reason).
//
// TBD (deferred — out of v1 scope):
//   - per-project overrides (e.g., .envset/custom.json near the audited files)
//   - rule import/export bundles
//   - time-bound rules (auto-expire & re-confirm)
//   - rules that rewrite the auditor's suggestion text
package rules

// CurrentVersion is the schema version of both ideals.json and custom.json.
// Bump when fields change in incompatible ways.
const CurrentVersion = 1

// IdealCheck describes one built-in audit check at the policy level.
// The Go check function in pkg/audit produces findings with the matching ID;
// the values here drive rendering and impact reporting.
type IdealCheck struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	DefaultSeverity string `json:"defaultSeverity"` // "error" | "warning" | "info"
	ApproxImpact    string `json:"approxImpact"`    // human-readable, e.g. "approx +5–50ms shell startup"
	Description     string `json:"description,omitempty"`
}

// Ideals is the on-disk form of the built-in rule set.
type Ideals struct {
	Version     int          `json:"version"`
	GeneratedBy string       `json:"generatedBy"` // e.g. "envset v0.x.x"
	Checks      []IdealCheck `json:"checks"`
}

// Match identifies the line/construct a custom rule applies to.
// Hybrid keying: Subject is preferred (broader, survives formatting tweaks);
// LineHash is the fallback (exact line content fingerprint, sha256 truncated).
// At least one of Subject or LineHash MUST be set.
type Match struct {
	File     string `json:"file"`     // absolute path, ~ expanded at write-time
	Subject  string `json:"subject,omitempty"`
	LineHash string `json:"lineHash,omitempty"`
}

// CustomRule is one user-confirmed exception. v1 only supports
// Action="acknowledge": the finding is still shown but tagged as user-confirmed.
type CustomRule struct {
	CheckID      string `json:"checkId"`
	Match        Match  `json:"match"`
	Action       string `json:"action"`               // "acknowledge"
	Reason       string `json:"reason,omitempty"`     // free-text, optional
	ApproxImpact string `json:"approxImpact"`         // snapshotted at creation, refreshed on --fix
	CreatedAt    string `json:"createdAt"`            // RFC3339
}

// Custom is the on-disk form of user-confirmed exceptions.
type Custom struct {
	Version int          `json:"version"`
	Rules   []CustomRule `json:"rules"`
}

// Action constants. Only Acknowledge is supported in v1.
const (
	ActionAcknowledge = "acknowledge"
)
