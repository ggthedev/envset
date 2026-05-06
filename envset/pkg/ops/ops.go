package ops

import (
	"fmt"
	"os"
	"strings"

	"github.com/experimentalist/envset/pkg/detector"
	"github.com/experimentalist/envset/pkg/targets"
	"github.com/experimentalist/envset/pkg/wizard"
)

// Add adds an environment variable entry.
func Add(key, value, targetName, varTypeStr string, dryRun bool) error {
	var varType targets.VarType
	var err error

	// If any required flag is missing, run wizard
	if key == "" || value == "" || targetName == "" {
		var confirmed bool
		key, value, targetName, varType, confirmed, err = wizard.RunAddWizard(dryRun)
		if err != nil {
			return fmt.Errorf("wizard error: %w", err)
		}
		if !confirmed {
			fmt.Println("Aborted.")
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
	}

	home, _ := os.UserHomeDir()
	t, err := targets.Factory(targetName, home)
	if err != nil {
		return err
	}

	if !dryRun {
		preview, _ := t.Preview(key, value, varType)
		fmt.Printf("Adding to %s:\n  %s\n", t.Path(), preview)
	}

	return t.Add(key, value, varType, dryRun)
}

// Remove removes (or comments out) an environment variable entry.
func Remove(key, targetName string, comment, dryRun bool) error {
	if key == "" || targetName == "" {
		var confirmed bool
		var err error
		key, targetName, comment, confirmed, err = wizard.RunRemoveWizard()
		if err != nil {
			return fmt.Errorf("wizard error: %w", err)
		}
		if !confirmed {
			fmt.Println("Aborted.")
			return nil
		}
	}

	home, _ := os.UserHomeDir()
	t, err := targets.Factory(targetName, home)
	if err != nil {
		return err
	}

	return t.Remove(key, comment, dryRun)
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

	fmt.Printf("Detected OS:        %s\n", info.OS)
	fmt.Printf("Detected Shell:     %s\n", info.Shell)
	fmt.Printf("Home Directory:     %s\n", info.HomeDir)
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
