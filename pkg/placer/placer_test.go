package placer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggthedev/envset/pkg/detector"
	"github.com/ggthedev/envset/pkg/targets"
)

func TestDetectIntent(t *testing.T) {
	cases := []struct {
		key       string
		varType   targets.VarType
		wantCat   Category
		wantScope Scope
	}{
		{"PATH", targets.VarTypePathAppend, CatPath, ScopeAllSessions},
		{"JAVA_HOME", targets.VarTypeExport, CatApp, ScopeAllSessions},
		{"EDITOR", targets.VarTypeExport, CatUserPref, ScopeInteractive},
		{"AWS_SECRET_KEY", targets.VarTypeExport, CatSecret, ScopeAllSessions},
		{"LANG", targets.VarTypeExport, CatSystem, ScopeAllSessions},
		{"FOO", targets.VarTypeExport, CatGeneric, ScopeUnknown},
	}

	for _, c := range cases {
		intent := DetectIntent(c.key, "bar", c.varType)
		if intent.Category != c.wantCat {
			t.Errorf("DetectIntent(%q).Category = %v, want %v", c.key, intent.Category, c.wantCat)
		}
		if intent.Scope != c.wantScope {
			t.Errorf("DetectIntent(%q).Scope = %v, want %v", c.key, intent.Scope, c.wantScope)
		}
	}
}

func TestPlacerAnalyze(t *testing.T) {
	home := t.TempDir()

	// Create a fake .zshrc
	zshrc := filepath.Join(home, ".zshrc")
	os.WriteFile(zshrc, []byte("export EDITOR=emacs\n"), 0644)

	info := &detector.Info{
		OS:         "darwin",
		Shell:      "zsh",
		LoginShell: "zsh",
		HomeDir:    home,
		ShellConfigs: []detector.ShellConfig{
			{Name: ".zshrc", Path: zshrc, Shell: "zsh", Exists: true, Priority: 1},
		},
	}

	p := NewPlacer(info)

	// PATH should go to all_sessions scope (zshenv, or gg_system_exports if exists)
	intent := DetectIntent("MYPATH", "/foo", targets.VarTypePathAppend)
	placements := p.Analyze(intent)
	if len(placements) == 0 {
		t.Fatal("expected placements for PATH")
	}

	// EDITOR already exists in zshrc → should suggest zshrc with high confidence
	intent2 := DetectIntent("EDITOR", "vim", targets.VarTypeExport)
	placements2 := p.Analyze(intent2)
	if len(placements2) == 0 {
		t.Fatal("expected placements for EDITOR")
	}
	top := placements2[0]
	if !top.Update {
		t.Error("expected top placement to be an update (EDITOR exists)")
	}
	if top.Confidence < 0.9 {
		t.Errorf("expected high confidence for existing key, got %.2f", top.Confidence)
	}
}

func TestRegistryCustomFiles(t *testing.T) {
	home := t.TempDir()

	// Create custom files
	ggExports := filepath.Join(home, ".gg_system_exports")
	ggInteractive := filepath.Join(home, ".gg_system_interactive")
	os.WriteFile(ggExports, []byte("export FOO=bar\n"), 0644)
	os.WriteFile(ggInteractive, []byte("alias l=ls\n"), 0644)

	reg := NewRegistry(home, "zsh")

	var foundExports, foundInteractive bool
	for _, f := range reg.Files {
		if f.Name == "gg_system_exports" {
			foundExports = true
			if !f.Exists {
				t.Error("gg_system_exports should exist")
			}
			if f.Scope != ScopeAllSessions {
				t.Errorf("gg_system_exports scope = %v, want all_sessions", f.Scope)
			}
		}
		if f.Name == "gg_system_interactive" {
			foundInteractive = true
			if !f.Exists {
				t.Error("gg_system_interactive should exist")
			}
			if f.Scope != ScopeInteractive {
				t.Errorf("gg_system_interactive scope = %v, want interactive", f.Scope)
			}
		}
	}

	if !foundExports {
		t.Error("gg_system_exports not in registry")
	}
	if !foundInteractive {
		t.Error("gg_system_interactive not in registry")
	}
}

// findPlacement is a small test helper.
func findPlacement(pls []Placement, name string) *Placement {
	for i := range pls {
		if pls[i].File.Name == name {
			return &pls[i]
		}
	}
	return nil
}

// TestPlacerScopeMismatchPenalty reproduces the exact "houston" case from
// audit: gg_system_exports is declared all_sessions but only sourced from
// .zprofile (login). When the user wants all_sessions, the placer must:
//   - mark gg_system_exports with ScopeMismatch=true
//   - drop its confidence by ~0.20
//   - prefer .zshenv (correct effective scope) as the top suggestion
func TestPlacerScopeMismatchPenalty(t *testing.T) {
	home := t.TempDir()
	writeFile(t, home, ".gg_system_exports", "export OLD=1\n")
	writeFile(t, home, ".zprofile", "source ~/.gg_system_exports\n")
	writeFile(t, home, ".zshenv", "# nothing\n")
	writeFile(t, home, ".zshrc", "# nothing\n")

	info := &detector.Info{
		OS: "darwin", Shell: "zsh", LoginShell: "zsh", HomeDir: home,
	}
	p := NewPlacer(info)

	// JAVA_HOME is classified by DetectIntent as CatApp + all_sessions.
	intent := DetectIntent("JAVA_HOME", "/opt/jdk", targets.VarTypeExport)
	if intent.Scope != ScopeAllSessions {
		t.Fatalf("precondition: intent.Scope=%v, want all_sessions", intent.Scope)
	}

	placements := p.Analyze(intent)

	gg := findPlacement(placements, "gg_system_exports")
	if gg == nil {
		t.Fatal("gg_system_exports missing from placements")
	}
	if !gg.ScopeMismatch {
		t.Errorf("expected ScopeMismatch=true on gg_system_exports (effective=%s, intent=%s)",
			gg.EffectiveScope, intent.Scope)
	}
	if gg.EffectiveScope != ScopeLogin {
		t.Errorf("EffectiveScope = %s, want login", gg.EffectiveScope)
	}

	zshenv := findPlacement(placements, "zshenv")
	if zshenv == nil {
		t.Fatal("zshenv missing from placements")
	}
	if zshenv.ScopeMismatch {
		t.Errorf("zshenv should not have ScopeMismatch (it IS the all_sessions primary)")
	}
	if zshenv.Confidence <= gg.Confidence {
		t.Errorf("zshenv confidence (%.2f) should beat broken gg_system_exports (%.2f)",
			zshenv.Confidence, gg.Confidence)
	}

	// Top suggestion should be a healthy file, not the broken one.
	if placements[0].ScopeMismatch {
		t.Errorf("top placement (%s) has ScopeMismatch=true; expected a healthy file to win",
			placements[0].File.Name)
	}
}

