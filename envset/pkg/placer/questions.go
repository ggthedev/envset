package placer

import (
	"fmt"
	"strings"

	"github.com/AlecAivazis/survey/v2"
)

// AskScope prompts the user to clarify the intended scope.
func AskScope() (Scope, error) {
	var choice string
	prompt := &survey.Select{
		Message: "When should this variable be active?",
		Options: []string{
			"Interactive shells only (terminals I open)",
			"Login shells (ssh, tmux new sessions)",
			"All sessions (scripts, cron, background processes too)",
		},
		Default: "All sessions (scripts, cron, background processes too)",
		Help:    "This determines which config file the entry goes into.",
	}
	if err := survey.AskOne(prompt, &choice); err != nil {
		return ScopeUnknown, err
	}

	switch {
	case containsStr(choice, "Interactive"):
		return ScopeInteractive, nil
	case containsStr(choice, "Login"):
		return ScopeLogin, nil
	case containsStr(choice, "All"):
		return ScopeAllSessions, nil
	}
	return ScopeUnknown, nil
}

// AskSystemOrUser prompts whether this is a system-level or personal setting.
func AskSystemOrUser() (bool, error) {
	var isSystem bool
	prompt := &survey.Confirm{
		Message: "Is this a system-level setting (affects tools, scripts, background processes)?",
		Default: false,
		Help:    "System settings go to all-sessions files. Personal preferences go to interactive files.",
	}
	if err := survey.AskOne(prompt, &isSystem); err != nil {
		return false, err
	}
	return isSystem, nil
}

// AskPlacementConfirmation shows the top suggestion and asks for confirmation.
// If user rejects, they pick from the full registry. Surfaces duplicate
// locations and scope-mismatch warnings inline so the user has full context
// before answering yes/no.
func AskPlacementConfirmation(pl Placement, reg *Registry) (bool, string, error) {
	var confirm bool
	fmt.Printf("\nSuggested file: %s (%s)\n", pl.File.Name, pl.File.Path)
	fmt.Printf("Reason: %s (confidence: %.0f%%)\n", pl.Reason, pl.Confidence*100)
	if pl.Update {
		fmt.Println("Note: This key already exists — it will be updated.")
	}
	if pl.ScopeMismatch {
		fmt.Printf("⚠ Effective scope is %s, not %s — variables may not reach all the shells you expect.\n",
			pl.EffectiveScope, pl.File.Scope)
	}
	if len(pl.DuplicateLocations) > 0 {
		fmt.Printf("⚠ Same key also defined in: %s — those copies will be shadowed.\n",
			strings.Join(pl.DuplicateLocations, ", "))
	}
	fmt.Println()

	prompt := &survey.Confirm{
		Message: "Use this file?",
		Default: pl.Confidence >= 0.75,
	}
	if err := survey.AskOne(prompt, &confirm); err != nil {
		return false, "", err
	}

	if confirm {
		return true, pl.File.Name, nil
	}

	// If rejected, let user pick from all files
	name, err := AskTargetPick(reg)
	if err != nil {
		return false, "", err
	}
	return true, name, nil
}

// AskScopeMismatchResolution presents the user with three options when the
// auto-suggested file has a broken source-chain. This is the Stage-2 prompt
// that replaces the plain confirmation in the broken-scope case.
//
// `broken` is the file the placer would otherwise have suggested
// (e.g. ~/.gg_system_exports, declared all_sessions but only sourced from
// .zprofile). `primary` is the canonical file for the intent's scope
// (e.g. .zshenv for all_sessions). Either may be returned as the final
// target name. A user-cancellation results in (name="", err!=nil) so callers
// can abort the operation cleanly.
func AskScopeMismatchResolution(broken Placement, primary ConfigFile, intentScope Scope) (string, error) {
	optBroken := fmt.Sprintf("Add to %s anyway (only reaches %s shells until source-chain is fixed)",
		broken.File.Name, broken.EffectiveScope)
	optPrimary := fmt.Sprintf("Add to %s instead (correct %s scope)",
		primary.Name, intentScope)
	optBail := "Cancel — let me fix the source-chain first (run: envset audit)"

	fmt.Printf("\n⚠ Scope mismatch detected:\n")
	fmt.Printf("    %s is declared %s but is only reachable from %s shells.\n",
		broken.File.Name, broken.File.Scope, broken.EffectiveScope)
	fmt.Printf("    Your variable wants %s scope.\n\n", intentScope)

	var choice string
	prompt := &survey.Select{
		Message: "How should I resolve this?",
		Options: []string{optBroken, optPrimary, optBail},
		Default: optPrimary,
	}
	if err := survey.AskOne(prompt, &choice); err != nil {
		return "", err
	}
	switch choice {
	case optBroken:
		return broken.File.Name, nil
	case optPrimary:
		return primary.Name, nil
	case optBail:
		return "", fmt.Errorf("cancelled by user — run `envset audit` for fix instructions")
	}
	return "", fmt.Errorf("unexpected selection: %s", choice)
}

// PrimaryFileForScope returns the canonical (non-Custom, declared-scope-matches)
// existing file for a given scope. Used by the wizard to offer the "use the
// correct file instead" option when source-chain mismatch is detected.
//
// Returns (nil, false) if no matching primary exists in the registry.
func PrimaryFileForScope(reg *Registry, scope Scope) (*ConfigFile, bool) {
	for i := range reg.Files {
		f := &reg.Files[i]
		if f.Custom || f.Scope != scope {
			continue
		}
		// Prefer files that the shell actually reads directly (EffectiveScope
		// matches declared Scope and isn't proxied through a sourced ancestor).
		if f.EffectiveScope == scope {
			return f, true
		}
	}
	// Fallback: any file whose declared scope matches, even if not currently
	// existing on disk (the operation will create it).
	for i := range reg.Files {
		f := &reg.Files[i]
		if !f.Custom && f.Scope == scope {
			return f, true
		}
	}
	return nil, false
}

// AskTargetPick lets the user pick any registered file.
func AskTargetPick(registry *Registry) (string, error) {
	var names []string
	for _, f := range registry.Files {
		status := "missing"
		if f.Exists {
			status = "exists"
		}
		names = append(names, fmt.Sprintf("%s (%s) [%s]", f.Name, f.Path, status))
	}

	var choice string
	prompt := &survey.Select{
		Message: "Choose target file:",
		Options: names,
	}
	if err := survey.AskOne(prompt, &choice); err != nil {
		return "", err
	}

	// Extract name from "name (path) [status]"
	parts := strings.Split(choice, " ")
	if len(parts) > 0 {
		return parts[0], nil
	}
	return "", fmt.Errorf("could not parse selection")
}

// AskUserIntent runs a full clarifying questionnaire when confidence is low.
func AskUserIntent(intent Intent) (Intent, error) {
	// If scope is still unknown, ask
	if intent.Scope == ScopeUnknown {
		scope, err := AskScope()
		if err != nil {
			return intent, err
		}
		intent.Scope = scope
	}

	// If generic, ask system vs user
	if intent.Category == CatGeneric {
		isSystem, err := AskSystemOrUser()
		if err != nil {
			return intent, err
		}
		if isSystem {
			intent.Scope = ScopeAllSessions
			intent.Category = CatSystem
		} else {
			intent.Scope = ScopeInteractive
			intent.Category = CatUserPref
		}
	}

	return intent, nil
}

func containsStr(s, substr string) bool {
	return strings.Contains(s, substr)
}
