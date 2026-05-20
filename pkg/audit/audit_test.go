package audit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/experimentalist/envset/pkg/placer"
)

// helper to create a fake home with the requested files and run an audit
func runAudit(t *testing.T, files map[string]string) Report {
	t.Helper()
	home := t.TempDir()
	for name, content := range files {
		full := filepath.Join(home, name)
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	reg := placer.NewRegistry(home, "zsh")
	return New(reg).Run()
}

func contains(t *testing.T, r Report, sev Severity, titleSubstr string) bool {
	t.Helper()
	for _, f := range r.Findings {
		if f.Severity == sev && containsStr(f.Title, titleSubstr) {
			return true
		}
	}
	return false
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestCheckBrokenPath(t *testing.T) {
	r := runAudit(t, map[string]string{
		".zshenv": `. "$HOME/.cargo/env"
export PATH="echo "$(which node)":$PATH"
`,
	})
	if !contains(t, r, SevError, "Broken PATH") {
		t.Fatalf("expected broken PATH error; got %+v", r.Findings)
	}
}

func TestCheckMisplacedPathExport(t *testing.T) {
	r := runAudit(t, map[string]string{
		".zshrc": `export PATH="$HOME/.rbenv/shims:$PATH"
`,
	})
	if !contains(t, r, SevWarn, "PATH export in interactive-only file") {
		t.Fatalf("expected PATH-in-zshrc warning; got %+v", r.Findings)
	}
}

func TestCheckSourceChainMismatch(t *testing.T) {
	// gg_system_exports lives at all_sessions scope but is sourced from
	// .zprofile (login scope) — should warn.
	r := runAudit(t, map[string]string{
		".zprofile":          `source ~/.gg_system_exports`,
		".gg_system_exports": `export JAVA_HOME=/foo`,
	})
	if !contains(t, r, SevWarn, "narrower than its content") {
		t.Fatalf("expected source-chain mismatch warning; got %+v", r.Findings)
	}
}

func TestCheckDuplicateCompinit(t *testing.T) {
	r := runAudit(t, map[string]string{
		".zshrc": `source $ZSH/oh-my-zsh.sh
autoload -Uz compinit
compinit -C
`,
	})
	if !contains(t, r, SevWarn, "Duplicate compinit") {
		t.Fatalf("expected duplicate-compinit warning; got %+v", r.Findings)
	}
}

func TestCheckSlowInteractiveInit(t *testing.T) {
	r := runAudit(t, map[string]string{
		".zshrc": `eval "$(direnv hook zsh)"
`,
	})
	if !contains(t, r, SevInfo, "Eager init") {
		t.Fatalf("expected eager-init info; got %+v", r.Findings)
	}
}

func TestSlowInitInsideLazyWrapperIgnored(t *testing.T) {
	r := runAudit(t, map[string]string{
		".zshrc": `pyenv() {
  unfunction pyenv
  eval "$(pyenv init - zsh)"
  pyenv "$@"
}
`,
	})
	if contains(t, r, SevInfo, "Eager init") {
		t.Fatalf("did NOT expect eager-init for lazy-wrapped pyenv; got %+v", r.Findings)
	}
}

func TestCheckDuplicateKeysAcrossFiles(t *testing.T) {
	r := runAudit(t, map[string]string{
		".zshrc":             `export EDITOR=vim`,
		".gg_system_exports": `export EDITOR=zed`,
	})
	if !contains(t, r, SevInfo, "defined in multiple files") {
		t.Fatalf("expected duplicate-key info; got %+v", r.Findings)
	}
}

func TestCleanFileNoFindings(t *testing.T) {
	r := runAudit(t, map[string]string{
		".zshenv": `export PATH="$HOME/.local/bin:$PATH"
export EDITOR=vim
`,
	})
	if len(r.Findings) != 0 {
		t.Fatalf("expected zero findings for a clean file; got %+v", r.Findings)
	}
}
