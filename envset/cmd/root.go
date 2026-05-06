package cmd

import (
	"fmt"
	"os"

	"github.com/experimentalist/envset/pkg/ops"
	"github.com/spf13/cobra"
)

var (
	targetName  string
	key         string
	value       string
	varType     string
	dryRun      bool
	comment     bool
	yes         bool
	printSource bool
)

var rootCmd = &cobra.Command{
	Use:   "envset",
	Short: "Interactive environment variable setter for shell configs",
	Long: `envset helps you add, list, and remove environment variables
from your shell configuration files (zshrc, bashrc, etc.) interactively.

Subcommands:
  add      Add or set an environment variable
  remove   Remove or comment out an environment variable
  list     List environment variables in a config file
  detect   Show detected shell and configuration files
`,
}

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add an environment variable",
	Long:  `Add or set an environment variable in a shell config file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ops.Add(key, value, targetName, varType, dryRun, yes, printSource)
	},
}

var removeCmd = &cobra.Command{
	Use:     "remove",
	Aliases: []string{"rm", "delete", "del"},
	Short:   "Remove an environment variable",
	Long: `Remove (or comment out) an environment variable from a shell config file.
Use --comment to comment out instead of deleting.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ops.Remove(key, targetName, comment, dryRun, printSource)
	},
}

var reloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Print `source <file>` for one or all config files",
	Long: `Print source commands for the given target (or every existing config file).
Use with eval to apply changes in your current shell:

  eval "$(envset reload --target zshenv)"
  eval "$(envset reload)"            # all existing files
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ops.Reload(targetName)
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List environment variables",
	Long:  `List environment variables in a shell config file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ops.List(key, targetName)
	},
}

var detectCmd = &cobra.Command{
	Use:   "detect",
	Short: "Detect environment",
	Long:  `Show detected OS, shell, and configuration files.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ops.Detect()
	},
}

func init() {
	rootCmd.AddCommand(addCmd, removeCmd, listCmd, detectCmd, reloadCmd)

	// add flags
	addCmd.Flags().StringVarP(&key, "key", "k", "", "Variable name")
	addCmd.Flags().StringVarP(&value, "value", "v", "", "Variable value")
	addCmd.Flags().StringVarP(&targetName, "target", "t", "", "Target config file (e.g., zshrc, bashrc)")
	addCmd.Flags().StringVar(&varType, "type", "export", "Variable type: export, eval, source, path_append, path_prepend, fish_set")
	addCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without writing")
	addCmd.Flags().BoolVar(&yes, "yes", false, "Auto-confirm placer suggestions when --target is omitted")
	addCmd.Flags().BoolVar(&printSource, "print-source", false, "After writing, print only `source <file>` to stdout (eval-friendly)")

	// remove flags
	removeCmd.Flags().StringVarP(&key, "key", "k", "", "Variable name to remove")
	removeCmd.Flags().StringVarP(&targetName, "target", "t", "", "Target config file")
	removeCmd.Flags().BoolVar(&comment, "comment", true, "Comment out instead of deleting")
	removeCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without writing")
	removeCmd.Flags().BoolVar(&printSource, "print-source", false, "After writing, print only `source <file>` to stdout (eval-friendly)")

	// list flags
	listCmd.Flags().StringVarP(&key, "key", "k", "", "Variable name filter (empty = all)")
	listCmd.Flags().StringVarP(&targetName, "target", "t", "", "Target config file")

	// reload flags
	reloadCmd.Flags().StringVarP(&targetName, "target", "t", "", "Target config file (omit for all existing files)")
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
