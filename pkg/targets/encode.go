package targets

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ggthedev/envset/pkg/logger"
)

// QuoteMode controls how a value is encoded into a shell config line.
type QuoteMode string

const (
	// QuoteExpand (default): the value is literal text except that $VAR and
	// ${VAR} expand when the file is sourced. Command substitution ($(...) and
	// backticks) is rejected because it would run on every shell start.
	// A leading ~/ is rewritten to $HOME/ so it still expands inside quotes.
	QuoteExpand QuoteMode = ""
	// QuoteLiteral: the value is written verbatim inside single quotes;
	// nothing expands.
	QuoteLiteral QuoteMode = "literal"
	// QuoteRaw: the value is written exactly as given, unquoted and unchecked
	// (apart from newlines). The caller takes responsibility for its syntax.
	QuoteRaw QuoteMode = "raw"
)

var keyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateKey rejects anything that is not a plain shell variable name, so a
// key can never inject code into the line it is written to.
func ValidateKey(key string) error {
	defer logger.Enter()()
	if !keyRe.MatchString(key) {
		return logger.Private("invalid variable name",
			fmt.Errorf("invalid variable name %q: use letters, digits and underscores, not starting with a digit", key))
	}
	return nil
}

var bareChars = regexp.MustCompile(`^[A-Za-z0-9_./:@%+,=-]+$`)

// bare reports whether v needs no quoting in any supported shell. zsh expands
// a word starting "=" to a command's path, also after ":" in an assignment, so
// such values are quoted.
func bare(v string) bool {
	defer logger.Enter()()
	return bareChars.MatchString(v) && !strings.HasPrefix(v, "=") && !strings.Contains(v, ":=")
}

func checkNoNewline(v string) error {
	defer logger.Enter()()
	if strings.ContainsAny(v, "\x00\n\r") {
		return fmt.Errorf("value must be a single line (contains a newline or NUL byte)")
	}
	return nil
}

func checkExpandable(v string) error {
	defer logger.Enter()()
	if err := checkNoNewline(v); err != nil {
		return err
	}
	if strings.Contains(v, "`") || strings.Contains(v, "$(") {
		return fmt.Errorf("value contains command substitution ($(...) or backticks), which would run on every shell start; " +
			"pass --literal to store it as plain text, or --raw if you really want it executed")
	}
	return nil
}

func tildeToHome(v string) string {
	defer logger.Enter()()
	if v == "~" {
		return "$HOME"
	}
	if strings.HasPrefix(v, "~/") {
		return "$HOME" + v[1:]
	}
	return v
}

// posixDoubleInner escapes v for use inside a POSIX double-quoted string,
// leaving $ live for variable expansion.
func posixDoubleInner(v string) string {
	defer logger.Enter()()
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(tildeToHome(v))
}

func posixSingle(v string) string {
	defer logger.Enter()()
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// encodePosix encodes a value as a complete shell word for sh/bash/zsh.
// forceQuote always produces quotes (used by launchd for readability).
func encodePosix(v string, mode QuoteMode, forceQuote bool) (string, error) {
	defer logger.Enter()()
	switch mode {
	case QuoteRaw:
		return v, checkNoNewline(v)
	case QuoteLiteral:
		if err := checkNoNewline(v); err != nil {
			return "", err
		}
		if !forceQuote && bare(v) {
			return v, nil
		}
		return posixSingle(v), nil
	case QuoteExpand:
		if err := checkExpandable(v); err != nil {
			return "", err
		}
		if !forceQuote && bare(v) {
			return v, nil
		}
		return `"` + posixDoubleInner(v) + `"`, nil
	default:
		return "", fmt.Errorf("unknown quote mode %q", mode)
	}
}

// encodePosixPathElem encodes v as the dir to splice into "$PATH:<dir>".
// The result is placed directly inside an existing double-quoted string.
func encodePosixPathElem(v string, mode QuoteMode) (string, error) {
	defer logger.Enter()()
	switch mode {
	case QuoteRaw:
		return v, checkNoNewline(v)
	case QuoteLiteral:
		if err := checkNoNewline(v); err != nil {
			return "", err
		}
		// Close the double quote, emit a single-quoted literal, reopen.
		return `"` + posixSingle(v) + `"`, nil
	case QuoteExpand:
		if err := checkExpandable(v); err != nil {
			return "", err
		}
		return posixDoubleInner(v), nil
	default:
		return "", fmt.Errorf("unknown quote mode %q", mode)
	}
}

// fishSingle quotes v literally for fish, where (unlike POSIX shells)
// backslash still escapes "\" and "'" inside single quotes.
func fishSingle(v string) string {
	defer logger.Enter()()
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}

// encodeFishPathElem is encodePosixPathElem for fish files.
func encodeFishPathElem(v string, mode QuoteMode) (string, error) {
	defer logger.Enter()()
	if mode == QuoteLiteral {
		if err := checkNoNewline(v); err != nil {
			return "", err
		}
		return `"` + fishSingle(v) + `"`, nil
	}
	return encodePosixPathElem(v, mode)
}

// encodeFish encodes a value as a fish word.
func encodeFish(v string, mode QuoteMode) (string, error) {
	defer logger.Enter()()
	switch mode {
	case QuoteRaw:
		return v, checkNoNewline(v)
	case QuoteLiteral:
		if err := checkNoNewline(v); err != nil {
			return "", err
		}
		if bare(v) {
			return v, nil
		}
		return fishSingle(v), nil
	case QuoteExpand:
		if err := checkExpandable(v); err != nil {
			return "", err
		}
		if bare(v) {
			return v, nil
		}
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(tildeToHome(v)) + `"`, nil
	default:
		return "", fmt.Errorf("unknown quote mode %q", mode)
	}
}

// unquoteValue reverses the quoting envset writes, for display and comparison.
func unquoteValue(v string) string {
	defer logger.Enter()()
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(v[1 : len(v)-1])
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], `'\''`, `'`)
	}
	return v
}
