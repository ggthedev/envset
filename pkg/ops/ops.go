package ops

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/experimentalist/envset/pkg/audit"
	"github.com/experimentalist/envset/pkg/detector"
	"github.com/experimentalist/envset/pkg/lazy"
	"github.com/experimentalist/envset/pkg/logger"
	"github.com/experimentalist/envset/pkg/placer"
	"github.com/experimentalist/envset/pkg/rules"
	"github.com/experimentalist/envset/pkg/targets"
	"github.com/experimentalist/envset/pkg/wizard"
)

// envsetVersion is stamped into ideals.json's GeneratedBy field.
// TBD: replace with -ldflags injection during release builds.
const envsetVersion = "dev"

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

	// Load layered rules: ideals (built-in) + custom (user-confirmed).
	// On first run the files are created in ~/.config/envset/.
	ideals, custom, err := rules.LoadOrInit(envsetVersion)
	if err != nil {
		return fmt.Errorf("loading rules: %w", err)
	}
	logger.Trace("rules loaded: ideals.checks=%d custom.rules=%d", len(ideals.Checks), len(custom.Rules))

	a := audit.New(p.Registry)
	r := a.Run()
	r.ApplyCustomRules(custom)

	counts := r.CountBySeverity()
	logger.Decision("audit: errors=%d warnings=%d info=%d acknowledged=%d",
		counts[audit.SevError], counts[audit.SevWarn], counts[audit.SevInfo], countAcknowledged(r))
	if logger.Verbose() {
		for i, f := range r.Findings {
			logger.Trace("  finding %d: [%s] %s @ %s:%d ack=%v", i+1, f.Severity, f.Title, f.File, f.Line, f.Acknowledged)
		}
	}
	audit.PrintReport(os.Stdout, r)

	if fix || dryRun {
		applied, err := runAuditFix(r, &custom, ideals, dryRun, yes)
		if err != nil {
			return err
		}
		if applied {
			// Re-evaluate after fixes so the exit code reflects the actual
			// post-fix state of the files on disk.
			r2 := audit.New(p.Registry).Run()
			r2.ApplyCustomRules(custom)
			counts = r2.CountBySeverity()
			logger.Decision("audit post-fix: errors=%d warnings=%d info=%d",
				counts[audit.SevError], counts[audit.SevWarn], counts[audit.SevInfo])
		}
	}

	// Active errors only — acknowledged ones don't fail the build.
	activeErrors := 0
	for _, f := range r.Findings {
		if f.Severity == audit.SevError && !f.Acknowledged {
			activeErrors++
		}
	}
	if activeErrors > 0 {
		logger.Result("audit: %d error-level issue(s) — exit non-zero", activeErrors)
		return fmt.Errorf("audit found %d error-level issue(s)", activeErrors)
	}
	logger.Result("audit: no error-level issues")
	return nil
}

func countAcknowledged(r audit.Report) int {
	n := 0
	for _, f := range r.Findings {
		if f.Acknowledged {
			n++
		}
	}
	return n
}

// runAuditFix handles the --fix sub-workflow.
//
// Two modes:
//
//   - --yes (non-interactive): apply every auto-fixable finding, no prompts,
//     no overrides created. Backwards-compatible with the original flow.
//
//   - default (interactive): walk every active (non-acknowledged) finding and
//     prompt per-finding [a/s/o]: apply / skip / override. Overrides become
//     entries in custom.json. Then apply the chosen fixes.
//
// Regardless of mode, custom.json is refreshed on disk: impact strings are
// re-snapshotted from current ideals, and rules whose target file no longer
// exists are pruned. This is the "training" loop the user described.
func runAuditFix(r audit.Report, custom *rules.Custom, ideals rules.Ideals, dryRun, yes bool) (bool, error) {
	// Always refresh metadata, even if user makes no choices this run.
	if rules.RefreshImpacts(custom, ideals) {
		logger.Trace("custom rules: impact strings refreshed from ideals")
	}
	if pruned := rules.PruneStale(custom); pruned > 0 {
		logger.Trace("custom rules: pruned %d stale rule(s)", pruned)
	}

	if yes {
		return runAuditFixYes(r, custom, dryRun)
	}
	return runAuditFixInteractive(r, custom, ideals, dryRun)
}

