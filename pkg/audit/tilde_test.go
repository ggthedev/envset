package audit

import (
	"strings"
	"testing"
)

func TestQuotedTildePath(t *testing.T) {
	flagged := []string{
		`export PATH="~/.local/bin:$PATH"`,
		`export PATH="$PATH:~/bin"`,
		`PATH='~/x:/usr/bin'`,
		`[[ -d x ]] && export PATH="~/x:$PATH"`,
		`PATH+=":~/y"`,
	}
	clean := []string{
		`export PATH="$HOME/.local/bin:$PATH"`,
		`export PATH=~/bin:$PATH`, // unquoted: the shell expands it
		`# export PATH="~/old:$PATH"`,
		`export MANPATH="~/man"`,
		`export PATH="/opt/a~/b:$PATH"`, // ~ not at the start of an element
		`export NOTE="~/x"`,
	}
	for _, ln := range flagged {
		got := checkQuotedTildePath("/h/.zshrc", []string{ln})
		if len(got) != 1 {
			t.Errorf("%q: %d findings, want 1", ln, len(got))
			continue
		}
		if !strings.Contains(got[0].Suggestion, "$HOME") || strings.Contains(got[0].Suggestion, "~") {
			t.Errorf("%q: suggestion %q", ln, got[0].Suggestion)
		}
	}
	for _, ln := range clean {
		if got := checkQuotedTildePath("/h/.zshrc", []string{ln}); len(got) != 0 {
			t.Errorf("%q flagged: %s", ln, got[0].Suggestion)
		}
	}
}
