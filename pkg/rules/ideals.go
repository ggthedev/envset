package rules

import "github.com/ggthedev/envset/pkg/logger"

// Check IDs — keep in sync with the emitters in pkg/audit/audit.go.
const (
	CheckBrokenPath          = "broken-path"
	CheckMisplacedPathExport = "misplaced-path-export"
	CheckSourceChainMismatch = "source-chain-mismatch"
	CheckDuplicateCompinit   = "duplicate-compinit"
	CheckSlowInteractiveInit = "slow-interactive-init"
	CheckDuplicateKeys       = "duplicate-keys"
	CheckQuotedTildePath     = "quoted-tilde-path"
)

// BuiltinIdeals returns the default rule set shipped with this binary.
// On first run (and on version upgrade), this is serialised to ideals.json
// so users can read the policy the auditor enforces.
func BuiltinIdeals(envsetVersion string) Ideals {
	defer logger.Enter()()
	return Ideals{
		Version:     CurrentVersion,
		GeneratedBy: "envset " + envsetVersion,
		Checks: []IdealCheck{
			{
				ID:              CheckQuotedTildePath,
				Title:           "Quoted ~ in PATH never expands",
				DefaultSeverity: "warning",
				ApproxImpact:    "Commands in that directory are not found",
				Description:     "A ~ inside quotes stays literal: zsh and other programs skip that PATH entry. Use $HOME.",
			},
			{
				ID:              CheckBrokenPath,
				Title:           "Broken PATH assignment",
				DefaultSeverity: "error",
				ApproxImpact:    "PATH corrupted on every shell start",
				Description:     "PATH= contains literal `echo` or other unescaped quoting that silently breaks PATH.",
			},
			{
				ID:              CheckMisplacedPathExport,
				Title:           "PATH export in interactive-only file",
				DefaultSeverity: "warning",
				ApproxImpact:    "PATH invisible to scripts, cron, GUI subshells",
				Description:     "PATH-mutating export placed in a file sourced only by interactive shells (e.g., .zshrc).",
			},
			{
				ID:              CheckSourceChainMismatch,
				Title:           "Sourced file scope is narrower than its content's intent",
				DefaultSeverity: "warning",
				ApproxImpact:    "Variables don't reach all intended shells",
				Description:     "An all-sessions file is sourced from a login-only or interactive-only file.",
			},
			{
				ID:              CheckDuplicateCompinit,
				Title:           "Duplicate compinit invocation",
				DefaultSeverity: "warning",
				ApproxImpact:    "approx +10–30ms shell startup",
				Description:     "compinit runs more times than necessary (Oh My Zsh already runs it).",
			},
			{
				ID:              CheckSlowInteractiveInit,
				Title:           "Eager init of slow tool in interactive scope",
				DefaultSeverity: "info",
				ApproxImpact:    "approx +5–50ms shell startup",
				Description:     "Tool initialiser is eval'd on every interactive shell instead of being lazy-loaded.",
			},
			{
				ID:              CheckDuplicateKeys,
				Title:           "Key defined in multiple files",
				DefaultSeverity: "info",
				ApproxImpact:    "Dead code; last-sourced definition wins",
				Description:     "Same env var exported in more than one config file.",
			},
		},
	}
}

// FindCheck returns the IdealCheck with the given id, or nil.
func (i Ideals) FindCheck(id string) *IdealCheck {
	defer logger.Enter()()
	for k := range i.Checks {
		if i.Checks[k].ID == id {
			return &i.Checks[k]
		}
	}
	return nil
}
