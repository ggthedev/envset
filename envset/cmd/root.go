package cmd

import (
	"fmt"
	"os"

	"github.com/experimentalist/envset/pkg/logger"
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
	verbose     bool

	auditFix    bool
	auditDryRun bool
	auditYes    bool
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
  audit    Audit shell config files for placement & performance issues
  detect   Show detected shell and configuration files
  reload   Print source commands for one or all config files
`,
	// Errors from RunE are application-level, not CLI-usage problems.
	SilenceUsage:  true,
	SilenceErrors: true,

	// Open the audit log before any subcommand runs.
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logger.Init(verbose, os.Args)
		logger.Step("command:" + cmd.Name())
	},
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

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit shell config files for placement & performance issues",
	Long: `Scan every known shell config file (standard + custom ~/.gg_system_*) and
report:
  - broken syntax (e.g. PATH assignments that silently corrupt $PATH)
  - PATH/exports placed in interactive-only files
  - source-chain mismatches (file scope narrower than its content's intent)
  - duplicate compinit invocations
  - eager init of slow tools that should be lazy-loaded
  - keys defined in multiple files (dead code)

Exits non-zero if any error-level finding is reported.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ops.Audit(auditFix, auditDryRun, auditYes)
	},
}

func init() {
	rootCmd.AddCommand(addCmd, removeCmd, listCmd, detectCmd, reloadCmd, auditCmd)

	// add flags
	// Persistent flags (apply to every subcommand)
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Trace flow of control to log file and mirror to stderr")

	addCmd.Flags().StringVarP(&key, "key", "k", "", "Variable name")
	addCmd.Flags().StringVar(&value, "value", "", "Variable value")
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

	// audit flags
	auditCmd.Flags().BoolVar(&auditFix, "fix", false, "Apply safe automatic fixes (comment out broken lines)")
	auditCmd.Flags().BoolVar(&auditDryRun, "dry-run", false, "Preview fixes without writing (use with --fix)")
	auditCmd.Flags().BoolVar(&auditYes, "yes", false, "Apply all fixes without interactive confirmation")
}

// Execute runs the root command and finalises the audit log.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		logger.Result("error: %v", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	} else {
		logger.Result("ok")
	}
	logger.Close()
	if err != nil {
		os.Exit(1)
	}
}
