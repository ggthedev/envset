package wizard

import (
	"fmt"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/experimentalist/envset/pkg/detector"
	"github.com/experimentalist/envset/pkg/placer"
	"github.com/experimentalist/envset/pkg/targets"
)

// RunAddWizard interactively prompts for add operation details.
// Uses the placer to intelligently suggest the optimal config file.
func RunAddWizard(dryRun bool) (key, value, targetName string, varType targets.VarType, confirmed bool, err error) {
	// 1. Key
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

	// 2. Var type
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

	// 3. Value
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

	// 4. Detect environment & build placer
	info, detectErr := detector.Detect()
	if detectErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not detect environment: %v\n", detectErr)
	}

	p := placer.NewPlacer(info)
	intent := placer.DetectIntent(key, value, varType)

	// 5. Analyze placements
	placements := p.Analyze(intent)

	// 6. Route by confidence
	if len(placements) == 0 {
		fmt.Println("No suitable config files found.")
		os.Exit(1)
	}

	top := placements[0]

	if top.Confidence >= 0.75 {
		// High confidence — suggest with explanation
		useIt, name, qErr := placer.AskPlacementConfirmation(top, p.Registry)
		if qErr != nil {
			err = qErr
			return
		}
		confirmed = useIt
		targetName = name
	} else {
		// Low confidence — ask clarifying questions
		fmt.Println("\nThe optimal location is unclear. Let me ask a few questions...")
		intent, qErr := placer.AskUserIntent(intent)
		if qErr != nil {
			err = qErr
			return
		}

		// Re-analyze with refined intent
		placements = p.Analyze(intent)
		if len(placements) == 0 {
			fmt.Println("No suitable config files found after refinement.")
			os.Exit(1)
		}

		useIt, name, qErr := placer.AskPlacementConfirmation(placements[0], p.Registry)
		if qErr != nil {
			err = qErr
			return
		}
		confirmed = useIt
		targetName = name
	}

	// 7. Preview
	if confirmed && targetName != "" {
		t, tErr := targets.Factory(targetName, info.HomeDir)
		if tErr != nil {
			err = tErr
			return
		}
		preview, _ := t.Preview(key, value, varType)
		fmt.Printf("\nPreview line:\n  %s\n\n", preview)
	}

	return
}

// RunRemoveWizard interactively prompts for remove operation details.
func RunRemoveWizard() (key, targetName string, commentOut bool, confirmed bool, err error) {
	keyPrompt := &survey.Input{
		Message: "Variable name to remove:",
		Help:    "The environment variable key to remove.",
	}
	if err = survey.AskOne(keyPrompt, &key, survey.WithValidator(survey.Required)); err != nil {
		return
	}

	// Use placer registry to show all files including custom ones
	info, _ := detector.Detect()
	p := placer.NewPlacer(info)
	targetName, err = placer.AskTargetPick(p.Registry)
	if err != nil {
		return
	}

	commentPrompt := &survey.Confirm{
		Message: "Comment out instead of deleting?",
		Default: true,
		Help:    "Commenting preserves the line but disables it. Deleting removes it permanently.",
	}
	if err = survey.AskOne(commentPrompt, &commentOut); err != nil {
		return
	}

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
	p := placer.NewPlacer(info)
	targetName, err = placer.AskTargetPick(p.Registry)
	if err != nil {
		return
	}
	return
}
