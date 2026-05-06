package ops

import (
	"fmt"
	"os"
	"strings"

	"github.com/experimentalist/envset/pkg/audit"
	"github.com/experimentalist/envset/pkg/detector"
	"github.com/experimentalist/envset/pkg/logger"
	"github.com/experimentalist/envset/pkg/placer"
	"github.com/experimentalist/envset/pkg/targets"
	"github.com/experimentalist/envset/pkg/wizard"
)

// Add adds an environment variable entry.
// When printSource is true, only the `source <file>` command is written to stdout
// (so you can `eval "$(envset add ... --print-source)"`). All other output goes to stderr.
func Add(key, value, targetName, varTypeStr string, dryRun, autoYes, printSource bool) error {
	logger.Step("ops.Add")
	logger.Input("key=%q value=%q target=%q type=%q dry_run=%v auto_yes=%v print_source=%v",
		key, redact(key, value), targetName, varTypeStr, dryRun, autoYes, printSource)

	var varType targets.VarType
	var err error

	// Route human-readable output
	infoOut := os.Stdout
	if printSource {
		infoOut = os.Stderr
	}

	// If any required flag is missing, run wizard
	if key == "" || value == "" {
		logger.Trace("add: missing required input — entering wizard")
		var confirmed bool
		key, value, targetName, varType, confirmed, err = wizard.RunAddWizard(dryRun)
		if err != nil {
			return fmt.Errorf("wizard error: %w", err)
		}
		if !confirmed {
			logger.Result("add: aborted by user in wizard")
			fmt.Fprintln(infoOut, "Aborted.")
			return nil
		}
	} else {
		varType, err = targets.VarTypeFromString(varTypeStr)
		if err != nil {
			return err
		}
		if detector.IsSecret(key) {
			fmt.Fprintf(os.Stderr, "WARNING: %s looks like a secret/API key.\n", key)
		}

		// If no target specified, use placer to auto-suggest
		if targetName == "" {
			logger.Step("auto-placement")
			info, detectErr := detector.Detect()
			if detectErr != nil {
				return fmt.Errorf("cannot detect environment for auto-placement: %w", detectErr)
			}
			p := placer.NewPlacer(info)
			intent := placer.DetectIntent(key, value, varType)
			logger.Trace("placer intent: category=%s scope=%s", intent.Category, intent.Scope)
			placements := p.Analyze(intent)
			if len(placements) == 0 {
				return fmt.Errorf("cannot determine optimal config file for %s", key)
			}
			for i, pl := range placements {
				logger.Trace("  candidate %d: %-22s confidence=%.0f%% reason=%q", i+1, pl.File.Name, pl.Confidence*100, pl.Reason)
			}
			top := placements[0]
			logger.Decision("placed=%s path=%s confidence=%.0f%% update=%v scope_mismatch=%v dups=%d reason=%q",
				top.File.Name, top.File.Path, top.Confidence*100, top.Update,
				top.ScopeMismatch, len(top.DuplicateLocations), top.Reason)
			fmt.Fprintf(infoOut, "Auto-placed: %s (%s)\n", top.File.Name, top.File.Path)
			fmt.Fprintf(infoOut, "Reason: %s (confidence: %.0f%%)\n", top.Reason, top.Confidence*100)
			if top.Update {
				fmt.Fprintln(infoOut, "Note: This key already exists — it will be updated.")
			}
			if top.ScopeMismatch {
				fmt.Fprintf(os.Stderr,
					"WARNING: %s effective scope is %s (declared %s). Variables may not reach %s shells. Run `envset audit` for details.\n",
					top.File.Name, top.EffectiveScope, top.File.Scope, intent.Scope)
				logger.Trace("ops.Add: scope mismatch on top placement; warned on stderr")
			}
			if len(top.DuplicateLocations) > 0 {
				fmt.Fprintf(os.Stderr,
					"WARNING: %q is also defined in: %s — those copies will be shadowed.\n",
					key, strings.Join(top.DuplicateLocations, ", "))
				logger.Trace("ops.Add: duplicate locations on top placement: %v", top.DuplicateLocations)
			}
			targetName = top.File.Name

			if !autoYes {
				logger.Result("auto-placement awaiting --yes confirmation; aborted")
				fmt.Fprintln(infoOut, "Use --yes to skip this confirmation.")
				return fmt.Errorf("auto-placement requires --yes to confirm")
			}
		}
	}

	home, _ := os.UserHomeDir()
	t, err := targets.Factory(targetName, home)
	if err != nil {
		return err
	}

	if !dryRun {
		preview, _ := t.Preview(key, value, varType)
		fmt.Fprintf(infoOut, "Adding to %s:\n  %s\n", t.Path(), preview)
	}

	logger.Step("write")
	if err := t.Add(key, value, varType, dryRun); err != nil {
		return err
	}
	if dryRun {
		logger.Result("add: dry-run — would write %s=%s to %s", key, redact(key, value), t.Path())
	} else {
		logger.Result("add: wrote %s to %s", key, t.Path())
	}

	// Post-write: emit source hint or eval-able source command
	if !dryRun {
		if printSource {
			PrintSourceCommand(os.Stdout, t.Path())
		} else {
			PrintReloadHint(t.Path())
		}
	}
	return nil
}

