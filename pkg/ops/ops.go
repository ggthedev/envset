package ops

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/experimentalist/envset/pkg/audit"
	"github.com/experimentalist/envset/pkg/detector"
	"github.com/experimentalist/envset/pkg/fsutil"
	"github.com/experimentalist/envset/pkg/lazy"
	"github.com/experimentalist/envset/pkg/logger"
	"github.com/experimentalist/envset/pkg/placer"
	"github.com/experimentalist/envset/pkg/rules"
	"github.com/experimentalist/envset/pkg/targets"
	"github.com/experimentalist/envset/pkg/term"
	"github.com/experimentalist/envset/pkg/wizard"
)

// envsetVersion is stamped into ideals.json's GeneratedBy field.
// TBD: replace with -ldflags injection during release builds.
const envsetVersion = "dev"

// Add adds an environment variable entry.
// When printSource is true, only the `source <file>` command is written to stdout
// (so you can `eval "$(envset add ... --print-source)"`). All other output goes to stderr.
//
// quote selects value encoding: targets.QuoteExpand (default), QuoteLiteral or QuoteRaw.
func Add(key, value, targetName, varTypeStr string, quote targets.QuoteMode, dryRun, autoYes, printSource bool) error {
	logger.Step("ops.Add")
	logger.Input("key=%q value=%q target=%q type=%q quote=%q dry_run=%v auto_yes=%v print_source=%v",
		key, redact(key, value), targetName, varTypeStr, quote, dryRun, autoYes, printSource)

	var varType targets.VarType
	var err error

	// Route human-readable output
	infoOut := os.Stdout
	if printSource {
		infoOut = os.Stderr
	}

	// PATH entries don't need --key.
	if key == "" && (varTypeStr == string(targets.VarTypePathAppend) || varTypeStr == string(targets.VarTypePathPrepend)) {
		key = "PATH"
	}

	// If any required flag is missing, run the wizard — but only when a human
	// can answer it. Under cron, pipes, eval "$(…)" or with --yes, fail fast.
	if key == "" || value == "" {
		if autoYes || !term.Interactive() {
			return fmt.Errorf("--key and --value are required when not running interactively (or with --yes)")
		}
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
				// A dry run tells "update" apart from "already set" for the
				// exact quoting the user asked for.
				if t, err := targets.Factory(top.File.Name, info.HomeDir); err == nil {
					if res, err := t.Add(key, value, varType, targets.WriteOptions{DryRun: true, Quote: quote}); err == nil {
						if res.Outcome == targets.Unchanged {
							fmt.Fprintln(infoOut, "Note: already set to this value.")
						} else {
							fmt.Fprintln(infoOut, "Note: This key already exists — it will be updated.")
						}
					}
				}
			}
			if top.ScopeMismatch {
				fmt.Fprintf(os.Stderr,
					"WARNING: %s effective scope is %s (declared %s). Variables may not reach %s shells. Run `envset audit` for details.\n",
					top.File.Name, top.EffectiveScope, top.File.Scope, intent.Scope)
				logger.Trace("ops.Add: scope mismatch on top placement; warned on stderr")
			}
			targetName = top.File.Name

			if !autoYes {
				warnDuplicates(targetName, key)
				logger.Result("auto-placement awaiting --yes confirmation; aborted")
				fmt.Fprintln(infoOut, "Use --yes to skip this confirmation.")
				return fmt.Errorf("auto-placement requires --yes to confirm")
			}
		}
	}

	if varType == targets.VarTypePathAppend || varType == targets.VarTypePathPrepend {
		key = "PATH"
	}

	home, err := homeDir()
	if err != nil {
		return err
	}
	t, err := targets.Factory(targetName, home)
	if err != nil {
		return err
	}

	warnDuplicates(targetName, key)

	logger.Step("write")
	res, err := t.Add(key, value, varType, targets.WriteOptions{DryRun: dryRun, Quote: quote})
	if err != nil {
		return err
	}
	dry := ""
	if dryRun {
		dry = "[DRY-RUN] would have "
	}
	switch res.Outcome {
	case targets.Unchanged:
		fmt.Fprintf(infoOut, "No change: %s already contains:\n  %s\n", t.Path(), strings.Join(res.Lines, "\n  "))
		logger.Result("add: unchanged — %s already present in %s", key, t.Path())
		return nil
	case targets.Updated:
		fmt.Fprintf(infoOut, "%supdated %s line %d:\n  - %s\n  + %s\n", dry, t.Path(), res.LineNum,
			strings.TrimSpace(res.Previous), strings.Join(res.Lines, "\n  + "))
	default:
		fmt.Fprintf(infoOut, "%sadded to %s:\n  %s\n", dry, t.Path(), strings.Join(res.Lines, "\n  "))
	}
	if dryRun {
		logger.Result("add: dry-run — would %s %s=%s in %s", res.Outcome, key, redact(key, value), t.Path())
		return nil
	}
	reportBackup(infoOut, res.Backup)
	logger.Result("add: %s %s in %s", res.Outcome, key, t.Path())

	// Post-write: emit source hint or eval-able source command
	if printSource {
		PrintSourceCommand(os.Stdout, t.Path())
	} else {
		PrintReloadHint(t.Path(), targetName)
	}
	return nil
}