// TestPlacerNoMismatchWhenSourceChainHealthy verifies the penalty is NOT
// applied when gg_system_exports is correctly sourced from .zshenv.
func TestPlacerNoMismatchWhenSourceChainHealthy(t *testing.T) {
	home := t.TempDir()
	writeFile(t, home, ".gg_system_exports", "export OLD=1\n")
	writeFile(t, home, ".zshenv", "source ~/.gg_system_exports\n")

	info := &detector.Info{
		OS: "darwin", Shell: "zsh", LoginShell: "zsh", HomeDir: home,
	}
	p := NewPlacer(info)

	intent := DetectIntent("MYAPP_HOME", "/opt", targets.VarTypeExport)
	pls := p.Analyze(intent)

	gg := findPlacement(pls, "gg_system_exports")
	if gg == nil {
		t.Fatal("gg_system_exports missing")
	}
	if gg.ScopeMismatch {
		t.Errorf("ScopeMismatch=true on a healthy gg_system_exports (effective=%s)", gg.EffectiveScope)
	}
	if gg.EffectiveScope != ScopeAllSessions {
		t.Errorf("EffectiveScope=%s, want all_sessions", gg.EffectiveScope)
	}
}

// TestPlacerDuplicateLocations: when the same key already lives in 2 files,
// every candidate placement (even the winning update) carries the OTHER
// locations in DuplicateLocations so the UI can warn about dead duplicates.
func TestPlacerDuplicateLocations(t *testing.T) {
	home := t.TempDir()
	zshenv := writeFile(t, home, ".zshenv", "export FOO=one\n")
	zshrc := writeFile(t, home, ".zshrc", "export FOO=two\n")

	info := &detector.Info{
		OS: "darwin", Shell: "zsh", LoginShell: "zsh", HomeDir: home,
	}
	p := NewPlacer(info)

	intent := DetectIntent("FOO", "three", targets.VarTypeExport)
	pls := p.Analyze(intent)

	// Both existing files should appear as Update placements.
	envPl := findPlacement(pls, "zshenv")
	rcPl := findPlacement(pls, "zshrc")
	if envPl == nil || rcPl == nil {
		t.Fatalf("expected both zshenv and zshrc placements, got %d entries", len(pls))
	}

	if !envPl.Update || !rcPl.Update {
		t.Errorf("both update flags should be true; envPl.Update=%v rcPl.Update=%v",
			envPl.Update, rcPl.Update)
	}

	// envPl should mention zshrc as a duplicate location and vice versa.
	if len(envPl.DuplicateLocations) != 1 || envPl.DuplicateLocations[0] != zshrc {
		t.Errorf("envPl.DuplicateLocations = %v, want [%s]", envPl.DuplicateLocations, zshrc)
	}
	if len(rcPl.DuplicateLocations) != 1 || rcPl.DuplicateLocations[0] != zshenv {
		t.Errorf("rcPl.DuplicateLocations = %v, want [%s]", rcPl.DuplicateLocations, zshenv)
	}

	// A NON-existing-file candidate (e.g. zprofile) should also list both
	// existing locations as duplicates the user should be aware of.
	if pf := findPlacement(pls, "zprofile"); pf != nil {
		if len(pf.DuplicateLocations) != 2 {
			t.Errorf("zprofile placement should list 2 duplicates, got %v", pf.DuplicateLocations)
		}
	}
}

// Placement must not depend on the shell envset happens to run under: the
// registry for a zsh user always contains the zsh files.
func TestRegistrySkipsMissingCustomFiles(t *testing.T) {
	home := t.TempDir()
	reg := NewRegistry(home, "zsh")
	for _, n := range []string{"gg_system_exports", "gg_system_interactive"} {
		if _, err := reg.FindByName(n); err == nil {
			t.Errorf("%s registered although missing", n)
		}
	}
	if _, err := reg.FindByName("zshenv"); err != nil {
		t.Error("zshenv missing from zsh registry")
	}
}

func TestPlacePATHFromShScriptContext(t *testing.T) {
	home := t.TempDir()
	// Detector resolves the user shell to zsh even when running under sh.
	info := &detector.Info{OS: "darwin", Shell: "zsh", RunningShell: "sh", LoginShell: "sh", HomeDir: home}
	pls := NewPlacer(info).Analyze(DetectIntent("PATH", "/opt/x/bin", targets.VarTypePathAppend))
	if len(pls) == 0 || pls[0].File.Name != "zshenv" {
		t.Fatalf("PATH placed in %v, want zshenv", pls)
	}
}
