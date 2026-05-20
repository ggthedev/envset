package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withTempXDG redirects rules.Dir() to a temp directory for the duration of the test.
func withTempXDG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "envset")
}

func TestLoadOrInit_CreatesFiles(t *testing.T) {
	cfgDir := withTempXDG(t)

	ideals, custom, err := LoadOrInit("test")
	if err != nil {
		t.Fatalf("LoadOrInit: %v", err)
	}
	if len(ideals.Checks) == 0 {
		t.Fatal("expected built-in ideals to be populated")
	}
	if len(custom.Rules) != 0 {
		t.Fatal("expected empty custom rules on first run")
	}

	for _, name := range []string{"ideals.json", "custom.json"} {
		if _, err := os.Stat(filepath.Join(cfgDir, name)); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}
}

func TestLoadOrInit_PreservesCustomOnIdealsRefresh(t *testing.T) {
	cfgDir := withTempXDG(t)

	if _, _, err := LoadOrInit("v1"); err != nil {
		t.Fatal(err)
	}

	// Inject a custom rule.
	rule := CustomRule{
		CheckID: CheckSlowInteractiveInit,
		Match:   Match{File: "/tmp/a.zshrc", Subject: "pyenv"},
		Action:  ActionAcknowledge,
		Reason:  "intentionally interactive",
	}
	if err := SaveCustom(Custom{Version: CurrentVersion, Rules: []CustomRule{rule}}); err != nil {
		t.Fatal(err)
	}

	// Force ideals refresh by writing a stale version.
	stale := Ideals{Version: 0, Checks: []IdealCheck{{ID: "old"}}}
	b, _ := json.Marshal(stale)
	if err := os.WriteFile(filepath.Join(cfgDir, "ideals.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	_, custom, err := LoadOrInit("v2")
	if err != nil {
		t.Fatal(err)
	}
	if len(custom.Rules) != 1 || custom.Rules[0].Match.Subject != "pyenv" {
		t.Fatalf("custom rule lost during ideals refresh: %+v", custom.Rules)
	}
}

func TestFindMatching_BySubject(t *testing.T) {
	c := Custom{Rules: []CustomRule{
		{
			CheckID: CheckSlowInteractiveInit,
			Match:   Match{File: "/h/.zshrc", Subject: "pyenv"},
			Action:  ActionAcknowledge,
		},
	}}
	if r := FindMatching(c, CheckSlowInteractiveInit, "/h/.zshrc", "pyenv", "anyhash"); r == nil {
		t.Error("expected subject match")
	}
	if r := FindMatching(c, CheckSlowInteractiveInit, "/h/.zshrc", "fnm", "anyhash"); r != nil {
		t.Error("different subject must not match")
	}
	if r := FindMatching(c, CheckSlowInteractiveInit, "/h/.bashrc", "pyenv", "anyhash"); r != nil {
		t.Error("different file must not match")
	}
}

func TestFindMatching_ByLineHash(t *testing.T) {
	hash := LineHash(`eval "$(pyenv init - zsh)"`)
	c := Custom{Rules: []CustomRule{
		{
			CheckID: CheckSlowInteractiveInit,
			Match:   Match{File: "/h/.zshrc", LineHash: hash},
			Action:  ActionAcknowledge,
		},
	}}
	if r := FindMatching(c, CheckSlowInteractiveInit, "/h/.zshrc", "", hash); r == nil {
		t.Error("expected hash match when subject empty")
	}
	if r := FindMatching(c, CheckSlowInteractiveInit, "/h/.zshrc", "", "differenthash"); r != nil {
		t.Error("different hash must not match")
	}
}

func TestRefreshImpacts(t *testing.T) {
	custom := &Custom{Rules: []CustomRule{
		{CheckID: CheckSlowInteractiveInit, ApproxImpact: "stale"},
	}}
	ideals := BuiltinIdeals("test")
	if !RefreshImpacts(custom, ideals) {
		t.Error("expected refresh to mark change")
	}
	if custom.Rules[0].ApproxImpact == "stale" {
		t.Errorf("impact not refreshed: %s", custom.Rules[0].ApproxImpact)
	}
}

func TestPruneStale(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists")
	if err := os.WriteFile(existing, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	custom := &Custom{Rules: []CustomRule{
		{CheckID: "x", Match: Match{File: existing}},
		{CheckID: "y", Match: Match{File: filepath.Join(dir, "missing")}},
	}}
	pruned := PruneStale(custom)
	if pruned != 1 {
		t.Errorf("expected 1 pruned; got %d", pruned)
	}
	if len(custom.Rules) != 1 || custom.Rules[0].Match.File != existing {
		t.Errorf("wrong rule survived: %+v", custom.Rules)
	}
}

func TestLineHash_Stable(t *testing.T) {
	a := LineHash(`  eval "$(pyenv init - zsh)"  `)
	b := LineHash(`eval "$(pyenv init - zsh)"`)
	if a != b {
		t.Errorf("trim should not affect hash: %s vs %s", a, b)
	}
}
