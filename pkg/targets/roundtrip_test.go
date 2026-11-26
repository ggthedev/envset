package targets

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// What you add is exactly what the real shell sees, for the values that used
// to be changed: zsh "=word" expansion and fish backslash escapes.
func TestValueRoundTripInRealShells(t *testing.T) {
	values := []string{
		"=ls", "=eq", "=_.", ":=a", "/a:=x", "a=b", "=", "/a:~root",
		`C:\\x`, `a\\\\b`, `C:\Users`, `end\`, `\`, `\'`, `it's`, `say "hi"`, `a b`, `{x}`, `#h`, `;x`, `*`, `%s`, `é ✓`,
	}
	shells := map[string]struct {
		target string
		args   []string
	}{
		"zsh":  {"zshenv", []string{"-f", "-c"}},
		"bash": {"bashrc", []string{"--norc", "--noprofile", "-c"}},
		"fish": {"config_fish", []string{"--no-config", "-c"}},
	}
	for shell, cfg := range shells {
		bin, err := exec.LookPath(shell)
		if err != nil {
			t.Logf("%s not installed", shell)
			continue
		}
		types := []VarType{VarTypeExport}
		if shell == "fish" {
			types = append(types, VarTypeFishSet)
		}
		for _, mode := range []QuoteMode{QuoteLiteral, QuoteExpand} {
			for _, vt := range types {
				for _, v := range values {
					st, path := shellTarget(t, cfg.target, "")
					if _, err := st.Add("K", v, vt, WriteOptions{Quote: mode}); err != nil {
						t.Errorf("%s %s %s %q: %v", shell, mode, vt, v, err)
						continue
					}
					cmd := exec.Command(bin, append(cfg.args, `source "`+path+`"; printf '%s' "$K"`)...)
					cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
					out, err := cmd.Output()
					if err != nil || string(out) != v {
						t.Errorf("%s %q %s %q: shell sees %q (%v); line %q", shell, mode, vt, v, out, err, content(t, path))
					}
				}
			}
		}
		// PATH entries with backslashes.
		for _, mode := range []QuoteMode{QuoteLiteral, QuoteExpand} {
			st, path := shellTarget(t, cfg.target, "")
			dir := `/opt/a\\b c's`
			if _, err := st.Add("PATH", dir, VarTypePathAppend, WriteOptions{Quote: mode}); err != nil {
				t.Errorf("%s path %q: %v", shell, mode, err)
				continue
			}
			cmd := exec.Command(bin, append(cfg.args, `source "`+path+`"; /usr/bin/printenv PATH`)...)
			cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
			out, err := cmd.Output()
			if err != nil || !strings.Contains(string(out), ":"+dir) {
				t.Errorf("%s path %q: PATH=%q (%v); line %q", shell, mode, out, err, content(t, path))
			}
		}
	}
}
