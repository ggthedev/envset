package ops

import "testing"

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/Users/a/.zshrc":            "/Users/a/.zshrc",
		"~/.zshenv":                  "~/.zshenv",
		"zshenv":                     "zshenv",
		"/Users/a/My Dotfiles/zshrc": "'/Users/a/My Dotfiles/zshrc'",
		"~/My Files/x":               "~/'My Files/x'",
		"/tmp/it's":                  `'/tmp/it'\''s'`,
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}
