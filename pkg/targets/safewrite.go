package targets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/experimentalist/envset/pkg/fsutil"
	"github.com/experimentalist/envset/pkg/logger"
)

// textFile is a line-preserving view of a file: joining lines back reproduces
// the original bytes exactly (blank lines, CRLF and final newline included).
type textFile struct {
	lines      []string
	trailingNL bool
	raw        []byte
	existed    bool
}

func readTextFile(path string) (textFile, error) {
	defer logger.Enter()()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return textFile{}, nil
	}
	if err != nil {
		return textFile{}, fmt.Errorf("reading %s: %w", path, err)
	}
	logger.Debug("read %s (%d bytes)", path, len(data))
	tf := textFile{raw: data, existed: true}
	if len(data) == 0 {
		return tf, nil
	}
	s := string(data)
	if strings.HasSuffix(s, "\n") {
		tf.trailingNL = true
		s = s[:len(s)-1]
	}
	tf.lines = strings.Split(s, "\n")
	return tf, nil
}

func (tf *textFile) bytes() []byte {
	defer logger.Enter()()
	if len(tf.lines) == 0 {
		return nil
	}
	s := strings.Join(tf.lines, "\n")
	if tf.trailingNL {
		s += "\n"
	}
	return []byte(s)
}

// appendLines adds lines at the end, ensuring the file ends with a newline
// and using CRLF if the file does.
func (tf *textFile) appendLines(add ...string) {
	defer logger.Enter()()
	if n := len(tf.lines); n > 0 && strings.HasSuffix(tf.lines[0], "\r") {
		if !strings.HasSuffix(tf.lines[n-1], "\r") {
			tf.lines[n-1] += "\r"
		}
		crlf := make([]string, len(add))
		for i, l := range add {
			crlf[i] = l + "\r"
		}
		add = crlf
	}
	tf.lines = append(tf.lines, add...)
	tf.trailingNL = true
}

func leadingWS(s string) string {
	defer logger.Enter()()
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

// ShellForPath guesses which shell parses a config file, for syntax checks.
func ShellForPath(path string) string {
	defer logger.Enter()()
	base := filepath.Base(path)
	switch {
	case strings.HasSuffix(base, ".fish"):
		return "fish"
	case strings.HasPrefix(base, ".z") || strings.HasPrefix(base, ".gg_system"):
		return "zsh"
	case base == ".profile":
		return "sh"
	default:
		return "bash"
	}
}

// CommitFile is the single write path for every config file envset touches:
//  1. refuses the write if the new content no longer parses in its shell
//     (unless the old content already failed to parse);
//  2. saves a timestamped backup of the current file;
//  3. writes atomically, following symlinks and keeping the file mode.
//
// newMode applies only when the file is created. Returns the backup path
// ("" when the file did not exist before).
func CommitFile(path, shell string, oldData []byte, existed bool, newData []byte, newMode os.FileMode) (string, error) {
	defer logger.Enter()()
	if err := fsutil.CheckWritable(path); err != nil {
		return "", err
	}
	if err := syntaxGuard(shell, oldData, existed, newData); err != nil {
		return "", err
	}
	var backup string
	if existed {
		b, err := fsutil.Backup(path)
		if err != nil {
			return "", fmt.Errorf("backup failed, not writing %s: %w", path, err)
		}
		backup = b
	} else {
		real, err := fsutil.ResolvePath(path)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
			return "", fmt.Errorf("creating directory for %s: %w", path, err)
		}
	}
	if err := unchangedSinceRead(path, oldData, existed); err != nil {
		return "", err
	}
	logger.Debug("writing %s (%d -> %d bytes, existed=%v)", path, len(oldData), len(newData), existed)
	if err := fsutil.WriteFileAtomic(path, newData, newMode); err != nil {
		return "", err
	}
	return backup, nil
}

// unchangedSinceRead checks, just before replacing path, that nothing else
// (an editor, say, which takes no lock) changed it since envset read it.
func unchangedSinceRead(path string, oldData []byte, existed bool) error {
	defer logger.Enter()()
	now, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if !existed {
			return nil
		}
	case err != nil:
		return fmt.Errorf("re-reading %s: %w", path, err)
	case existed && bytes.Equal(now, oldData):
		return nil
	}
	return fmt.Errorf("%s changed while envset was editing it; nothing was written, run the command again", path)
}

func syntaxGuard(shell string, oldData []byte, existed bool, newData []byte) error {
	defer logger.Enter()()
	if os.Getenv("ENVSET_NO_SYNTAX_CHECK") == "1" {
		logger.Warn("syntax check skipped: ENVSET_NO_SYNTAX_CHECK=1")
		return nil
	}
	newErr, ok := parseCheck(shell, newData)
	if !ok || newErr == "" {
		return nil // checker unavailable, or new content parses
	}
	if existed {
		if oldErr, _ := parseCheck(shell, oldData); oldErr != "" {
			return nil // file was already unparseable; don't block edits to it
		}
	}
	// The checker quotes lines of the file, so its output stays out of the log.
	return logger.Private("refusing to write: the result would not parse in "+shell,
		fmt.Errorf("refusing to write: the result would not parse in %s:\n%s", shell, newErr))
}

// parseCheck runs the shell's no-exec parse mode on data. ok=false means the
// shell is not installed (check skipped); msg is non-empty on a parse error.
func parseCheck(shell string, data []byte) (msg string, ok bool) {
	defer logger.Enter()()
	var bin string
	var args []string
	switch shell {
	case "zsh", "bash", "sh":
		bin, args = shell, []string{"-n"}
	case "fish":
		bin, args = "fish", []string{"--no-execute"}
	default:
		return "", false
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		logger.Warn("syntax check skipped: %s is not installed", bin)
		return "", false
	}
	logger.Debug("syntax check with %s (%d bytes)", path, len(data))
	tmp, err := os.CreateTemp("", "envset-syntax-*")
	if err != nil {
		return "", false
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", false
	}
	tmp.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, path, append(args, tmp.Name())...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			logger.Warn("syntax check skipped: %s timed out", bin)
			return "", false // timed out: don't block on the checker
		}
		out := strings.ReplaceAll(strings.TrimSpace(stderr.String()), tmp.Name(), "<new content>")
		if out == "" {
			out = err.Error()
		}
		return out, true
	}
	return "", true
}
