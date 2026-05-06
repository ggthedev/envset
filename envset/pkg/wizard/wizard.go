package wizard

import (
	"fmt"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/experimentalist/envset/pkg/detector"
	"github.com/experimentalist/envset/pkg/targets"
)

// RunAddWizard interactively prompts for add operation details.
func RunAddWizard(dryRun bool) (key, value, targetName string, varType targets.VarType, confirmed bool, err error) {
	// Key
	keyPrompt := &survey.Input{
		Message: "Variable name (e.g., FOO):",
		Help:    "The environment variable key to set.",
	}
	if err = survey.AskOne(keyPrompt, &key, survey.WithValidator(survey.Required)); err != nil {
		return
	}

	// Secret warning
	if detector.IsSecret(key) {
		warn := false
		warnPrompt := &survey.Confirm{
			Message: fmt.Sprintf("WARNING: %s looks like a secret/API key. Continue?", key),
			Default: false,
		}
		if err = survey.AskOne(warnPrompt, &warn); err != nil {
			return
		}
		if !warn {
			fmt.Println("Aborted.")
			os.Exit(0)
		}
	}

	// Var type
	varTypeStr := ""
	varTypeOpts := []string{
		"export (export FOO=bar)",
		"eval (eval $(command))",
		"source (source /path/to/file)",
		"path_append (PATH=\"$PATH:/new\")",
		"path_prepend (PATH=\"/new:$PATH\")",
		"fish_set (set -x FOO bar, fish only)",
	}
	typePrompt := &survey.Select{
		Message: "How should this be set?",
		Options: varTypeOpts,
		Default: varTypeOpts[0],
	}
	if err = survey.AskOne(typePrompt, &varTypeStr); err != nil {
		return
	}

	// Parse var type
	switch {
	case strings.HasPrefix(varTypeStr, "export"):
		varType = targets.VarTypeExport
	case strings.HasPrefix(varTypeStr, "eval"):
		varType = targets.VarTypeEval
	case strings.HasPrefix(varTypeStr, "source"):
		varType = targets.VarTypeSource
	case strings.HasPrefix(varTypeStr, "path_append"):
		varType = targets.VarTypePathAppend
	case strings.HasPrefix(varTypeStr, "path_prepend"):
		varType = targets.VarTypePathPrepend
	case strings.HasPrefix(varTypeStr, "fish_set"):
		varType = targets.VarTypeFishSet
	}

	// Value
	valHelp := "The value to assign. For eval, enter the command. For source, enter the file path."
	if varType == targets.VarTypePathAppend || varType == targets.VarTypePathPrepend {
		valHelp = "The path to append/prepend to PATH."
	}
	valuePrompt := &survey.Input{
		Message: "Value:",
		Help:    valHelp,
	}
	if err = survey.AskOne(valuePrompt, &value, survey.WithValidator(survey.Required)); err != nil {
		return
	}

	// Detect environment for target suggestion
	info, detectErr := detector.Detect()
	if detectErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not detect environment: %v\n", detectErr)
	}

	// Target selection
	targetOpts := make([]string, 0, len(targets.AllShellTargets()))
	targetDescriptions := make(map[string]string)
	for _, t := range targets.AllShellTargets() {
		label := t
		path := "(not found)"
		exists := false
		if info != nil {
			for _, c := range info.ShellConfigs {
				if c.Name == t || (t == "config_fish" && c.Name == "config.fish") {
					path = c.Path
					exists = c.Exists
					break
				}
			}
		}
		if exists {
			label = fmt.Sprintf("%s [exists] %s", t, path)
		} else {
			label = fmt.Sprintf("%s %s", t, path)
		}
		targetOpts = append(targetOpts, label)
		targetDescriptions[label] = t
	}

	defaultTarget := targetOpts[0]
	if info != nil {
		suggested := info.SuggestConfig()
		if suggested != nil {
			for _, opt := range targetOpts {
				if strings.HasPrefix(opt, suggested.Name) {
					defaultTarget = opt
					break
				}
			}
		}
	}

	targetPrompt := &survey.Select{
		Message: "Target configuration file:",
		Options: targetOpts,
		Default: defaultTarget,
	}
	var selectedLabel string
	if err = survey.AskOne(targetPrompt, &selectedLabel); err != nil {
		return
	}
	targetName = targetDescriptions[selectedLabel]
	if targetName == "" {
		// fallback: extract first word
		targetName = strings.Fields(selectedLabel)[0]
	}

	// Preview
	t, err := targets.Factory(targetName, info.HomeDir)
	if err != nil {
		return
	}
	preview, _ := t.Preview(key, value, varType)
	fmt.Printf("\nPreview line:\n  %s\n\n", preview)

	// Confirm
	confirmPrompt := &survey.Confirm{
		Message: fmt.Sprintf("Write to %s?", t.Path()),
		Default: true,
	}
	if err = survey.AskOne(confirmPrompt, &confirmed); err != nil {
		return
	}

	return
}

// RunRemoveWizard interactively prompts for remove operation details.
func RunRemoveWizard() (key, targetName string, commentOut bool, confirmed bool, err error) {
	// Key
	keyPrompt := &survey.Input{
		Message: "Variable name to remove:",
		Help:    "The environment variable key to remove.",
	}
	if err = survey.AskOne(keyPrompt, &key, survey.WithValidator(survey.Required)); err != nil {
		return
	}

	// Target
	info, _ := detector.Detect()
	targetOpts := targets.AllShellTargets()
	defaultTarget := targetOpts[0]
	if info != nil && info.SuggestConfig() != nil {
		defaultTarget = info.SuggestConfig().Name
	}

	targetPrompt := &survey.Select{
		Message: "Target configuration file:",
		Options: targetOpts,
		Default: defaultTarget,
	}
	if err = survey.AskOne(targetPrompt, &targetName); err != nil {
		return
	}

	// Comment or delete
	commentPrompt := &survey.Confirm{
		Message: "Comment out instead of deleting?",
		Default: true,
		Help:    "Commenting preserves the line but disables it. Deleting removes it permanently.",
	}
	if err = survey.AskOne(commentPrompt, &commentOut); err != nil {
		return
	}

	// Confirm
	action := "delete"
	if commentOut {
		action = "comment out"
	}
	confirmPrompt := &survey.Confirm{
		Message: fmt.Sprintf("%s %q from %s?", action, key, targetName),
		Default: true,
	}
	if err = survey.AskOne(confirmPrompt, &confirmed); err != nil {
		return
	}

	return
}

// RunListWizard prompts for target to list.
func RunListWizard() (targetName string, err error) {
	info, _ := detector.Detect()
	targetOpts := targets.AllShellTargets()
	defaultTarget := targetOpts[0]
	if info != nil && info.SuggestConfig() != nil {
		defaultTarget = info.SuggestConfig().Name
	}

	targetPrompt := &survey.Select{
		Message: "Target configuration file to list:",
		Options: targetOpts,
		Default: defaultTarget,
	}
	if err = survey.AskOne(targetPrompt, &targetName); err != nil {
		return
	}
	return
}
