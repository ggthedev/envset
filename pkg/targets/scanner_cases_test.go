package targets

import (
	"errors"
	"os/exec"
	"testing"
)

// Constructs found by differential testing against real shells: in each, K sits
// inside a block or continued command, so envset must refuse to edit it.
func TestScannerSeesTheEnclosingBlock(t *testing.T) {
	both := []string{"zshrc", "bashrc"}
	cases := []struct {
		name   string
		body   string
		shells []string
	}{
		{"case pattern inside subshell", "(\n  case x in\n  y)\n    :\n    ;;\n  esac\n  export K=1\n)\n", both},
		{"brace argument in bash", "f() {\n  echo \\\n    } >/dev/null\n  export K=1\n}\n", []string{"bashrc"}},
		{"brace argument same line", "f() {\n  echo } >/dev/null\n  export K=1\n}\n", []string{"bashrc"}},
		{"keyword after substitution", "if a; then\n  echo $(date) fi\n  export K=1\nfi\n", both},
		{"heredoc on a continued line", "cat <<E \\\n  > out.txt\nbody\nE\nif false; then\n  export K=1\nfi\n", both},
		{"block inside subshell lines", "if a; then\n  (\n  echo\n  if x\n  then y; fi\n  )\n  export K=1\nfi\n", both},
		{"keywords in an array", "if a; then\n  arr=(\n    fi done esac\n  )\n  export K=1\nfi\n", both},
		{"escaped redirect then pipe", "echo a\\>|\n  export K=1\n", both},
	}
	for _, c := range cases {
		for _, shell := range c.shells {
			st, p := shellTarget(t, shell, c.body)
			if bin, err := exec.LookPath(st.shell); err == nil {
				if out, err := exec.Command(bin, "-n", p).CombinedOutput(); err != nil {
					t.Fatalf("%s: the case itself does not parse in %s: %s", c.name, st.shell, out)
				}
			}
			var refused *RefusedError
			if _, err := st.Add("K", "2", VarTypeExport, WriteOptions{}); !errors.As(err, &refused) {
				t.Errorf("%s/%s: add err = %v, want refusal", c.name, shell, err)
			}
			if content(t, p) != c.body {
				t.Errorf("%s/%s changed", c.name, shell)
			}
		}
	}
}

// Constructs after which K runs unconditionally and stays editable.
func TestScannerDoesNotOverRefuse(t *testing.T) {
	cases := map[string]string{
		"zsh disown &|":             "sleep 0 &|\n",
		"multi-line substitution":   "false &&\nx=$(\n)\n",
		"redirect to fi after done": "for i in; do\n  :\ndone < /dev/null\n",
		"closers without ;":         "{ if a; then b; fi }\n",
		"clobber redirect":          "echo a >|\n/dev/null\n",
	}
	for name, pre := range cases {
		if name == "clobber redirect" {
			pre = "echo a >| /dev/null\n"
		}
		st, p := shellTarget(t, "zshrc", pre+"export K=1\n")
		if _, err := st.Add("K", "2", VarTypeExport, WriteOptions{}); err != nil {
			t.Errorf("%s: %v", name, err)
		} else if got := content(t, p); got != pre+"export K=2\n" {
			t.Errorf("%s: %q", name, got)
		}
	}
}
