package ops

import (
	"fmt"
	"os"
	"strings"

	"github.com/experimentalist/envset/pkg/audit"
	"github.com/experimentalist/envset/pkg/detector"
	"github.com/experimentalist/envset/pkg/placer"
	"github.com/experimentalist/envset/pkg/targets"
	"github.com/experimentalist/envset/pkg/wizard"
)

// Add adds an environment variable entry.
// When printSource is true, only the `source <file>` command is written to stdout
// (so you can `eval "$(envset add ... --print-source)"`). All other output goes to stderr.
func Add(key, value, targetName, varTypeStr string, dryRun, autoYes, printSource bool) error {
	var varType targets.VarType
	var err error

	// Route human-readable output
	infoOut := os.Stdout
	if printSource {
		infoOut = os.Stderr
	}

	// If any required flag is missing, run wizard
	if key == "" || value == "" {
		var confirmed bool
		key, value, targetName, varType, confirmed, err = wizard.RunAddWizard(dryRun)
		if err != nil {
			return fmt.Errorf("wizard error: %w", err)
		}
		if !confirmed {
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
			info, detectErr := detector.Detect()
			if detectErr != nil {
				return fmt.Errorf("cannot detect environment for auto-placement: %w", detectErr)
			}
			p := placer.NewPlacer(info)
			intent := placer.DetectIntent(key, value, varType)
			placements := p.Analyze(intent)
			if len(placements) == 0 {
				return fmt.Errorf("cannot determine optimal config file for %s", key)
			}
			top := placements[0]
			fmt.Fprintf(infoOut, "Auto-placed: %s (%s)\n", top.File.Name, top.File.Path)
			fmt.Fprintf(infoOut, "Reason: %s (confidence: %.0f%%)\n", top.Reason, top.Confidence*100)
			if top.Update {
				fmt.Fprintln(infoOut, "Note: This key already exists — it will be updated.")
			}
			targetName = top.File.Name

			if !autoYes {
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

	if err := t.Add(key, value, varType, dryRun); err != nil {
		return err
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
	infoOut := os.Stdout
	if printSource {
		infoOut = os.Stderr
	}

	if key == "" || targetName == "" {
		var confirmed bool
		var err error
		key, targetName, comment, confirmed, err = wizard.RunRemoveWizard()
		if err != nil {
			return fmt.Errorf("wizard error: %w", err)
		}
		if !confirmed {
			fmt.Fprintln(infoOut, "Aborted.")
			return nil
		}
	}

	home, _ := os.UserHomeDir()
	t, err := targets.Factory(targetName, home)
	if err != nil {
		return err
	}

	if err := t.Remove(key, comment, dryRun); err != nil {
		return err
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
	if targetName == "" {
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
	info, err := detector.Detect()
	if err != nil {
		return err
	}

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
// and prints a human-readable report. Returns a non-nil error iff any finding
// has severity "error".
func Audit() error {
	info, err := detector.Detect()
	if err != nil {
		return err
	}
	p := placer.NewPlacer(info)
	a := audit.New(p.Registry)
	r := a.Run()
	audit.PrintReport(os.Stdout, r)
	if r.CountBySeverity()[audit.SevError] > 0 {
		return fmt.Errorf("audit found %d error-level issue(s)", r.CountBySeverity()[audit.SevError])
	}
	return nil
}

// Reload prints `source <file>` lines suitable for `eval "$(envset reload ...)"`.
// If targetName is empty, all *existing* files in the registry are emitted in order.
func Reload(targetName string) error {
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
		return nil
	}

	// All existing files
	for _, f := range p.Registry.Files {
		if f.Exists {
			PrintSourceCommand(os.Stdout, f.Path)
		}
	}
	return nil
}
