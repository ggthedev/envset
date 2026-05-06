package placer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/experimentalist/envset/pkg/detector"
	"github.com/experimentalist/envset/pkg/targets"
)

func TestDetectIntent(t *testing.T) {
	cases := []struct {
		key      string
		varType  targets.VarType
		wantCat  Category
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