// Remove removes (or comments out) an environment variable entry.
func Remove(key, targetName string, comment, dryRun, printSource bool) error {
	logger.Step("ops.Remove")
	logger.Input("key=%q target=%q comment=%v dry_run=%v print_source=%v",
		key, targetName, comment, dryRun, printSource)

	infoOut := os.Stdout
	if printSource {
		infoOut = os.Stderr
	}

	if key == "" || targetName == "" {
		logger.Trace("remove: missing input — entering wizard")
		var confirmed bool
		var err error
		key, targetName, comment, confirmed, err = wizard.RunRemoveWizard()
		if err != nil {
			return fmt.Errorf("wizard error: %w", err)
		}
		if !confirmed {
			logger.Result("remove: aborted by user in wizard")
			fmt.Fprintln(infoOut, "Aborted.")
			return nil
		}
	}

	home, _ := os.UserHomeDir()
	t, err := targets.Factory(targetName, home)
	if err != nil {
		return err
	}

	logger.Step("write")
	if err := t.Remove(key, comment, dryRun); err != nil {
		return err
	}
	action := "deleted"
	if comment {
		action = "commented"
	}
	if dryRun {
		logger.Result("remove: dry-run — would have %s %s from %s", action, key, t.Path())
	} else {
		logger.Result("remove: %s %s in %s", action, key, t.Path())
	}

	if !dryRun {
		if printSource {
			PrintSourceCommand(os.Stdout, t.Path())
		} else {
			PrintReloadHint(t.Path())
		}
	}
	return nil
}

// List lists entries in a target config.
func List(key, targetName string) error {
	logger.Step("ops.List")
	logger.Input("key=%q target=%q", key, targetName)

	if targetName == "" {
		logger.Trace("list: target missing — entering wizard")
		var err error
		targetName, err = wizard.RunListWizard()
		if err != nil {
			return fmt.Errorf("wizard error: %w", err)
		}
	}

	home, _ := os.UserHomeDir()
	t, err := targets.Factory(targetName, home)
	if err != nil {
		return err
	}

	entries, err := t.List(key)
	if err != nil {
		return err
	}
	logger.Result("list: %d entries in %s", len(entries), t.Path())

	if len(entries) == 0 {
		fmt.Printf("No entries found in %s\n", t.Path())
		return nil
	}

	fmt.Printf("Entries in %s:\n", t.Path())
	for _, e := range entries {
		prefix := ""
		if e.IsComment {
			prefix = "[commented] "
		}
		line := strings.TrimSpace(e.RawLine)
		if line == "" {
			continue
		}
		fmt.Printf("  line %d: %s%s\n", e.LineNum, prefix, line)
	}

	return nil
}

// Detect prints detected environment information.
func Detect() error {
	logger.Step("ops.Detect")
	info, err := detector.Detect()
	if err != nil {
		return err
	}
	logger.Trace("detector: os=%s shell=%s login_shell=%s", info.OS, info.Shell, info.LoginShell)
	logger.Result("detect: shell=%s login_shell=%s configs=%d", info.Shell, info.LoginShell, len(info.ShellConfigs))

	fmt.Printf("Detected OS:         %s\n", info.OS)
	fmt.Printf("Current Shell:       %s (from process tree)\n", info.Shell)
	fmt.Printf("Login Shell:         %s (from $SHELL)\n", info.LoginShell)
	fmt.Printf("Home Directory:      %s\n", info.HomeDir)
	fmt.Println("Shell Configs:")
	for _, c := range info.ShellConfigs {
		status := "missing"
		if c.Exists {
			status = "exists"
		}
		fmt.Printf("  %-15s %s (%s)\n", c.Name, c.Path, status)
	}

	if suggested := info.SuggestConfig(); suggested != nil {
		fmt.Printf("Suggested target:   %s (%s)\n", suggested.Name, suggested.Path)
	}

	return nil
}

