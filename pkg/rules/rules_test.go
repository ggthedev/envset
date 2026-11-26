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
	t.Setenv("HOME", dir)
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

func TestPruneStaleKeepsOtherMacsRules(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	custom := &Custom{Rules: []CustomRule{
		{CheckID: "a", Match: Match{File: "/Users/someone-else/.zshrc"}}, // other Mac, old absolute form
		{CheckID: "b", Match: Match{File: "~/.missing_here"}},            // under this HOME, missing
	}}
	if n := PruneStale(custom); n != 1 || len(custom.Rules) != 1 || custom.Rules[0].CheckID != "a" {
		t.Fatalf("pruned %d, left %+v", n, custom.Rules)
	}
}

// A rule recorded on one Mac (as ~/…) matches the same file on another Mac
// with a different home directory.
func TestPortableRulePathsAcrossHomes(t *testing.T) {
	cfg := withTempXDG(t)
	homeA := t.TempDir()
	t.Setenv("HOME", homeA)
	rule := CustomRule{CheckID: CheckSlowInteractiveInit, Match: Match{File: PortablePath(filepath.Join(homeA, ".zshrc")), Subject: "pyenv"}}
	if rule.Match.File != "~/.zshrc" {
		t.Fatalf("stored as %q, want ~/.zshrc", rule.Match.File)
	}
	if err := SaveCustom(Custom{Rules: []CustomRule{rule}}); err != nil {
		t.Fatal(err)
	}
	_ = cfg

	homeB := t.TempDir()
	t.Setenv("HOME", homeB)
	l, err := Load("t")
	if err != nil {
		t.Fatal(err)
	}
	if FindMatching(l.Custom, CheckSlowInteractiveInit, filepath.Join(homeB, ".zshrc"), "pyenv", "") == nil {
		t.Fatal("rule from HOME A did not match the same file under HOME B")
	}
}

func TestLoadMigratesAbsolutePathsUnderHome(t *testing.T) {
	cfg := withTempXDG(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(cfg, 0o755)
	b, _ := json.Marshal(Custom{Version: CurrentVersion, Rules: []CustomRule{{CheckID: "x", Match: Match{File: filepath.Join(home, ".zshrc"), Subject: "s"}}}})
	os.WriteFile(filepath.Join(cfg, "custom.json"), b, 0o644)
	l, _ := Load("t")
	if got := l.Custom.Rules[0].Match.File; got != "~/.zshrc" {
		t.Fatalf("migrated to %q", got)
	}
}

func TestLoadWritesNothing(t *testing.T) {
	cfg := withTempXDG(t)
	l, err := Load("t")
	if err != nil {
		t.Fatal(err)
	}
	if !l.IdealsStale || len(l.Ideals.Checks) == 0 {
		t.Fatal("expected built-in ideals in memory")
	}
	if _, err := os.Stat(cfg); !os.IsNotExist(err) {
		t.Fatalf("Load created %s", cfg)
	}
	if err := PersistIdeals(l); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg, "ideals.json")); err != nil {
		t.Fatal("PersistIdeals did not write ideals.json")
	}
}

func TestLoadMalformedCustomIsAWarning(t *testing.T) {
	cfg := withTempXDG(t)
	os.MkdirAll(cfg, 0o755)
	bad := []byte("{ not json")
	os.WriteFile(filepath.Join(cfg, "custom.json"), bad, 0o644)
	l, err := Load("t")
	if err != nil {
		t.Fatalf("malformed custom.json should not be fatal: %v", err)
	}
	if !l.CustomBroken || l.CustomWarning == nil || len(l.Custom.Rules) != 0 {
		t.Fatalf("got %+v", l)
	}
	if got, _ := os.ReadFile(filepath.Join(cfg, "custom.json")); string(got) != string(bad) {
		t.Fatal("malformed custom.json was modified")
	}
}
