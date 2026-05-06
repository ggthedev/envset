package placer

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// helper: write a file under home with given content; returns absolute path.
func writeFile(t *testing.T, home, name, content string) string {
	t.Helper()
	p := filepath.Join(home, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// findFile picks a file out of the registry by name (test helper).
func findFile(t *testing.T, r *Registry, name string) *ConfigFile {
	t.Helper()
	f, err := r.FindByName(name)
	if err != nil {
		t.Fatalf("file %q not in registry", name)
	}
	return f
}

func TestSourceChain_NotSourcedAnywhere(t *testing.T) {
	home := t.TempDir()
	// gg_system_exports exists but no shell file sources it.
	writeFile(t, home, ".gg_system_exports", "export FOO=bar\n")

	r := NewRegistry(home, "zsh")
	gg := findFile(t, r, "gg_system_exports")

	if got, want := gg.EffectiveScope, ScopeAllSessions; got != want {
		t.Errorf("EffectiveScope (no sources) = %s, want %s (declared)", got, want)
	}
	if len(gg.SourcedFrom) != 0 {
		t.Errorf("SourcedFrom should be empty, got %v", gg.SourcedFrom)
	}
}

func TestSourceChain_BrokenScope_LoginOnly(t *testing.T) {
	// The exact pattern your audit just flagged: gg_system_exports is declared
	// all_sessions but is sourced only from .zprofile (login).
	home := t.TempDir()
	writeFile(t, home, ".gg_system_exports", "export FOO=bar\n")
	writeFile(t, home, ".zprofile", "source ~/.gg_system_exports\n")
	// .zshenv exists but does NOT source gg_system_exports
	writeFile(t, home, ".zshenv", "# nothing\n")

	r := NewRegistry(home, "zsh")
	gg := findFile(t, r, "gg_system_exports")

	if got, want := gg.EffectiveScope, ScopeLogin; got != want {
		t.Errorf("EffectiveScope = %s, want %s (because only .zprofile sources it)", got, want)
	}
	wantSources := []string{filepath.Join(home, ".zprofile")}
	got := append([]string(nil), gg.SourcedFrom...)
	sort.Strings(got)
	sort.Strings(wantSources)
	if len(got) != 1 || got[0] != wantSources[0] {
		t.Errorf("SourcedFrom = %v, want %v", got, wantSources)
	}
}

func TestSourceChain_HealthyScope_FromZshenv(t *testing.T) {
	home := t.TempDir()
	writeFile(t, home, ".gg_system_exports", "export FOO=bar\n")
	writeFile(t, home, ".zshenv", "source ~/.gg_system_exports\n")

	r := NewRegistry(home, "zsh")
	gg := findFile(t, r, "gg_system_exports")

	if got, want := gg.EffectiveScope, ScopeAllSessions; got != want {
		t.Errorf("EffectiveScope = %s, want %s (sourced from zshenv)", got, want)
	}
}

func TestSourceChain_BroadestWins_WhenSourcedFromMultiple(t *testing.T) {
	home := t.TempDir()
	writeFile(t, home, ".gg_system_exports", "export FOO=bar\n")
	writeFile(t, home, ".zshenv", "source ~/.gg_system_exports\n")
	writeFile(t, home, ".zprofile", "source ~/.gg_system_exports\n")

	r := NewRegistry(home, "zsh")
	gg := findFile(t, r, "gg_system_exports")

	if got, want := gg.EffectiveScope, ScopeAllSessions; got != want {
		t.Errorf("EffectiveScope = %s, want %s (broadest of zshenv+zprofile)", got, want)
	}
}

func TestSourceChain_PrimaryFilesUseDeclaredScope(t *testing.T) {
	home := t.TempDir()
	writeFile(t, home, ".zshrc", "# nothing\n")
	writeFile(t, home, ".zshenv", "# nothing\n")
	writeFile(t, home, ".zprofile", "# nothing\n")

	r := NewRegistry(home, "zsh")
	for _, want := range []struct {
		name  string
		scope Scope
	}{
		{"zshrc", ScopeInteractive},
		{"zshenv", ScopeAllSessions},
		{"zprofile", ScopeLogin},
	} {
		f := findFile(t, r, want.name)
		if f.EffectiveScope != want.scope {
			t.Errorf("%s.EffectiveScope = %s, want %s", want.name, f.EffectiveScope, want.scope)
		}
	}
}

func TestSourceChain_CycleDoesNotInfiniteLoop(t *testing.T) {
	// Pathological: a sources b, b sources a. Should still terminate.
	home := t.TempDir()
	writeFile(t, home, ".gg_system_exports", "source ~/.gg_system_interactive\n")
	writeFile(t, home, ".gg_system_interactive", "source ~/.gg_system_exports\n")

	r := NewRegistry(home, "zsh")
	// Just make sure we got here without hanging.
	gg := findFile(t, r, "gg_system_exports")
	if gg.EffectiveScope == "" {
		t.Errorf("EffectiveScope unset after cycle; should have a value")
	}
}

func TestSourceChain_IgnoresCommentedSource(t *testing.T) {
	home := t.TempDir()
	writeFile(t, home, ".gg_system_exports", "export FOO=bar\n")
	writeFile(t, home, ".zshenv", "# source ~/.gg_system_exports\n")

	r := NewRegistry(home, "zsh")
	gg := findFile(t, r, "gg_system_exports")
	if len(gg.SourcedFrom) != 0 {
		t.Errorf("commented source should not register, got %v", gg.SourcedFrom)
	}
	if gg.EffectiveScope != ScopeAllSessions {
		t.Errorf("EffectiveScope = %s, want all_sessions (declared)", gg.EffectiveScope)
	}
}