// Audit scans every registered config file for placement / performance issues
// and prints a human-readable report.
//
//   - fix=true:    also compute and display a diff preview of auto-fixes.
//   - dryRun=true: show the diff but do not write anything (implies fix).
//   - yes=true:    apply all fixes without an interactive confirmation prompt.
//
// Returns a non-nil error iff any finding has severity "error" (after fixes are
// applied in fix mode the report is re-evaluated).
func Audit(fix, dryRun, yes bool) error {
	logger.Step("ops.Audit")
	logger.Input("fix=%v dry_run=%v yes=%v", fix, dryRun, yes)
	info, err := detector.Detect()
	if err != nil {
		return err
	}
	p := placer.NewPlacer(info)
	a := audit.New(p.Registry)
	r := a.Run()
	counts := r.CountBySeverity()
	logger.Decision("audit: errors=%d warnings=%d info=%d",
		counts[audit.SevError], counts[audit.SevWarn], counts[audit.SevInfo])
	if logger.Verbose() {
		for i, f := range r.Findings {
			logger.Trace("  finding %d: [%s] %s @ %s:%d", i+1, f.Severity, f.Title, f.File, f.Line)
		}
	}
	audit.PrintReport(os.Stdout, r)

	if fix || dryRun {
		applied, err := runAuditFix(r, dryRun, yes)
		if err != nil {
			return err
		}
		if applied {
			// Re-evaluate after fixes so the exit code reflects the actual
			// post-fix state of the files on disk.
			r2 := audit.New(p.Registry).Run()
			counts = r2.CountBySeverity()
			logger.Decision("audit post-fix: errors=%d warnings=%d info=%d",
				counts[audit.SevError], counts[audit.SevWarn], counts[audit.SevInfo])
		}
	}

	if counts[audit.SevError] > 0 {
		logger.Result("audit: %d error-level issue(s) — exit non-zero", counts[audit.SevError])
		return fmt.Errorf("audit found %d error-level issue(s)", counts[audit.SevError])
	}
	logger.Result("audit: no error-level issues")
	return nil
}

// runAuditFix handles the --fix sub-workflow: diff preview + optional apply.
// Returns (true, nil) when fixes were actually written to disk.
func runAuditFix(r audit.Report, dryRun, yes bool) (bool, error) {
	ops, err := audit.BuildFixOps(r)
	if err != nil {
		return false, fmt.Errorf("building fix ops: %w", err)
	}

	audit.PrintDiff(os.Stdout, ops)

	if len(ops) == 0 {
		return false, nil
	}

	if dryRun {
		fmt.Fprintln(os.Stdout, "(dry-run: no files modified)")
		logger.Result("audit --fix --dry-run: %d op(s) previewed, nothing written", len(ops))
		return false, nil
	}

	if !yes {
		ok, promptErr := confirmPrompt(fmt.Sprintf("Apply %d fix(es)? Backups will be created as <file>.envset.bak", len(ops)))
		if promptErr != nil || !ok {
			fmt.Fprintln(os.Stdout, "Aborted — no changes made.")
			logger.Result("audit --fix: user declined")
			return false, nil
		}
	}

	if err := audit.ApplyFixes(ops); err != nil {
		return false, fmt.Errorf("applying fixes: %w", err)
	}

	// Report which files were modified.
	seen := map[string]bool{}
	for _, op := range ops {
		if !seen[op.File] {
			seen[op.File] = true
			fmt.Fprintf(os.Stdout, "  fixed  %s  (backup: %s.envset.bak)\n", op.File, op.File)
		}
	}
	fmt.Fprintf(os.Stdout, "\nApplied %d fix(es). Run `envset audit` to verify.\n", len(ops))
	logger.Result("audit --fix: applied %d op(s)", len(ops))
	return true, nil
}

// confirmPrompt prints a [y/N] prompt and returns true iff the user answers "y".
func confirmPrompt(msg string) (bool, error) {
	fmt.Fprintf(os.Stdout, "%s [y/N]: ", msg)
	var resp string
	_, err := fmt.Fscanln(os.Stdin, &resp)
	if err != nil {
		return false, nil // treat EOF/scan error as "no"
	}
	return strings.EqualFold(strings.TrimSpace(resp), "y"), nil
}

// Reload prints `source <file>` lines suitable for `eval "$(envset reload ...)"`.
// If targetName is empty, all *existing* files in the registry are emitted in order.
func Reload(targetName string) error {
	logger.Step("ops.Reload")
	logger.Input("target=%q", targetName)

	info, err := detector.Detect()
	if err != nil {
		return err
	}
	p := placer.NewPlacer(info)

	if targetName != "" {
		f, err := p.Registry.FindByName(targetName)
		if err != nil {
			return err
		}
		if !f.Exists {
			return fmt.Errorf("file %s does not exist: %s", f.Name, f.Path)
		}
		PrintSourceCommand(os.Stdout, f.Path)
		logger.Result("reload: emitted source %s", f.Path)
		return nil
	}

	// All existing files
	count := 0
	for _, f := range p.Registry.Files {
		if f.Exists {
			PrintSourceCommand(os.Stdout, f.Path)
			count++
		}
	}
	logger.Result("reload: emitted %d source line(s)", count)
	return nil
}

// redact replaces the value with [redacted] when the key looks like a secret.
// Avoids leaking API tokens / passwords into the audit log.
func redact(key, value string) string {
	if detector.IsSecret(key) {
		return "[redacted]"
	}
	return value
}
