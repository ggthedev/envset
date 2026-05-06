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
// If user rejects, they pick from the full registry.
func AskPlacementConfirmation(pl Placement, reg *Registry) (bool, string, error) {
	var confirm bool
	fmt.Printf("\nSuggested file: %s (%s)\n", pl.File.Name, pl.File.Path)
	fmt.Printf("Reason: %s (confidence: %.0f%%)\n", pl.Reason, pl.Confidence*100)
	if pl.Update {
		fmt.Println("Note: This key already exists — it will be updated.")
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
