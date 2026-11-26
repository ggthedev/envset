package targets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func shellTarget(t *testing.T, name, content string) (*ShellTarget, string) {
	t.Helper()
	home := t.TempDir()
	st, err := NewShellTarget(name, home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(st.Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path(), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return st, st.Path()
}

func content(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Lines envset cannot rewrite safely are refused and left untouched, for
// update, comment-out and delete alike.
func TestRefusesToDamageHandWrittenCode(t *testing.T) {
	cases := map[string]string{
		"two assignments":  "export K=1 OTHER=2\n",
		"semicolon chain":  "export K=1; export OTHER=2\n",
		"and chain":        "export K=1 && echo ok\n",
		"continuation":     "export K=1 \\\n  OTHER=2\n",
		"multiline quoted": "export K=\"line1\nline2\"\n",
		"inside if/else":   "if true; then\n  export K=a\nelse\n  export K=b\nfi\n",
		"inside then only": "if true; then\n  export K=a\nfi\n",
		"inside function":  "f() {\n  export K=fn\n}\n",
		"function keyword": "function f {\n  export K=fn\n}\n",
		"inside case":      "case $X in\n  a)\n    export K=a\n    ;;\nesac\n",
		"inside for loop":  "for i in 1 2; do\n  export K=$i\ndone\n",
		"inside while":     "while false\ndo\n  export K=w\ndone\n",
		"inside braces":    "{\n  export K=g\n}\n",
		"inside subshell":  "(\n  export K=s\n)\n",
		"top then block":   "export K=top\nif true; then\n  export K=a\nfi\n",
		"nested blocks":    "if a; then\n  if b; then\n    :\n  fi\n  export K=n\nfi\n",
		"after &&":         "false &&\n  export K=a\n",
		"after ||":         "true ||\n  export K=a\n",
		"after pipe":       "echo |\n  export K=a\n",
		"after && comment": "false && # why\n\n  # more\n  export K=a\n",
	}
	for name, body := range cases {
		for _, shell := range []string{"zshrc", "bashrc"} {
			st, p := shellTarget(t, shell, body)
			_, addErr := st.Add("K", "NEW", VarTypeExport, WriteOptions{})
			_, comErr := st.Remove("K", RemoveOptions{Comment: true})
			_, delErr := st.Remove("K", RemoveOptions{})
			for op, err := range map[string]error{"add": addErr, "comment": comErr, "delete": delErr} {
				var refused *RefusedError
				if !errors.As(err, &refused) {
					t.Errorf("%s/%s %s: err = %v, want refusal", name, shell, op, err)
				} else if strings.Contains(err.Error(), "export") {
					t.Errorf("%s: error text repeats file contents (it is logged): %s", name, err)
				}
			}
			if got := content(t, p); got != body {
				t.Errorf("%s/%s: file changed:\n%s", name, shell, got)
			}
			// --append leaves every existing line alone and adds one that wins.
			res, err := st.Add("K", "NEW", VarTypeExport, WriteOptions{Append: true})
			if err != nil || res.Outcome != Appended {
				t.Errorf("%s/%s append: %v %v", name, shell, res.Outcome, err)
				continue
			}
			if got := content(t, p); got != body+"export K=NEW\n" {
				t.Errorf("%s/%s append result:\n%s", name, shell, got)
			}
			if res, _ := st.Add("K", "NEW", VarTypeExport, WriteOptions{Append: true}); res.Outcome != Unchanged {
				t.Errorf("%s/%s second append: %s", name, shell, res.Outcome)
			}
		}
	}
}

// Text inside heredocs and multi-line strings is data, never a definition.
func TestHeredocAndStringTextIsNotADefinition(t *testing.T) {
	cases := map[string]string{
		"heredoc":       "cat <<'X' >/dev/null\nexport K=inside\nX\n",
		"dash heredoc":  "cat <<-END >/dev/null\n\texport K=inside\n\tEND\n",
		"two heredocs":  "cat <<A <<B >/dev/null\nexport K=1\nA\nexport K=2\nB\n",
		"inside string": "export NOTE=\"\nexport K=inside\n\"\n",
		"single quoted": "NOTE='\nexport K=inside\n'\n",
	}
	for name, body := range cases {
		st, p := shellTarget(t, "zshrc", body)
		res, err := st.Add("K", "NEW", VarTypeExport, WriteOptions{})
		if err != nil || res.Outcome != Appended {
			t.Errorf("%s: %s %v", name, res.Outcome, err)
		}
		if got := content(t, p); got != body+"export K=NEW\n" {
			t.Errorf("%s: %q", name, got)
		}
		if _, err := st.Remove("K", RemoveOptions{}); err != nil {
			t.Errorf("%s remove: %v", name, err)
		}
		if got := content(t, p); got != body {
			t.Errorf("%s after remove: %q", name, got)
		}
	}
}

// Ordinary shell constructs must not make later top-level lines look nested.
func TestTopLevelLinesStayEditable(t *testing.T) {
	preludes := []string{
		"if [[ -n $A ]]; then\n  :\nfi\n",
		"if true; then echo a; else echo b; fi\n",
		"f() {\n  echo \"}\"\n}\n",
		"f() { echo hi }\n",
		"case $x in\n  (a|b) echo ;;\n  c) ;;\nesac\n",
		"for f in *.sh; do source $f; done\n",
		"plugins=(\n  git\n  z # tools (x)\n)\n",
		"echo $(( 1 << 2 ))\n(( x = 1 << 3 ))\n",
		"cat <<< 'here string' >/dev/null\n",
		"echo \"$(dirname \"$(which ls)\")\" # it's fine\n",
		"echo ${#PATH} $# a#b {a,b} {} \\' \n",
		"PROMPT='%F{blue}(%~)%f '\n",
		"x=$'a\\'b'\n",
		"[[ $a =~ '^(x|y)$' ]] && echo\n",
		"y=`echo \"a)\"`\n",
		"true && echo a || echo b\n",
		"sleep 0 &\n",
		"ls | wc -l\n",
		"false &&\n  echo joined\n",
		"case $x in\n  a) echo ;|\n  b) ;;\nesac\n",
		"case $x in\n  a) echo ;&\n  b) ;;\nesac\n",
	}
	for _, pre := range preludes {
		st, p := shellTarget(t, "zshrc", pre+"export K=1  # why\n")
		res, err := st.Add("K", "2", VarTypeExport, WriteOptions{})
		if err != nil || res.Outcome != Updated {
			t.Errorf("after %q: %s %v", pre, res.Outcome, err)
			continue
		}
		if got := content(t, p); got != pre+"export K=2  # why\n" {
			t.Errorf("after %q: %q", pre, got)
		}
	}
}

func TestTrailingCommentKeptAndIdentical(t *testing.T) {
	st, p := shellTarget(t, "zshrc", "export K=\"a # not a comment\"   # real\n")
	if res, err := st.Add("K", "a # not a comment", VarTypeExport, WriteOptions{}); err != nil || res.Outcome != Unchanged {
		t.Fatalf("identical: %s %v", res.Outcome, err)
	}
	if _, err := st.Add("K", "b", VarTypeExport, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := content(t, p); got != "export K=b   # real\n" {
		t.Fatalf("%q", got)
	}
}

func TestFishBlocks(t *testing.T) {
	body := "if status is-interactive\n    set -x K a\nelse if true\n    echo\nend\n"
	st, p := shellTarget(t, "config_fish", body)
	var refused *RefusedError
	if _, err := st.Add("K", "NEW", VarTypeFishSet, WriteOptions{}); !errors.As(err, &refused) {
		t.Fatalf("want refusal, got %v", err)
	}
	if content(t, p) != body {
		t.Fatal("changed")
	}
	st, p = shellTarget(t, "config_fish", body+"set -x K top a b\n")
	if res, err := st.Add("K", "NEW", VarTypeFishSet, WriteOptions{}); err != nil || res.Outcome != Updated {
		t.Fatalf("top-level after block: %s %v", res.Outcome, err)
	}
	if got := content(t, p); got != body+"set -x K NEW\n" {
		t.Fatalf("%q", got)
	}
}

func TestPATHRemovalRefusesCompoundLine(t *testing.T) {
	body := "export PATH=\"$PATH:/a\"; echo x\n"
	st, p := shellTarget(t, "zshrc", body)
	var refused *RefusedError
	if _, err := st.Remove("PATH", RemoveOptions{Value: "/a"}); !errors.As(err, &refused) {
		t.Fatalf("want refusal, got %v", err)
	}
	if content(t, p) != body {
		t.Fatal("changed")
	}
}

func TestNoAppendIntoUnterminatedText(t *testing.T) {
	for _, body := range []string{"cat <<X\nabc\n", "export A=\"open\n", "echo \\\n"} {
		st, p := shellTarget(t, "zshrc", body)
		if _, err := st.Add("K", "1", VarTypeExport, WriteOptions{}); err == nil {
			t.Errorf("%q: appended", body)
		}
		if got := content(t, p); got != body {
			t.Errorf("%q changed to %q", body, got)
		}
	}
}