// runAuditFixYes is the non-interactive path: apply all fixable findings, no overrides.
func runAuditFixYes(r audit.Report, custom *rules.Custom, dryRun bool) (bool, error) {
	ops, err := audit.BuildFixOps(r)
	if err != nil {
		return false, fmt.Errorf("building fix ops: %w", err)
	}
	audit.PrintDiff(os.Stdout, ops)

	// Persist any rule-metadata refresh even when no fixes are applied.
	if err := rules.SaveCustom(*custom); err != nil {
		logger.Trace("save custom: %v", err)
	}

	if len(ops) == 0 {
		return false, nil
	}
	if dryRun {
		fmt.Fprintln(os.Stdout, "(dry-run: no files modified)")
		logger.Result("audit --fix --dry-run: %d op(s) previewed, nothing written", len(ops))
		return false, nil
	}
	if err := audit.ApplyFixes(ops); err != nil {
		return false, fmt.Errorf("applying fixes: %w", err)
	}
	reportApplied(ops)
	return true, nil
}

// runAuditFixInteractive walks each active finding and prompts.
func runAuditFixInteractive(r audit.Report, custom *rules.Custom, ideals rules.Ideals, dryRun bool) (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	var toApply []audit.Finding
	addedRules := 0

	// Build the working list — only non-acknowledged findings need decisions.
	var pending []audit.Finding
	for _, f := range r.Findings {
		if !f.Acknowledged {
			pending = append(pending, f)
		}
	}
	if len(pending) == 0 {
		fmt.Fprintln(os.Stdout, "Nothing to review.")
		if err := rules.SaveCustom(*custom); err != nil {
			logger.Trace("save custom: %v", err)
		}
		return false, nil
	}

	fmt.Fprintf(os.Stdout, "\nInteractive review — %d finding(s).\n", len(pending))
	fmt.Fprintln(os.Stdout, "Per finding: [a]pply auto-fix / [s]kip / [o]verride (acknowledge as user-confirmed) / [q]uit")

reviewLoop:
	for i, f := range pending {
		fmt.Fprintf(os.Stdout, "\n--- [%d/%d] %s\n", i+1, len(pending), f.Title)
		if f.Line > 0 {
			fmt.Fprintf(os.Stdout, "    %s:%d\n", f.File, f.Line)
		}
		if f.Code != "" {
			fmt.Fprintf(os.Stdout, "    > %s\n", f.Code)
		}
		if f.ApproxImpact != "" {
			fmt.Fprintf(os.Stdout, "    approx impact: %s\n", f.ApproxImpact)
		}

		opts := "[s]kip / [o]verride / [q]uit"
		if f.Fixable() {
			opts = "[a]pply / " + opts
		}
		choice := promptChoice(reader, opts, f.Fixable())
		switch choice {
		case "a":
			toApply = append(toApply, f)
		case "s":
			// no-op for this run
		case "o":
			rule := buildCustomRule(reader, f, ideals)
			custom.Rules = append(custom.Rules, rule)
			addedRules++
			fmt.Fprintf(os.Stdout, "    recorded user-confirmed exception for %s.\n", f.CheckID)
		case "q":
			fmt.Fprintln(os.Stdout, "Aborted review.")
			break reviewLoop
		}
	}

	// Persist custom.json regardless (refresh + any new rules).
	if err := rules.SaveCustom(*custom); err != nil {
		return false, fmt.Errorf("saving custom rules: %w", err)
	}
	if addedRules > 0 {
		fmt.Fprintf(os.Stdout, "\nRecorded %d new user-confirmed exception(s) in custom.json.\n", addedRules)
	}

	// Apply selected fixes.
	if len(toApply) == 0 {
		return false, nil
	}
	subset := audit.Report{Findings: toApply}
	ops, err := audit.BuildFixOps(subset)
	if err != nil {
		return false, fmt.Errorf("building fix ops: %w", err)
	}
	audit.PrintDiff(os.Stdout, ops)
	if dryRun || len(ops) == 0 {
		if dryRun {
			fmt.Fprintln(os.Stdout, "(dry-run: no files modified)")
		}
		return false, nil
	}
	if err := audit.ApplyFixes(ops); err != nil {
		return false, fmt.Errorf("applying fixes: %w", err)
	}
	reportApplied(ops)
	return true, nil
}

