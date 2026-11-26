package ops

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/experimentalist/envset/pkg/fsutil"
)

// PrintReloadHint prints a human-friendly hint to stderr after a write,
// telling the user how to apply the changes in the current shell.
func PrintReloadHint(path, targetName string) {
	if strings.HasSuffix(path, "launchd.sh") {
		fmt.Fprintln(os.Stderr, "\nApplied to this login session; GUI apps launched from now on will see it.")
		return
	}
	fmt.Fprintf(os.Stderr, "\nTo apply in your current shell, run:\n  source %s\n", shellQuote(fsutil.Tilde(path)))
	if targetName != "" {
		fmt.Fprintf(os.Stderr, "or:\n  eval \"$(envset reload --target %s)\"\n", shellQuote(targetName))
	}
}

// shellQuote single-quotes s if it contains anything a shell would split or
// expand, leaving a leading ~/ outside the quotes so it still expands.
func shellQuote(s string) string {
	if s == "" || strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./~") == "" {
		return s
	}
	if strings.HasPrefix(s, "~/") {
		return "~/" + "'" + strings.ReplaceAll(s[2:], "'", `'\''`) + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// PrintSourceCommand writes only the source command to stdout — suitable for
// `eval "$(envset add ... --print-source)"`. All other output should already
// be going to stderr at this point.
func PrintSourceCommand(out io.Writer, path string) {
	fmt.Fprintf(out, "source %s\n", shellQuote(path))
}
