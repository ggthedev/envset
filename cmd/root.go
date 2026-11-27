package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ggthedev/envset/pkg/detector"
	"github.com/ggthedev/envset/pkg/lazy"
	"github.com/ggthedev/envset/pkg/logger"
	"github.com/ggthedev/envset/pkg/ops"
	"github.com/ggthedev/envset/pkg/targets"
	"github.com/ggthedev/envset/pkg/version"
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

	lazyInitCmd string
	lazyWrite   bool

	literal bool
	raw     bool

	restoreList   bool
	restoreBackup string

	shellFlag string
	appendNew bool
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
  reload   Print source commands for a config file or your startup files
  restore  Undo the last change to a config file from its backup
  lazy     Generate a lazy-load wrapper for a slow-init tool
  version  Show which build is running

Every write is atomic and preceded by a backup in ~/.config/envset/backups.
`,
	// Errors from RunE are application-level, not CLI-usage problems.
	SilenceUsage:  true,
	SilenceErrors: true,

	// Open the audit log before any subcommand runs.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		logger.Init(verbose, os.Args)
		logger.Debug("command: %s", cmd.Name())
		return detector.SetShellOverride(shellFlag)
	},
}

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add an environment variable",
	Long: `Add or set an environment variable in a shell config file.

Adding a key that already exists updates it in place (keeping any trailing
comment); adding an identical line again is a no-op. If the key is set inside a
block (if, case, loop, function) or shares its line with other code, envset
refuses; --append adds a new line at the end that overrides it.

Value encoding:
  (default)   literal text, except $VAR / ${VAR} expand and a leading ~/ means $HOME/.
              Command substitution ($(...) or backticks) is rejected.
  --literal   store the value exactly, single-quoted; nothing expands.
  --raw       write the value unquoted and unchecked (you own the syntax).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if literal && raw {
			return fmt.Errorf("--literal and --raw are mutually exclusive")
		}
		quote := targets.QuoteExpand
		if literal {
			quote = targets.QuoteLiteral
		} else if raw {
			quote = targets.QuoteRaw
		}
		typed := "" // only a --type the user gave; the wizard asks otherwise
		if cmd.Flags().Changed("type") {
			typed = varType
		}
		return ops.Add(key, value, targetName, typed, quote, dryRun, yes, printSource, appendNew)
	},
}

var removeCmd = &cobra.Command{
	Use:     "remove",
	Aliases: []string{"rm", "delete", "del"},
	Short:   "Remove an environment variable",
	Long: `Remove (or comment out) an environment variable from a shell config file.
Comments out by default; pass --comment=false to delete the line(s).
For PATH, --value is required and selects which entry to remove.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ops.Remove(key, value, targetName, comment, dryRun, printSource, cmd.Flags().Changed("comment"))
	},
}

var reloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Print `source <file>` for a config file or your startup files",
	Long: `Print source commands for the given target, or for the files your shell reads
at startup, in load order. Use with eval to apply changes in your current shell:

  eval "$(envset reload --target zshenv)"
  eval "$(envset reload)"            # your shell's startup files
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
	Long: `Show the detected OS, the shell envset places entries for (and how it was
chosen), the shell it is running under, and that shell's configuration files.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ops.Detect()
	},
}

var lazyCmd = &cobra.Command{
	Use:   "lazy <tool>",
	Short: "Generate a lazy-load wrapper for a slow-init tool",
	Long: `Generate a shell function that defers tool initialization until first use.
This removes the startup cost of version managers such as pyenv or rbenv.

Examples:
  envset lazy pyenv              # print wrapper to stdout
  envset lazy rbenv --write      # append wrapper to your interactive config
  envset lazy mytool --init-cmd "mytool setup --shell zsh"

Supported tools: ` + strings.Join(lazy.KnownToolNames(), ", ") + `.

starship, oh-my-posh, direnv, atuin and mcfly are refused: their init installs
a prompt or hook, which a lazy wrapper would leave missing.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return ops.Lazy(args[0], lazyInitCmd, lazyWrite)
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show which build of envset is running",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.Full())
	},
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Undo the last change to a config file from its backup",
	Long: `Restore a config file from the backups envset takes before every write.

  envset restore --target zshrc --list        # show available backups
  envset restore --target zshrc               # restore the most recent one
  envset restore --target zshrc --backup <f>  # restore a specific backup

The current contents are saved first (marked pre-restore), so a restore can
itself be undone with --backup.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ops.Restore(targetName, restoreBackup, restoreList, dryRun)
	},
}

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit shell config files for placement & performance issues",
	Long: `Scan every known shell config file for your shell (plus ~/.gg_system_* when present) and
