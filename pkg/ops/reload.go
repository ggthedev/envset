package ops

import (
	"fmt"
	"io"
	"os"
)

// PrintReloadHint prints a human-friendly hint to stderr after a write,
// telling the user how to apply the changes in the current shell.
func PrintReloadHint(path string) {
	fmt.Fprintf(os.Stderr, "\nTo apply changes in your current shell, run:\n  source %s\n", path)
	fmt.Fprintf(os.Stderr, "Or for a one-liner:\n  eval \"$(envset reload --target <name>)\"\n")
}

// PrintSourceCommand writes only the source command to stdout — suitable for
// `eval "$(envset add ... --print-source)"`. All other output should already
// be going to stderr at this point.
func PrintSourceCommand(out io.Writer, path string) {
	fmt.Fprintf(out, "source %s\n", path)
}