// promptChoice loops until a valid single-letter choice is given.
// allowApply controls whether 'a' is accepted.
func promptChoice(r *bufio.Reader, opts string, allowApply bool) string {
	for {
		fmt.Fprintf(os.Stdout, "    choice %s: ", opts)
		line, err := r.ReadString('\n')
		if err != nil {
			return "q"
		}
		c := strings.ToLower(strings.TrimSpace(line))
		switch c {
		case "a":
			if allowApply {
				return "a"
			}
		case "s", "o", "q":
			return c
		}
		fmt.Fprintln(os.Stdout, "    invalid choice; try again")
	}
}

// buildCustomRule prompts for the optional reason and constructs a CustomRule
// from the finding. Subject is preferred; LineHash is recorded as fallback.
func buildCustomRule(r *bufio.Reader, f audit.Finding, ideals rules.Ideals) rules.CustomRule {
	fmt.Fprint(os.Stdout, "    reason (optional, press Enter to skip): ")
	reasonLine, _ := r.ReadString('\n')
	reason := strings.TrimSpace(reasonLine)

	impact := f.ApproxImpact
	if check := ideals.FindCheck(f.CheckID); check != nil {
		impact = check.ApproxImpact
	}

	return rules.CustomRule{
		CheckID: f.CheckID,
		Match: rules.Match{
			File:     f.File,
			Subject:  f.Subject,
			LineHash: rules.LineHash(f.Code),
		},
		Action:       rules.ActionAcknowledge,
		Reason:       reason,
		ApproxImpact: impact,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
}

func reportApplied(ops []audit.FixOp) {
	seen := map[string]bool{}
	for _, op := range ops {
		if !seen[op.File] {
			seen[op.File] = true
			fmt.Fprintf(os.Stdout, "  fixed  %s  (backup: %s.envset.bak)\n", op.File, op.File)
		}
	}
	fmt.Fprintf(os.Stdout, "\nApplied %d fix(es). Run `envset audit` to verify.\n", len(ops))
	logger.Result("audit --fix: applied %d op(s)", len(ops))
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
		var path string
		f, err := p.Registry.FindByName(targetName)
		if err == nil {
			if !f.Exists {
				return fmt.Errorf("file %s does not exist: %s", f.Name, f.Path)
			}
			path = f.Path
		} else {
			// Fallback to targets.Factory
			t, err := targets.Factory(targetName, info.HomeDir)
			if err != nil {
				return fmt.Errorf("no registered file or target named %q", targetName)
			}
			if !t.Exists() {
				return fmt.Errorf("file %s does not exist: %s", t.Name(), t.Path())
			}
			path = t.Path()
		}
		PrintSourceCommand(os.Stdout, path)
		logger.Result("reload: emitted source %s", path)
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

// Lazy generates a lazy-load wrapper for the given tool and writes it to stdout.
// If write is true, the wrapper is also appended to the appropriate interactive
// config file (.zshrc or .bashrc).
func Lazy(toolName, initCmd string, write bool) error {
	logger.Step("ops.Lazy")
	logger.Input("tool=%q init_cmd=%q write=%v", toolName, initCmd, write)

	info, err := detector.Detect()
	if err != nil {
		return err
	}

	spec, err := lazy.Resolve(toolName, info.Shell, initCmd)
	if err != nil {
		return err
	}

	snippet := lazy.Generate(spec)
	fmt.Fprint(os.Stdout, snippet)

	if !write {
		logger.Result("lazy: printed snippet for %s (no write)", toolName)
		return nil
	}

	// Write to the interactive config file.
	p := placer.NewPlacer(info)
	var target *placer.ConfigFile
	for i, f := range p.Registry.Files {
		if f.Scope == placer.ScopeInteractive && f.Exists {
			target = &p.Registry.Files[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("no interactive config file found to write lazy wrapper")
	}

	// Append to file.
	fh, err := os.OpenFile(target.Path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", target.Path, err)
	}
	defer fh.Close()
	if _, err := fh.WriteString("\n" + snippet); err != nil {
		return fmt.Errorf("writing to %s: %w", target.Path, err)
	}

	fmt.Fprintf(os.Stderr, "Appended lazy wrapper to %s\n", target.Path)
	logger.Result("lazy: appended snippet for %s to %s", toolName, target.Path)
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
