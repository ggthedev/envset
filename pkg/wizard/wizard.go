package wizard

import (
	"fmt"
	"github.com/experimentalist/envset/pkg/term"
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

	var varTypeStr string
	step := 1

	for step <= 3 {
		switch step {
		case 1:
			varTypeOpts := []string{
				"export (export FOO=bar)",
				"export and update PATH (export FOO=/dir, PATH=$PATH:$FOO)",
				"path_append (PATH=\"$PATH:/new\")",
				"path_prepend (PATH=\"/new:$PATH\")",
				"eval (eval $(command))",
				"source (source /path/to/file)",
				"fish_set (set -x FOO bar, fish only)",
				"q - Quit",
			}
			typePrompt := &survey.Select{
				Message: "How should this be set?",
				Options: varTypeOpts,
				Default: varTypeOpts[0],
			}
			if err = term.Ask(typePrompt, &varTypeStr); err != nil {
				return
			}
			if strings.HasPrefix(varTypeStr, "q") {
				return // confirmed=false: caller prints "Aborted."
			}
			logger.QA("Var type", varTypeStr)

			switch {
			case strings.HasPrefix(varTypeStr, "export and"):
				varType = targets.VarTypeExportAndPathAppend
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
			step++

		case 2:
			if varType == targets.VarTypePathAppend || varType == targets.VarTypePathPrepend {
				key = "PATH"
				step++
				continue
			}

			keyPrompt := &survey.Input{
				Message: "Variable name (e.g., FOO) [q to quit, b to back]:",
				Help:    "The environment variable key to set. Type 'q' to quit, 'b' to go back.",
			}
			if err = term.Ask(keyPrompt, &key, survey.WithValidator(survey.Required)); err != nil {
				return
			}
			if key == "q" {
				return // confirmed=false: caller prints "Aborted."
			}
			if key == "b" {
				step--
				continue
			}
			logger.QA("Variable name", key)

			if detector.IsSecret(key) {
				warn := false
				warnPrompt := &survey.Confirm{
					Message: fmt.Sprintf("WARNING: %s looks like a secret/API key. Continue?", key),
					Default: false,
				}
				if err = term.Ask(warnPrompt, &warn); err != nil {
					return
				}
				logger.QA("Secret-looking key, continue?", fmt.Sprintf("%v", warn))
				if !warn {
					return // confirmed=false: caller prints "Aborted."
				}
			}
			step++

		case 3:
			valHelp := "The value to assign. For eval, enter the command. For source, enter the file path."
			if varType == targets.VarTypePathAppend || varType == targets.VarTypePathPrepend {
				valHelp = "The path to append/prepend to PATH."
			}
			valuePrompt := &survey.Input{
				Message: "Value [q to quit, b to back]:",
				Help:    valHelp + " Type 'q' to quit, 'b' to go back.",
			}
			if err = term.Ask(valuePrompt, &value, survey.WithValidator(survey.Required)); err != nil {
				return
			}
			if value == "q" {
				return // confirmed=false: caller prints "Aborted."
			}
			if value == "b" {
				if varType == targets.VarTypePathAppend || varType == targets.VarTypePathPrepend {
					step -= 2 // go back to var type, since we skipped key
				} else {
					step--
				}
				continue
			}
			displayVal := value
			if detector.IsSecret(key) {
				displayVal = "[redacted]"
			}
			logger.QA("Value", displayVal)
			step++
		}
	}

	// 4. Detect environment & build placer
	info, detectErr := detector.Detect()
	if detectErr != nil {
		err = fmt.Errorf("could not detect environment: %w", detectErr)
		return
	}

	p := placer.NewPlacer(info)
	intent := placer.DetectIntent(key, value, varType)

	// 5. Analyze placements
	logger.Trace("placer intent: category=%s scope=%s", intent.Category, intent.Scope)
	placements := p.Analyze(intent)

	// 6. Route by confidence
	if len(placements) == 0 {
		err = fmt.Errorf("no suitable config files found")
		return
	}

	top := placements[0]
	logger.Decision("top placement: %s confidence=%.0f%% reason=%q",
		top.File.Name, top.Confidence*100, top.Reason)

	// 6a. Source-chain check: if any *custom* file with a matching declared
	// scope is broken (effective scope narrower than the intent), surface the
	// scope-mismatch prompt before falling through to the normal flow. This
	// gives the user a clear choice between their familiar custom file, the
	// canonical primary file, or bailing out to fix the source-chain.
	if broken := findBrokenRelevantCustom(placements, intent); broken != nil {
		logger.Trace("scope mismatch detected: %s declared=%s effective=%s",
			broken.File.Name, broken.File.Scope, broken.EffectiveScope)
		if primary, ok := placer.PrimaryFileForScope(p.Registry, intent.Scope); ok {
			name, qErr := placer.AskScopeMismatchResolution(*broken, *primary, intent.Scope)
			if qErr != nil {
				err = qErr
				return
			}
			logger.QA("Scope-mismatch resolution",
				fmt.Sprintf("broken=%s primary=%s chosen=%s",
					broken.File.Name, primary.Name, name))
			confirmed = true
			targetName = name
			goto preview
		}
		logger.Trace("no canonical primary file found for scope %s; falling through", intent.Scope)
	}

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
		fmt.Fprintln(os.Stderr, "\nThe optimal location is unclear. Let me ask a few questions...")
		refined, qErr := placer.AskUserIntent(intent)
		if qErr != nil {
			err = qErr
			return
		}
		logger.QA("Refined intent", fmt.Sprintf("category=%s scope=%s", refined.Category, refined.Scope))

		// Re-analyze with refined intent
		placements = p.Analyze(refined)
		if len(placements) == 0 {
			err = fmt.Errorf("no suitable config files found after refinement")
			return
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

preview:

	// 7. Preview
	if confirmed && targetName != "" {
		t, tErr := targets.Factory(targetName, info.HomeDir)
		if tErr != nil {
			err = tErr
			return
		}
		preview, _ := t.Preview(key, value, varType, targets.QuoteExpand)
		fmt.Fprintf(os.Stderr, "\nPreview line:\n  %s\n\n", preview)
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
	if err = term.Ask(keyPrompt, &key, survey.WithValidator(survey.Required)); err != nil {
		return
	}
	logger.QA("Variable name to remove", key)

	// Use placer registry to show all files including custom ones
	info, dErr := detector.Detect()
	if dErr != nil {
		err = dErr
		return
	}
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
	if err = term.Ask(commentPrompt, &commentOut); err != nil {
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
	if err = term.Ask(confirmPrompt, &confirmed); err != nil {
		return
	}
	logger.QA("Confirm "+action, fmt.Sprintf("%v", confirmed))

	return
}

// RunListWizard prompts for target to list.
func RunListWizard() (targetName string, err error) {
	logger.Step("wizard.RunListWizard")
	info, dErr := detector.Detect()
	if dErr != nil {
		err = dErr
		return
	}
	p := placer.NewPlacer(info)
	targetName, err = placer.AskTargetPick(p.Registry)
	if err != nil {
		return
	}
	logger.QA("Target file", targetName)
	return
}

// findBrokenRelevantCustom scans placements for a *custom* file whose declared
// scope satisfies the user's intent but whose effective scope (per the
// source-chain) does not. Returns nil when no such file exists.
//
// This is the trigger for AskScopeMismatchResolution: it means the user has
// a familiar destination (e.g. ~/.gg_system_exports) that *looks* correct but
// is actually broken — silently steering them to .zshenv would be surprising.
func findBrokenRelevantCustom(placements []placer.Placement, intent placer.Intent) *placer.Placement {
	if intent.Scope == placer.ScopeUnknown {
		return nil
	}
	for i := range placements {
		pl := &placements[i]
		if !pl.File.Custom || !pl.ScopeMismatch {
			continue
		}
		if pl.File.Scope == intent.Scope {
			return pl
		}
	}
	return nil
}