// warnDuplicates tells the user, on stderr, when key is also defined in other
// files and which of the values their shells will end up with.
func warnDuplicates(targetName, key string) {
	info, err := detector.Detect()
	if err != nil {
		return
	}
	reg := placer.NewPlacer(info).Registry
	f, err := reg.FindByName(targetName)
	if err != nil {
		return // an explicit file path outside the registry
	}
	for _, note := range reg.DuplicateNotes(*f, key) {
		fmt.Fprintf(os.Stderr, "WARNING: %s\n", note)
		logger.Warn("duplicate: %s", note)
	}
}

// reportBackup tells the user where the pre-change copy went.
func reportBackup(w *os.File, backup string) {
	if backup == "" {
		return
	}
	fmt.Fprintf(w, "Backup saved: %s  (undo: envset restore --target <file>)\n", fsutil.Tilde(backup))
	logger.Trace("backup saved: %s", backup)
}

// homeDir returns $HOME, failing loudly instead of silently writing relative
// paths into the current directory when it cannot be determined.
func homeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("cannot determine home directory (is $HOME set?): %v", err)
	}
	return home, nil
}

// Remove removes (or comments out) an environment variable entry.
//
// value, when non-empty, restricts removal to entries whose value contains it
// (required for PATH so one entry can be removed without wiping the others).
func Remove(key, value, targetName string, comment, dryRun, printSource bool) error {
	logger.Step("ops.Remove")
	logger.Input("key=%q value=%q target=%q comment=%v dry_run=%v print_source=%v",
		key, redact(key, value), targetName, comment, dryRun, printSource)

	infoOut := os.Stdout
	if printSource {
		infoOut = os.Stderr
	}

	if key == "" || targetName == "" {
		if !term.Interactive() {
			return fmt.Errorf("--key and --target are required when not running interactively")
		}
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

	home, err := homeDir()
	if err != nil {
		return err
	}
	t, err := targets.Factory(targetName, home)
	if err != nil {
		return err
	}

	logger.Step("write")
	res, err := t.Remove(key, targets.RemoveOptions{Comment: comment, DryRun: dryRun, Value: value})
	if err != nil {
		return err
	}
	if len(res.Affected) == 0 {
		fmt.Fprintf(infoOut, "No change: %s is already commented out in %s\n", key, t.Path())
		logger.Result("remove: unchanged — %s already commented in %s", key, t.Path())
		return nil
	}
	action := "deleted"
	if comment {
		action = "commented out"
	}
	dry := ""
	if dryRun {
		dry = "[DRY-RUN] would have "
	}
	fmt.Fprintf(infoOut, "%s%s %d line(s) in %s:\n", dry, action, len(res.Affected), t.Path())
	for _, e := range res.Affected {
		fmt.Fprintf(infoOut, "  line %d: %s\n", e.LineNum, strings.TrimSpace(e.RawLine))
	}
	if dryRun {
		logger.Result("remove: dry-run — would have %s %s from %s", action, key, t.Path())
		return nil
	}
	reportBackup(infoOut, res.Backup)
	logger.Result("remove: %s %s in %s", action, key, t.Path())

	if printSource {
		PrintSourceCommand(os.Stdout, t.Path())
	} else {
		PrintReloadHint(t.Path(), targetName)
	}
	return nil
}

// List lists entries in a target config.
func List(key, targetName string) error {
	logger.Step("ops.List")
	logger.Input("key=%q target=%q", key, targetName)

	if targetName == "" {
		if !term.Interactive() {
			return fmt.Errorf("--target is required when not running interactively")
		}
		logger.Trace("list: target missing — entering wizard")
		var err error
		targetName, err = wizard.RunListWizard()
		if err != nil {
			return fmt.Errorf("wizard error: %w", err)
		}
	}

	home, err := homeDir()
	if err != nil {
		return err
	}
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
		// Show definitions, not the shell's control flow (if/else/fi) or prose.
		if line == "" || (e.Key == "" && !targets.IsDefinitionLine(line)) {
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
	logger.Trace("detector: os=%s shell=%s source=%q running=%s login_shell=%s",
		info.OS, info.Shell, info.ShellSource, info.RunningShell, info.LoginShell)
	logger.Result("detect: shell=%s running=%s configs=%d", info.Shell, info.RunningShell, len(info.ShellConfigs))

	running := info.RunningShell
	if running == "" {
		running = "none (not started from a shell)"
	}
	fmt.Printf("Detected OS:         %s\n", info.OS)
	fmt.Printf("Placing for:         %s (from %s)\n", info.Shell, info.ShellSource)
	fmt.Printf("Running under:       %s\n", running)
	fmt.Printf("Login shell ($SHELL): %s\n", info.LoginShell)
	fmt.Printf("Home Directory:      %s\n", fsutil.Tilde(info.HomeDir))
	fmt.Printf("%s configs:\n", info.Shell)
	for _, c := range info.ShellConfigs {
		if c.Shell != info.Shell {
			continue
		}
		status := "missing"
		if c.Exists {
			status = "exists"
		}
		fmt.Printf("  %-15s %s (%s)\n", c.Name, fsutil.Tilde(c.Path), status)
	}
	if info.Shell != "" && info.RunningShell != "" && info.RunningShell != info.Shell {
		fmt.Printf("Note: running under %s, but placing for %s. Use --shell to change.\n", info.RunningShell, info.Shell)
	}

	if suggested := info.SuggestConfig(); suggested != nil {
		fmt.Printf("Suggested target:   %s (%s)\n", suggested.Name, suggested.Path)
	}

	return nil
}

// Audit scans every registered config file for placement / performance issues
// and prints a human-readable report.
//
//   - plain:        read-only report.
//   - dryRun=true:  also show the diff of every auto-fix; never prompts, never
//     writes (with or without --fix).
//   - fix=true:     interactive per-finding review (needs a terminal), or with
//     yes=true apply every auto-fix without prompting.
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
	// Loading never writes; ideals.json/custom.json are only persisted by --fix.
	loaded, err := rules.Load(envsetVersion)
	if err != nil {
		return fmt.Errorf("loading rules: %w", err)
	}
	if loaded.CustomWarning != nil {
		fmt.Fprintf(os.Stderr, "WARNING: %v\n", loaded.CustomWarning)
		logger.Warn("rules: %v", loaded.CustomWarning)
	}
	customBroken = loaded.CustomBroken
	ideals, custom := loaded.Ideals, loaded.Custom
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

	if dryRun {
		fixOps, err := audit.BuildFixOps(r)
		if err != nil {
			return fmt.Errorf("building fix ops: %w", err)
		}
		audit.PrintDiff(os.Stdout, fixOps)
		if len(fixOps) > 0 {
			fmt.Fprintln(os.Stdout, "(dry-run: no files modified)")
		}
		logger.Result("audit --dry-run: %d op(s) previewed, nothing written", len(fixOps))
	} else if fix {
		if !yes && !term.Interactive() {
			return fmt.Errorf("audit --fix needs a terminal for its review; use --yes to apply all fixes non-interactively, or --dry-run to preview")
		}
		if err := rules.PersistIdeals(loaded); err != nil {
			logger.Warn("writing ideals.json: %v", err)
		}
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
	if err := saveCustom(*custom); err != nil {
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
	backups, err := audit.ApplyFixes(ops)
	if err != nil {
		return false, fmt.Errorf("applying fixes: %w", err)
	}
	reportApplied(ops, backups)
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
		if err := saveCustom(*custom); err != nil {
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
	if err := saveCustom(*custom); err != nil {
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
	backups, err := audit.ApplyFixes(ops)
	if err != nil {
		return false, fmt.Errorf("applying fixes: %w", err)
	}
	reportApplied(ops, backups)
	return true, nil
}

// customBroken is set by Audit when custom.json could not be parsed: the
// file is then never overwritten, so the user's rules can be repaired.
var customBroken bool

func saveCustom(c rules.Custom) error {
	if customBroken {
		return fmt.Errorf("not saving custom.json: the existing file is malformed; fix or delete it first")
	}
	return rules.SaveCustom(c)
}

// promptChoice loops until a valid single-letter choice is given.
// allowApply controls whether 'a' is accepted.
func promptChoice(r *bufio.Reader, opts string, allowApply bool) string {
	for {
		fmt.Fprintf(os.Stderr, "    choice %s: ", opts)
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
		fmt.Fprintln(os.Stderr, "    invalid choice; try again")
	}
}

// buildCustomRule prompts for the optional reason and constructs a CustomRule
// from the finding. Subject is preferred; LineHash is recorded as fallback.
func buildCustomRule(r *bufio.Reader, f audit.Finding, ideals rules.Ideals) rules.CustomRule {
	fmt.Fprint(os.Stderr, "    reason (optional, press Enter to skip): ")
	reasonLine, _ := r.ReadString('\n')
	reason := strings.TrimSpace(reasonLine)

	impact := f.ApproxImpact
	if check := ideals.FindCheck(f.CheckID); check != nil {
		impact = check.ApproxImpact
	}

	return rules.CustomRule{
		CheckID: f.CheckID,
		Match: rules.Match{
			File:     rules.PortablePath(f.File),
			Subject:  f.Subject,
			LineHash: rules.LineHash(f.Code),
		},
		Action:       rules.ActionAcknowledge,
		Reason:       reason,
		ApproxImpact: impact,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
}

func reportApplied(ops []audit.FixOp, backups map[string]string) {
	seen := map[string]bool{}
	for _, op := range ops {
		if !seen[op.File] {
			seen[op.File] = true
			fmt.Fprintf(os.Stdout, "  fixed  %s  (backup: %s)\n", op.File, fsutil.Tilde(backups[op.File]))
		}
	}
	fmt.Fprintf(os.Stdout, "\nApplied %d fix(es). Run `envset audit` to verify.\n", len(ops))
	logger.Result("audit --fix: applied %d op(s)", len(ops))
}

// Reload prints `source <file>` lines suitable for `eval "$(envset reload ...)"`.
// If targetName is empty, it emits the existing files the user's shell reads on
// its own, in load order (files they source come along with them).
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

	files := p.Registry.StartupFiles()
	for _, path := range files {
		PrintSourceCommand(os.Stdout, path)
	}
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "No %s startup files exist yet.\n", info.Shell)
	}
	logger.Result("reload: emitted %d source line(s)", len(files))
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

	// Idempotent: don't stack a second wrapper for the same tool.
	header := lazy.Header(spec.Name)
	existing, err := os.ReadFile(target.Path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", target.Path, err)
	}
	for _, ln := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(ln) == header {
			fmt.Fprintf(os.Stderr, "No change: a lazy wrapper for %s is already in %s\n", toolName, target.Path)
			logger.Result("lazy: wrapper for %s already present in %s", toolName, target.Path)
			return nil
		}
	}

	updated := string(existing)
	if len(updated) > 0 && !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	updated += "\n" + snippet
	backup, err := targets.CommitFile(target.Path, targets.ShellForPath(target.Path), existing, true, []byte(updated), 0o644)
	if err != nil {
		return fmt.Errorf("writing to %s: %w", target.Path, err)
	}

	fmt.Fprintf(os.Stderr, "Appended lazy wrapper to %s\n", target.Path)
	reportBackup(os.Stderr, backup)
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

// Restore lists or restores the backups envset keeps for a config file.
func Restore(targetName, backupPath string, list, dryRun bool) error {
	logger.Step("ops.Restore")
	logger.Input("target=%q backup=%q list=%v dry_run=%v", targetName, backupPath, list, dryRun)

	home, err := homeDir()
	if err != nil {
		return err
	}
	t, err := targets.Factory(targetName, home)
	if err != nil {
		return err
	}
	backups, err := fsutil.ListBackups(t.Path())
	if err != nil {
		return err
	}

	if list {
		if len(backups) == 0 {
			fmt.Printf("No backups for %s\n", t.Path())
			return nil
		}
		fmt.Printf("Backups for %s (newest first):\n", t.Path())
		for _, b := range backups {
			note := ""
			if b.PreRestore {
				note = "  (saved automatically before a restore)"
			}
			fmt.Printf("  %s  %s%s\n", b.Time.Format("2006-01-02 15:04:05"), fsutil.Tilde(b.Path), note)
		}
		return nil
	}

	if backupPath == "" {
		for _, b := range backups {
			if !b.PreRestore {
				backupPath = b.Path
				break
			}
		}
		if backupPath == "" {
			return fmt.Errorf("no backups found for %s", t.Path())
		}
	}
	if dryRun {
		fmt.Printf("[DRY-RUN] would restore %s from %s\n", t.Path(), backupPath)
		return nil
	}
	restored, err := fsutil.Restore(t.Path(), backupPath)
	if err != nil {
		return err
	}
	fmt.Printf("Restored %s from %s\n", fsutil.Tilde(t.Path()), fsutil.Tilde(restored))
	fmt.Println("The previous contents were saved first; see `envset restore --list`.")
	logger.Result("restore: %s from %s", t.Path(), restored)
	PrintReloadHint(t.Path(), targetName)
	return nil
}