report:
  - broken syntax (e.g. PATH assignments that silently corrupt $PATH)
  - PATH/exports placed in interactive-only files
  - source-chain mismatches (file scope narrower than its content's intent)
  - duplicate compinit invocations
  - eager init of slow tools that should be lazy-loaded
  - keys defined in multiple files (dead code)

  envset audit              read-only report
  envset audit --dry-run    report + diff of every auto-fix; never prompts or writes
  envset audit --fix        review each finding interactively (needs a terminal)
  envset audit --fix --yes  apply every auto-fix without prompting

Exits non-zero if any error-level finding is reported.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ops.Audit(auditFix, auditDryRun, auditYes)
	},
}

func init() {
	// --version prints the same line as the version command.
	rootCmd.Version = version.String()
	rootCmd.SetVersionTemplate(version.Full() + "\n")
	rootCmd.AddCommand(versionCmd)

	rootCmd.AddCommand(addCmd, removeCmd, listCmd, detectCmd, reloadCmd, auditCmd, lazyCmd, restoreCmd)

	// add flags
	// Persistent flags (apply to every subcommand)
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Write a debug trace of every call to the log file")
	rootCmd.PersistentFlags().StringVar(&shellFlag, "shell", "", "Place entries for this shell (zsh, bash, fish) instead of auto-detecting; also $ENVSET_SHELL")

	addCmd.Flags().StringVarP(&key, "key", "k", "", "Variable name")
	addCmd.Flags().StringVar(&value, "value", "", "Variable value")
	addCmd.Flags().StringVarP(&targetName, "target", "t", "", "Target config file (e.g., zshrc, bashrc)")
	addCmd.Flags().StringVar(&varType, "type", "export", "Variable type: export, export_and_path_append, eval, source, path_append, path_prepend, fish_set")
	addCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without writing")
	addCmd.Flags().BoolVar(&yes, "yes", false, "Auto-confirm placer suggestions when --target is omitted")
	addCmd.Flags().BoolVar(&printSource, "print-source", false, "After writing, print only `source <file>` to stdout (eval-friendly)")
	addCmd.Flags().BoolVar(&literal, "literal", false, "Store the value verbatim in single quotes (no $VAR expansion)")
	addCmd.Flags().BoolVar(&appendNew, "append", false, "Add a new line at the end instead of editing existing ones")
	addCmd.Flags().BoolVar(&raw, "raw", false, "Write the value unquoted and unchecked (allows $(...) — use with care)")

	// remove flags
	removeCmd.Flags().StringVarP(&key, "key", "k", "", "Variable name to remove")
	removeCmd.Flags().StringVarP(&targetName, "target", "t", "", "Target config file")
	removeCmd.Flags().StringVar(&value, "value", "", "Only remove entries whose value contains this (required for PATH)")
	removeCmd.Flags().BoolVar(&comment, "comment", true, "Comment out instead of deleting")
	removeCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without writing")
	removeCmd.Flags().BoolVar(&printSource, "print-source", false, "After writing, print only `source <file>` to stdout (eval-friendly)")

	// list flags
	listCmd.Flags().StringVarP(&key, "key", "k", "", "Variable name filter (empty = all)")
	listCmd.Flags().StringVarP(&targetName, "target", "t", "", "Target config file")

	// reload flags
	reloadCmd.Flags().StringVarP(&targetName, "target", "t", "", "Target config file (omit for your shell's startup files)")

	// restore flags
	restoreCmd.Flags().StringVarP(&targetName, "target", "t", "", "Config file to restore (e.g. zshrc, launchd, or a path)")
	restoreCmd.Flags().BoolVar(&restoreList, "list", false, "List available backups instead of restoring")
	restoreCmd.Flags().StringVar(&restoreBackup, "backup", "", "Restore this specific backup file instead of the latest")
	restoreCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show which backup would be restored without writing")
	_ = restoreCmd.MarkFlagRequired("target")

	// audit flags
	auditCmd.Flags().BoolVar(&auditFix, "fix", false, "Apply safe automatic fixes (comment out broken lines)")
	auditCmd.Flags().BoolVar(&auditDryRun, "dry-run", false, "Show the diff of every auto-fix without prompting or writing")
	auditCmd.Flags().BoolVar(&auditYes, "yes", false, "Apply all fixes without interactive confirmation")

	// lazy flags
	lazyCmd.Flags().StringVar(&lazyInitCmd, "init-cmd", "", "Custom init command (overrides built-in lookup)")
	lazyCmd.Flags().BoolVar(&lazyWrite, "write", false, "Append the wrapper to your interactive config file")
}

// Execute runs the root command and finalises the log.
func Execute() {
	err := rootCmd.Execute()
	code := 0
	if err != nil {
		code = 1
		// A command line cobra rejects never reaches PersistentPreRunE. Its
		// error quotes the arguments, so only the redacted command line is logged.
		if !logger.Started() {
			logger.Init(verbose, os.Args)
			logger.Error(errors.New("invalid command line (details were shown on the terminal)"))
		} else {
			logger.Error(err)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	logger.Close(code)
	os.Exit(code)
}
