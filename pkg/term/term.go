// Package term decides whether envset may prompt, and routes prompts to
// stderr so stdout stays clean for `eval "$(envset …)"`.
package term

import (
	"github.com/experimentalist/envset/pkg/logger"
	"os"

	"github.com/AlecAivazis/survey/v2"
	xterm "golang.org/x/term"
)

// ForceInteractive overrides detection in tests (nil = detect).
var ForceInteractive *bool

// Interactive reports whether a human can answer prompts: stdin and stderr
// must both be terminals. Under cron, CI, pipes, `< /dev/null` or an IDE task
// this is false and envset must fail fast instead of waiting for input.
func Interactive() bool {
	defer logger.Enter()()
	if ForceInteractive != nil {
		return *ForceInteractive
	}
	return xterm.IsTerminal(int(os.Stdin.Fd())) && xterm.IsTerminal(int(os.Stderr.Fd()))
}

// Ask runs a survey prompt on stdin/stderr (never stdout).
func Ask(p survey.Prompt, response interface{}, opts ...survey.AskOpt) error {
	defer logger.Enter()()
	opts = append(opts, survey.WithStdio(os.Stdin, os.Stderr, os.Stderr))
	return survey.AskOne(p, response, opts...)
}
