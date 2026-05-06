package wizard

import (
	"fmt"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/experimentalist/envset/pkg/detector"
	"github.com/experimentalist/envset/pkg/logger"
	"github.com/experimentalist/envset/pkg/placer"
	"github.com/experimentalist/envset/pkg/targets"
)

// RunAddWizard interactively prompts for add operation details.
// Uses the placer to intelligently suggest the optimal config file.
func RunAddWizard(dryRun bool) (key, value, targetName string, varType targets.VarType, confirmed bool, err error) {
	logger.Step("wizard.RunAddWizard")

	// 1. Key
	keyPrompt := &survey.Input{
		Message: "Variable name (e.g., FOO):",
		Help:    "The environment variable key to set.",
	}
	if err = survey.AskOne(keyPrompt, &key, survey.WithValidator(survey.Required)); err != nil {
		return
	}
	logger.QA("Variable name", key)

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
		logger.QA("Secret-looking key, continue?", fmt.Sprintf("%v", warn))
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
	logger.QA("Var type", varTypeStr)

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
	displayVal := value
	if detector.IsSecret(key) {
		displayVal = "[redacted]"
	}
	logger.QA("Value", displayVal)

	// 4. Detect environment & build placer
	info, detectErr := detector.Detect()
	if detectErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not detect environment: %v\n", detectErr)
	}

	p := placer.NewPlacer(info)
	intent := placer.DetectIntent(key, value, varType)

	// 5. Analyze placements
	logger.Trace("placer intent: category=%s scope=%s", intent.Category, intent.Scope)
	placements := p.Analyze(intent)

	// 6. Route by confidence
	if len(placements) == 0 {
		fmt.Println("No suitable config files found.")
		os.Exit(1)
	}

	top := placements[0]
	logger.Decision("top placement: %s confidence=%.0f%% reason=%q",
		top.File.Name, top.Confidence*100, top.Reason)

	if top.Confidence >= 0.75 {
		// High confidence — suggest with explanation
		useIt, name, qErr := placer.AskPlacementConfirmation(top, p.Registry)
		if qErr != nil {
			err = qErr
			return
		}
		logger.QA("Confirm placement", fmt.Sprintf("use=%v target=%s", useIt, name))
		confirmed = useIt
		targetName = name
	} else {
		// Low confidence — ask clarifying questions
		logger.Trace("top confidence below 0.75; asking clarifying questions")
		fmt.Println("\nThe optimal location is unclear. Let me ask a few questions...")
		refined, qErr := placer.AskUserIntent(intent)
		if qErr != nil {
			err = qErr
			return
		}
		logger.QA("Refined intent", fmt.Sprintf("category=%s scope=%s", refined.Category, refined.Scope))

		// Re-analyze with refined intent
		placements = p.Analyze(refined)
		if len(placements) == 0 {
			fmt.Println("No suitable config files found after refinement.")
			os.Exit(1)
		}

		useIt, name, qErr := placer.AskPlacementConfirmation(placements[0], p.Registry)
		if qErr != nil {
			err = qErr
			return
		}
		logger.QA("Confirm placement", fmt.Sprintf("use=%v target=%s", useIt, name))
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
	logger.Step("wizard.RunRemoveWizard")

	keyPrompt := &survey.Input{
		Message: "Variable name to remove:",
		Help:    "The environment variable key to remove.",
	}
	if err = survey.AskOne(keyPrompt, &key, survey.WithValidator(survey.Required)); err != nil {
		return
	}
	logger.QA("Variable name to remove", key)

	// Use placer registry to show all files including custom ones
	info, _ := detector.Detect()
	p := placer.NewPlacer(info)
	targetName, err = placer.AskTargetPick(p.Registry)
	if err != nil {
		return
	}
	logger.QA("Target file", targetName)

	commentPrompt := &survey.Confirm{
		Message: "Comment out instead of deleting?",
		Default: true,
		Help:    "Commenting preserves the line but disables it. Deleting removes it permanently.",
	}
	if err = survey.AskOne(commentPrompt, &commentOut); err != nil {
		return
	}
	logger.QA("Comment out instead of delete?", fmt.Sprintf("%v", commentOut))

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
	logger.QA("Confirm "+action, fmt.Sprintf("%v", confirmed))

	return
}

// RunListWizard prompts for target to list.
func RunListWizard() (targetName string, err error) {
	logger.Step("wizard.RunListWizard")
	info, _ := detector.Detect()
	p := placer.NewPlacer(info)
	targetName, err = placer.AskTargetPick(p.Registry)
	if err != nil {
		return
	}
	logger.QA("Target file", targetName)
	return
}
